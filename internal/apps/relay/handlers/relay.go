package handlers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/normahq/relay/internal/apps/relay/auth"
	relaychannel "github.com/normahq/relay/internal/apps/relay/channel"
	relaytelegram "github.com/normahq/relay/internal/apps/relay/channel/telegram"
	"github.com/normahq/relay/internal/apps/relay/messenger"
	relaysession "github.com/normahq/relay/internal/apps/relay/session"
	"github.com/normahq/relay/internal/throttle"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tgbotkit/client"
	"github.com/tgbotkit/runtime/events"
	"github.com/tgbotkit/runtime/handlers"
	"github.com/tgbotkit/runtime/messagetype"
	"go.uber.org/fx"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/genai"
)

// relayAuthorizer wraps OwnerStore and CollaboratorStore for auth.CanAccess.
type relayAuthorizer struct {
	ownerStore        *auth.OwnerStore
	collaboratorStore *auth.CollaboratorStore
}

const (
	ownerSessionLabel              = "relay"
	autoSessionLabel               = "auto"
	telegramTypingThrottleInterval = 4 * time.Second
)

func (a *relayAuthorizer) IsOwner(userID int64) bool {
	return a.ownerStore.IsOwner(userID)
}

func (a *relayAuthorizer) IsCollaborator(userID int64) bool {
	collab, found, err := a.collaboratorStore.GetCollaborator(context.Background(), fmt.Sprintf("%d", userID))
	if err != nil || !found {
		return false
	}
	return collab != nil
}

// RelayHandler handles bidirectional message relay between owner and agent.
type RelayHandler struct {
	ownerStore        *auth.OwnerStore
	collaboratorStore *auth.CollaboratorStore
	channel           *relaytelegram.Adapter
	sessionManager    *relaysession.Manager
	turnDispatcher    turnQueue
	messenger         *messenger.Messenger
	tgClient          client.ClientWithResponsesInterface
	authToken         string
	relayProviderName string
	logger            zerolog.Logger
	authorizer        auth.Authorizer

	mu          sync.RWMutex
	ownerID     int64
	chatID      int64
	botUsername string
	botUserID   int64
	now         func() time.Time
}

type relayHandlerDeps struct {
	fx.In

	LC                 fx.Lifecycle
	OwnerStore         *auth.OwnerStore
	CollaboratorStore  *auth.CollaboratorStore
	Channel            *relaytelegram.Adapter
	SessionManager     *relaysession.Manager
	TurnDispatcher     *TurnDispatcher
	Messenger          *messenger.Messenger
	TGClient           client.ClientWithResponsesInterface
	AuthToken          string `name:"relay_auth_token"`
	RelayProviderID    string `name:"relay_provider"`
	Logger             zerolog.Logger
	InternalMCPManager *InternalMCPManager `optional:"true"`
}

func NewRelayHandler(deps relayHandlerDeps) (*RelayHandler, error) {
	h := &RelayHandler{
		ownerStore:        deps.OwnerStore,
		collaboratorStore: deps.CollaboratorStore,
		channel:           deps.Channel,
		sessionManager:    deps.SessionManager,
		turnDispatcher:    deps.TurnDispatcher,
		messenger:         deps.Messenger,
		tgClient:          deps.TGClient,
		authToken:         strings.TrimSpace(deps.AuthToken),
		relayProviderName: strings.TrimSpace(deps.RelayProviderID),
		logger:            deps.Logger.With().Str("component", "relay.handler").Logger(),
	}
	h.authorizer = &relayAuthorizer{ownerStore: deps.OwnerStore, collaboratorStore: deps.CollaboratorStore}

	deps.LC.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return h.onStart(ctx)
		},
	})

	return h, nil
}

// Register registers the handler with the registry.
func (h *RelayHandler) Register(registry handlers.RegistryInterface) {
	registry.OnMessage(h.onMessage)
	registry.OnMessageType(messagetype.ForumTopicCreated, h.onForumTopicLifecycle)
	registry.OnMessageType(messagetype.ForumTopicEdited, h.onForumTopicLifecycle)
	registry.OnMessageType(messagetype.ForumTopicClosed, h.onForumTopicLifecycle)
	registry.OnMessageType(messagetype.ForumTopicReopened, h.onForumTopicLifecycle)
}

// SetOwner binds the handler to the owner. Pass chatID=0 when the chat
// is not yet known (it will be set from the first incoming message).
func (h *RelayHandler) SetOwner(ownerID, chatID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	log.Info().Int64("owner_id", ownerID).Int64("chat_id", chatID).Msg("Setting owner for relay")

	h.ownerID = ownerID
	if chatID != 0 {
		h.chatID = chatID
	}
}

// SendToOwner sends a message from the agent to the owner.
func (h *RelayHandler) SendToOwner(ctx context.Context, msg string) error {
	chatID := h.getChatID()
	if chatID == 0 {
		return fmt.Errorf("owner not set")
	}

	if err := h.messenger.SendPlain(ctx, chatID, msg, 0); err != nil {
		return fmt.Errorf("sending message: %w", err)
	}
	return nil
}

// ActivateOwner binds owner/chat for relay traffic and bootstraps the owner session.
func (h *RelayHandler) ActivateOwner(ctx context.Context, ownerID, chatID int64) error {
	h.SetOwner(ownerID, chatID)
	return h.bootstrapOwnerSession(ctx, ownerID, chatID)
}

func (h *RelayHandler) bootstrapOwnerSession(ctx context.Context, ownerID, chatID int64) error {
	relayProviderName := h.getProviderName()
	if relayProviderName == "" {
		return fmt.Errorf("relay provider is not configured")
	}

	locator := relaysession.NewTelegramSessionLocator(chatID, 0)
	transportUserID := relaysession.TelegramUserID(ownerID)

	ts, err := h.sessionManager.EnsureSession(ctx, relaysession.SessionContext{
		Locator: locator,
		UserID:  transportUserID,
	}, ownerSessionLabel)
	if err != nil {
		return fmt.Errorf("create owner session: %w", err)
	}

	metadata := h.sessionManager.GetAgentMetadata(relayProviderName)
	welcomeMsg := BuildAgentWelcomeMessage(ownerSessionLabel, ts.GetSessionID(), metadata.Type, metadata.Model, metadata.MCPServers)
	_ = h.channel.SendMarkdown(ctx, locator, welcomeMsg)

	h.logger.Info().
		Int64("owner_id", ownerID).
		Int64("chat_id", chatID).
		Str("agent", relayProviderName).
		Msg("owner session bootstrapped")
	return nil
}

func (h *RelayHandler) onMessage(ctx context.Context, event *events.MessageEvent) error {
	messageCtx, ok := h.channel.MessageContextFromEvent(event)
	if !ok {
		return nil
	}

	h.logger.Debug().
		Str("message_type", string(event.Type)).
		Interface("raw_transport_message", event.Message).
		Msg("received inbound telegram transport message")

	ownerID := h.getOwnerID()
	chatID := h.getChatID()

	if ownerID == 0 {
		return nil
	}

	// RBAC check: owner or collaborator
	if auth.CanAccess(h.authorizer, messageCtx.UserID, auth.ScopeCollaborator) != auth.Allow {
		return nil // Silent drop for unknown users
	}

	if chatID == 0 {
		h.setChatID(messageCtx.ChatID)
		log.Info().Int64("chat_id", messageCtx.ChatID).Msg("Chat ID set from message")
	}

	if messageCtx.HasCommand {
		return nil
	}

	topicID := messageCtx.TopicID
	text := messageCtx.Text
	if !messageCtx.IsDM {
		normalized, ok := h.normalizePublicText(messageCtx)
		if !ok {
			return nil
		}
		text = normalized
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}

	locator := messageCtx.Locator
	transportUserID := relaysession.TelegramUserID(messageCtx.UserID)

	log.Info().Int64("user_id", ownerID).Int("topic_id", topicID).Msg("Relaying message to agent")

	var ts *relaysession.TopicSession
	var err error

	if messageCtx.IsDM && topicID == 0 {
		existingSession, _ := h.sessionManager.GetSession(locator)
		sendOwnerWelcome := existingSession == nil
		relayProviderName := h.getProviderName()
		if relayProviderName == "" {
			_ = h.channel.SendPlain(ctx, locator, "Relay provider is not configured (`relay.provider`). Please close this chat and restart relay.")
			return nil
		}
		ts, err = h.sessionManager.EnsureSession(ctx, relaysession.SessionContext{
			Locator: locator,
			UserID:  transportUserID,
		}, ownerSessionLabel)
		if err != nil {
			log.Error().Err(err).Str("agent", relayProviderName).Msg("failed to ensure owner session")
			_ = h.channel.SendPlain(ctx, locator, fmt.Sprintf("Failed to start owner session: %v.\n\nPlease close this chat and start again.", err))
			return nil
		}
		if sendOwnerWelcome {
			metadata := h.sessionManager.GetAgentMetadata(relayProviderName)
			welcomeMsg := BuildAgentWelcomeMessage(ownerSessionLabel, ts.GetSessionID(), metadata.Type, metadata.Model, metadata.MCPServers)
			_ = h.channel.SendMarkdown(ctx, locator, welcomeMsg)
		}
	} else {
		ts, err = h.sessionManager.GetSession(locator)
		if err != nil {
			_ = h.channel.SendPlain(ctx, locator, "Restoring agent session...")
			ts, err = h.sessionManager.RestoreSession(ctx, relaysession.SessionContext{
				Locator:                    locator,
				UserID:                     transportUserID,
				AllowRelayProviderFallback: false,
			})
			if err != nil {
				if errors.Is(err, relaysession.ErrNoPersistedSession) {
					relayProviderName := h.getProviderName()
					if relayProviderName == "" {
						_ = h.channel.SendPlain(ctx, locator, "Relay provider is not configured (`relay.provider`). Please close this chat and restart relay.")
						return nil
					}
					ts, err = h.sessionManager.EnsureSession(ctx, relaysession.SessionContext{
						Locator: locator,
						UserID:  transportUserID,
					}, autoSessionLabel)
					if err != nil {
						log.Error().Err(err).Str("agent", relayProviderName).Int("topic_id", topicID).Msg("failed to create session")
						_ = h.channel.SendPlain(ctx, locator, fmt.Sprintf("Failed to start session: %v.\n\nPlease close this chat topic and create a new session with /topic <name>.", err))
						return nil
					}
				} else {
					log.Warn().Err(err).Int("topic_id", topicID).Msg("failed to restore session")
					_ = h.channel.SendPlain(ctx, locator, fmt.Sprintf("Failed to restore this session: %v.\n\nPlease close this chat topic and create a new session with /topic <name>.", err))
					return nil
				}
			}
			if ts != nil {
				relayProviderID := h.getProviderName()
				metadata := h.sessionManager.GetAgentMetadata(relayProviderID)
				welcomeName := h.welcomeDisplayName(messageCtx, ts)
				welcomeMsg := BuildAgentWelcomeMessage(welcomeName, ts.GetSessionID(), metadata.Type, metadata.Model, metadata.MCPServers)
				_ = h.channel.SendMarkdown(ctx, locator, welcomeMsg)
			}
		}
	}

	if h.turnDispatcher == nil {
		if err := h.runTurnTask(
			ctx,
			text,
			ts.GetRunner(),
			ts.GetUserID(),
			ts.GetSessionID(),
			ts.GetAgentSessionID(),
			locator,
			messageCtx.MessageID,
			topicID,
			messageCtx.ProgressPolicy,
		); err != nil {
			log.Error().Err(err).Int("topic_id", topicID).Msg("Agent execution failed")
		}
		return nil
	}

	if err := h.enqueueTurn(
		ctx,
		text,
		ts,
		locator,
		messageCtx.MessageID,
		topicID,
		messageCtx.ProgressPolicy,
	); err != nil {
		if errors.Is(err, ErrTurnQueueFull) {
			_ = h.channel.SendPlain(ctx, locator, fmt.Sprintf("Session is busy and queue is full (%d). Please wait or use /cancel.", perSessionQueueLimit))
			return nil
		}

		log.Error().Err(err).Int("topic_id", topicID).Msg("failed to enqueue relay turn")
		_ = h.channel.SendPlain(ctx, locator, "Failed to queue your message for processing. Please try again.")
	}

	return nil
}

func (h *RelayHandler) enqueueTurn(
	ctx context.Context,
	text string,
	ts *relaysession.TopicSession,
	locator relaysession.SessionLocator,
	messageID int,
	topicID int,
	progressPolicy relaychannel.ProgressPolicy,
) error {
	if ts == nil {
		return fmt.Errorf("topic session is required")
	}

	position, err := h.turnDispatcher.Enqueue(TurnTask{
		SessionID: ts.GetSessionID(),
		Run: func(runCtx context.Context) error {
			if _, getErr := h.sessionManager.GetSession(locator); getErr != nil {
				h.logger.Debug().
					Str("session_id", locator.SessionID).
					Str("address_key", locator.AddressKey).
					Msg("dropping queued turn for inactive session")
				return nil
			}
			return h.runTurnTask(
				runCtx,
				text,
				ts.GetRunner(),
				ts.GetUserID(),
				ts.GetSessionID(),
				ts.GetAgentSessionID(),
				locator,
				messageID,
				topicID,
				progressPolicy,
			)
		},
	})
	if err != nil {
		return err
	}

	if position > 0 {
		queuedMsg := fmt.Sprintf("Message queued (position %d). I will process it after the current turn.", position)
		if sendErr := h.channel.SendPlain(ctx, locator, queuedMsg); sendErr != nil {
			h.logger.Warn().
				Err(sendErr).
				Str("session_id", ts.GetSessionID()).
				Int("position", position).
				Msg("failed to send queued message notice")
		}
	}

	return nil
}

func (h *RelayHandler) runTurnTask(
	ctx context.Context,
	text string,
	r *runner.Runner,
	userID string,
	sessionID string,
	agentSessionID string,
	locator relaysession.SessionLocator,
	messageID int,
	topicID int,
	progressPolicy relaychannel.ProgressPolicy,
) error {
	err := h.runTurn(ctx, text, r, userID, sessionID, agentSessionID, locator, messageID, progressPolicy)
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		h.logger.Info().
			Str("session_id", sessionID).
			Int("topic_id", topicID).
			Msg("relay turn canceled")
		return nil
	}
	if _, getErr := h.sessionManager.GetSession(locator); getErr != nil {
		h.logger.Debug().
			Str("session_id", sessionID).
			Int("topic_id", topicID).
			Msg("suppressing relay turn error for inactive session")
		return nil
	}

	log.Error().Err(err).Int("topic_id", topicID).Msg("agent execution failed")
	errText := fmt.Sprintf("Agent execution failed: %v.\n\nPlease close this chat and start a new session.", err)
	if topicID > 0 {
		errText = fmt.Sprintf("Agent execution failed: %v.\n\nPlease close this chat topic and create a new session with /topic <name>.", err)
	}
	if sendErr := h.channel.SendPlain(context.Background(), locator, errText); sendErr != nil {
		log.Warn().Err(sendErr).Int("topic_id", topicID).Msg("failed to send relay error message")
	}
	return err
}

func (h *RelayHandler) onForumTopicLifecycle(_ context.Context, event *events.MessageEvent) error {
	lifecycle, ok := h.channel.TopicLifecycleFromEvent(event)
	if !ok {
		return nil
	}

	chatID := lifecycle.ChatID
	boundChatID := h.getChatID()
	if boundChatID != 0 && chatID != boundChatID {
		return nil
	}

	topicID := lifecycle.TopicID
	if topicID <= 0 {
		h.logger.Debug().
			Int64("chat_id", chatID).
			Str("event_type", string(lifecycle.Type)).
			Msg("ignoring forum topic lifecycle event without topic id")
		return nil
	}

	evt := h.logger.Info().
		Int64("chat_id", chatID).
		Int("topic_id", topicID).
		Int("message_id", lifecycle.MessageID).
		Str("event_type", string(lifecycle.Type))
	if lifecycle.UserID != 0 {
		evt = evt.Int64("user_id", lifecycle.UserID)
	}

	switch lifecycle.Type {
	case messagetype.ForumTopicCreated:
		evt.Msg("forum topic created")
	case messagetype.ForumTopicEdited:
		evt.Msg("forum topic edited")
	case messagetype.ForumTopicClosed:
		evt.Msg("forum topic closed")
	case messagetype.ForumTopicReopened:
		evt.Msg("forum topic reopened")
	default:
		evt.Msg("forum topic lifecycle event")
	}

	return nil
}

func (h *RelayHandler) runTurn(
	ctx context.Context,
	text string,
	r *runner.Runner,
	userID string,
	sessionID string,
	agentSessionID string,
	locator relaysession.SessionLocator,
	messageID int,
	progressPolicy relaychannel.ProgressPolicy,
) error {
	if strings.TrimSpace(agentSessionID) == "" {
		agentSessionID = sessionID
	}

	address, ok, err := locator.TelegramAddress()
	if err != nil {
		return fmt.Errorf("decode telegram locator: %w", err)
	}
	if !ok {
		return fmt.Errorf("unsupported channel type %q", locator.ChannelType)
	}

	chatID := address.ChatID
	topicID := address.TopicID
	userContent := genai.NewContentFromText(text, genai.RoleUser)
	draftID := messageID + 1

	runCtx := zerolog.Ctx(ctx).With().
		Int64("chat_id", chatID).
		Int("topic_id", topicID).
		Str("session_id", sessionID).
		Str("agent_session_id", agentSessionID).
		Str("transport_user_id", userID).
		Logger().
		WithContext(ctx)

	lastFinalResponseText := ""
	lastNonPartialText := ""
	lastPartialText := ""
	hasFinalCandidate := false
	hasFallbackCandidate := false
	hasPartialCandidate := false
	sawTurnComplete := false
	thinkingStages := []string{"Thinking.", "Thinking..", "Thinking..."}
	thinkingIdx := 0
	typingThrottle := throttle.New(telegramTypingThrottleInterval, throttle.WithClock(h.currentTime))

	for ev, err := range r.Run(runCtx, userID, agentSessionID, userContent, agent.RunConfig{}) {
		if err != nil {
			return fmt.Errorf("agent run: %w", err)
		}
		if ev == nil {
			continue
		}
		if !ev.TurnComplete {
			if progressPolicy.Typing {
				typingThrottle.Do(func() {
					if sendErr := h.channel.SendTyping(ctx, locator); sendErr != nil {
						log.Warn().Err(sendErr).Int("topic_id", topicID).Msg("failed to send typing chat action")
					}
				})
			}
			if progressPolicy.Thinking {
				if sendErr := h.channel.SendDraftPlain(ctx, locator, draftID, thinkingStages[thinkingIdx%len(thinkingStages)]); sendErr != nil {
					log.Warn().Err(sendErr).Int("topic_id", topicID).Msg("failed to send thinking draft")
				}
				thinkingIdx++
			}
		}
		contentRole := ""
		partCount := 0
		thoughtPartCount := 0
		textPartCount := 0
		textCharCount := 0
		functionCallPartCount := 0
		functionResponsePartCount := 0
		executableCodePartCount := 0
		codeExecutionResultPartCount := 0
		fileDataPartCount := 0
		inlineDataPartCount := 0
		var eventTextBuilder strings.Builder
		if ev.Content != nil {
			contentRole = ev.Content.Role
			partCount = len(ev.Content.Parts)
			for _, part := range ev.Content.Parts {
				if part == nil {
					continue
				}
				if part.Thought {
					thoughtPartCount++
					continue
				}
				if part.Text != "" {
					textPartCount++
					textCharCount += len(part.Text)
					eventTextBuilder.WriteString(part.Text)
				}
				if part.FunctionCall != nil {
					functionCallPartCount++
				}
				if part.FunctionResponse != nil {
					functionResponsePartCount++
				}
				if part.ExecutableCode != nil {
					executableCodePartCount++
				}
				if part.CodeExecutionResult != nil {
					codeExecutionResultPartCount++
				}
				if part.FileData != nil {
					fileDataPartCount++
				}
				if part.InlineData != nil {
					inlineDataPartCount++
				}
			}
		}
		eventText := eventTextBuilder.String()
		if strings.TrimSpace(eventText) != "" {
			if ev.Partial {
				switch {
				case !hasPartialCandidate:
					lastPartialText = eventText
				case strings.HasPrefix(eventText, lastPartialText):
					// Handle cumulative partial streams by replacing the prior snapshot.
					lastPartialText = eventText
				case strings.HasPrefix(lastPartialText, eventText):
					// Ignore shorter duplicate snapshots.
				default:
					// Handle delta partial streams by appending incoming text.
					lastPartialText += eventText
				}
				hasPartialCandidate = strings.TrimSpace(lastPartialText) != ""
			}
			if !ev.Partial {
				lastNonPartialText = eventText
				hasFallbackCandidate = true
			}
			if ev.IsFinalResponse() {
				lastFinalResponseText = eventText
				hasFinalCandidate = true
			}
		}
		zerolog.Ctx(runCtx).Debug().
			Str("event_id", ev.ID).
			Str("event_invocation_id", ev.InvocationID).
			Str("event_author", ev.Author).
			Str("event_branch", ev.Branch).
			Bool("partial", ev.Partial).
			Bool("interrupted", ev.Interrupted).
			Bool("turn_complete", ev.TurnComplete).
			Bool("has_content", ev.Content != nil).
			Str("content_role", contentRole).
			Int("part_count", partCount).
			Int("thought_part_count", thoughtPartCount).
			Int("text_part_count", textPartCount).
			Int("text_char_count", textCharCount).
			Int("function_call_part_count", functionCallPartCount).
			Int("function_response_part_count", functionResponsePartCount).
			Int("executable_code_part_count", executableCodePartCount).
			Int("code_execution_result_part_count", codeExecutionResultPartCount).
			Int("file_data_part_count", fileDataPartCount).
			Int("inline_data_part_count", inlineDataPartCount).
			Str("error_code", strings.TrimSpace(ev.ErrorCode)).
			Bool("has_error_message", strings.TrimSpace(ev.ErrorMessage) != "").
			Interface("finish_reason", ev.FinishReason).
			Int("long_running_tool_ids_count", len(ev.LongRunningToolIDs)).
			Int("state_delta_count", len(ev.Actions.StateDelta)).
			Int("artifact_delta_count", len(ev.Actions.ArtifactDelta)).
			Int("requested_tool_confirmations_count", len(ev.Actions.RequestedToolConfirmations)).
			Bool("skip_summarization", ev.Actions.SkipSummarization).
			Str("transfer_to_agent", strings.TrimSpace(ev.Actions.TransferToAgent)).
			Bool("escalate", ev.Actions.Escalate).
			Bool("final_candidate_present", hasFinalCandidate).
			Bool("fallback_candidate_present", hasFallbackCandidate).
			Bool("partial_candidate_present", hasPartialCandidate).
			Int("partial_candidate_char_count", len(lastPartialText)).
			Msg("received ACP event")
		if ev.TurnComplete {
			sawTurnComplete = true
			responseText := ""
			responseSource := "none"
			switch {
			case hasFinalCandidate:
				responseText = lastFinalResponseText
				responseSource = "final_response"
			case hasFallbackCandidate:
				responseText = lastNonPartialText
				responseSource = "non_partial_fallback"
			case hasPartialCandidate:
				responseText = lastPartialText
				responseSource = "partial_text_fallback"
			}
			responseEmitted := false
			if strings.TrimSpace(responseText) != "" {
				if sendErr := h.channel.SendAgentReply(ctx, locator, responseText); sendErr != nil {
					log.Warn().Err(sendErr).Int("topic_id", topicID).Msg("failed to send relay response")
				} else {
					responseEmitted = true
				}
			}
			zerolog.Ctx(runCtx).Debug().
				Str("response_source", responseSource).
				Bool("response_emitted_on_turn_complete", responseEmitted).
				Msg("processed turn complete event")
			break
		}
	}
	if !sawTurnComplete {
		zerolog.Ctx(runCtx).Warn().
			Bool("final_candidate_present", hasFinalCandidate).
			Bool("fallback_candidate_present", hasFallbackCandidate).
			Bool("partial_candidate_present", hasPartialCandidate).
			Msg("ACP stream ended without turn complete; suppressing relay response")
	}

	return nil
}

func (h *RelayHandler) getOwnerID() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.ownerID
}

func (h *RelayHandler) getChatID() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.chatID
}

func (h *RelayHandler) setChatID(chatID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.chatID = chatID
}

func (h *RelayHandler) onStart(ctx context.Context) error {
	if err := h.initializeBotUsername(ctx); err != nil {
		return fmt.Errorf("resolve relay telegram bot identity: %w", err)
	}

	if !h.ownerStore.HasOwner() {
		return nil
	}
	owner := h.ownerStore.GetOwner()
	if owner == nil {
		return nil
	}
	if owner.ChatID == 0 {
		return fmt.Errorf("owner.chat_id is required for relay startup; run /start to bind owner chat")
	}

	h.SetOwner(owner.UserID, owner.ChatID)

	if err := h.bootstrapOwnerSession(ctx, owner.UserID, owner.ChatID); err != nil {
		h.logger.Error().Err(err).Int64("owner_id", owner.UserID).Msg("failed to bootstrap owner session during startup")
		if sendErr := h.messenger.SendPlain(ctx, owner.UserID, fmt.Sprintf("Failed to start owner session: %v.\nPlease check relay configuration.", err), 0); sendErr != nil {
			h.logger.Warn().Err(sendErr).Msg("failed to send owner session failure message")
		}
		return nil
	}

	if err := h.messenger.SendPlain(ctx, owner.UserID, "Boss, I'm online and ready to work.", 0); err != nil {
		h.logger.Warn().Err(err).Int64("owner_id", owner.UserID).Msg("failed to send startup ready message to owner")
		return nil
	}
	h.logger.Info().Int64("owner_id", owner.UserID).Msg("startup ready message sent to owner")
	return nil
}

func (h *RelayHandler) initializeBotUsername(ctx context.Context) error {
	if h.tgClient == nil {
		return fmt.Errorf("telegram client is required")
	}

	meResp, err := h.tgClient.GetMeWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("getMe: %w", err)
	}
	if meResp == nil {
		return fmt.Errorf("getMe response is nil")
	}
	if meResp.JSON200 == nil {
		if meResp.JSON401 != nil {
			return fmt.Errorf("getMe unauthorized: %s", strings.TrimSpace(meResp.JSON401.Description))
		}
		if meResp.JSON400 != nil {
			return fmt.Errorf("getMe bad request: %s", strings.TrimSpace(meResp.JSON400.Description))
		}
		return fmt.Errorf("getMe response missing result (status %s)", strings.TrimSpace(meResp.Status()))
	}
	botUserID := meResp.JSON200.Result.Id
	if botUserID == 0 {
		return fmt.Errorf("getMe returned empty bot id")
	}

	username := ""
	if meResp.JSON200.Result.Username != nil {
		username = strings.TrimSpace(*meResp.JSON200.Result.Username)
	}
	if username == "" {
		return fmt.Errorf("getMe returned empty username for bot id %d", botUserID)
	}

	h.mu.Lock()
	h.botUserID = botUserID
	h.botUsername = username
	h.mu.Unlock()

	if h.authToken != "" {
		deeplink := fmt.Sprintf("https://t.me/%s?start=%s", username, h.authToken)
		h.logger.Info().Int64("bot_user_id", botUserID).Str("bot_username", username).Str("start_deeplink", deeplink).Msg("relay start deeplink ready")
		return nil
	}
	h.logger.Info().Int64("bot_user_id", botUserID).Str("bot_username", username).Msg("relay bot identity loaded")
	return nil
}

func (h *RelayHandler) getProviderName() string {
	providerName := strings.TrimSpace(h.sessionManager.RelayProviderID())
	if providerName == "" {
		h.mu.RLock()
		defer h.mu.RUnlock()
		providerName = strings.TrimSpace(h.relayProviderName)
	}
	return providerName
}

func (h *RelayHandler) welcomeDisplayName(messageCtx relaytelegram.MessageContext, ts *relaysession.TopicSession) string {
	if !messageCtx.IsDM {
		return ownerSessionLabel
	}
	if ts == nil {
		return ""
	}
	return ts.GetAgentName()
}

func (h *RelayHandler) normalizePublicText(messageCtx relaytelegram.MessageContext) (string, bool) {
	botUserID, botUsername := h.getBotIdentity()

	if botUsername != "" {
		mentionRanges := botMentionEntityRanges(messageCtx.Text, messageCtx.Entities, botUsername)
		if len(mentionRanges) > 0 {
			userMessage := strings.TrimSpace(removeTextByUTF16Ranges(messageCtx.Text, mentionRanges))
			replyContent := strings.TrimSpace(messageCtx.ReplyContent)
			return composeMentionTriggeredInput(userMessage, replyContent)
		}
	}

	if !messageCtx.IsReply || !messageCtx.ReplyToIsBot || botUserID == 0 {
		return "", false
	}

	if messageCtx.ReplyToUserID != botUserID {
		return "", false
	}

	return messageCtx.Text, true
}

func composeMentionTriggeredInput(userMessage, replyContent string) (string, bool) {
	switch {
	case replyContent != "" && userMessage != "":
		return fmt.Sprintf("Reply context:\n%s\n\nUser message:\n%s", replyContent, userMessage), true
	case replyContent != "":
		return fmt.Sprintf("Reply context:\n%s", replyContent), true
	case userMessage != "":
		return userMessage, true
	default:
		return "", false
	}
}

func (h *RelayHandler) currentTime() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

func (h *RelayHandler) getBotIdentity() (int64, string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.botUserID, h.botUsername
}

type utf16Range struct {
	start int
	end   int
}

func botMentionEntityRanges(text string, entities []client.MessageEntity, botUsername string) []utf16Range {
	trimmedUsername := strings.TrimSpace(botUsername)
	if trimmedUsername == "" || len(entities) == 0 || strings.TrimSpace(text) == "" {
		return nil
	}
	expectedMention := "@" + trimmedUsername
	ranges := make([]utf16Range, 0, len(entities))
	for _, entity := range entities {
		if entity.Type != "mention" {
			continue
		}
		if entity.Length <= 0 || entity.Offset < 0 {
			continue
		}
		mentionText, ok := utf16TextSlice(text, entity.Offset, entity.Length)
		if !ok {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(mentionText), expectedMention) {
			continue
		}
		ranges = append(ranges, utf16Range{
			start: entity.Offset,
			end:   entity.Offset + entity.Length,
		})
	}
	return ranges
}

func removeTextByUTF16Ranges(text string, ranges []utf16Range) string {
	if len(ranges) == 0 || text == "" {
		return text
	}
	runes := []rune(text)
	type runeRange struct {
		start int
		end   int
	}
	runeRanges := make([]runeRange, 0, len(ranges))
	for _, r := range ranges {
		start, ok := utf16OffsetToRuneIndex(runes, r.start)
		if !ok {
			continue
		}
		end, ok := utf16OffsetToRuneIndex(runes, r.end)
		if !ok || end <= start {
			continue
		}
		runeRanges = append(runeRanges, runeRange{start: start, end: end})
	}
	if len(runeRanges) == 0 {
		return text
	}
	sort.Slice(runeRanges, func(i, j int) bool {
		if runeRanges[i].start == runeRanges[j].start {
			return runeRanges[i].end < runeRanges[j].end
		}
		return runeRanges[i].start < runeRanges[j].start
	})
	merged := make([]runeRange, 0, len(runeRanges))
	for _, rr := range runeRanges {
		if len(merged) == 0 {
			merged = append(merged, rr)
			continue
		}
		last := &merged[len(merged)-1]
		if rr.start > last.end {
			merged = append(merged, rr)
			continue
		}
		if rr.end > last.end {
			last.end = rr.end
		}
	}

	var out strings.Builder
	prevEnd := 0
	for _, rr := range merged {
		if rr.start > prevEnd {
			out.WriteString(string(runes[prevEnd:rr.start]))
		}
		prevEnd = rr.end
	}
	if prevEnd < len(runes) {
		out.WriteString(string(runes[prevEnd:]))
	}
	return out.String()
}

func utf16TextSlice(text string, offset, length int) (string, bool) {
	if length <= 0 || offset < 0 {
		return "", false
	}
	runes := []rune(text)
	start, ok := utf16OffsetToRuneIndex(runes, offset)
	if !ok {
		return "", false
	}
	end, ok := utf16OffsetToRuneIndex(runes, offset+length)
	if !ok || end < start {
		return "", false
	}
	return string(runes[start:end]), true
}

func utf16OffsetToRuneIndex(runes []rune, targetOffset int) (int, bool) {
	if targetOffset < 0 {
		return 0, false
	}
	units := 0
	for idx, r := range runes {
		if units == targetOffset {
			return idx, true
		}
		units += utf16UnitsForRune(r)
	}
	if units == targetOffset {
		return len(runes), true
	}
	return 0, false
}

func utf16UnitsForRune(r rune) int {
	if r > 0xFFFF {
		return 2
	}
	return 1
}

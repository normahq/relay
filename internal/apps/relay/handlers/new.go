package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/normahq/relay/internal/apps/relay/auth"
	relaytelegram "github.com/normahq/relay/internal/apps/relay/channel/telegram"
	"github.com/normahq/relay/internal/apps/relay/messenger"
	"github.com/normahq/relay/internal/apps/relay/session"
	relaywelcome "github.com/normahq/relay/internal/apps/relay/welcome"
	"github.com/rs/zerolog/log"
	"github.com/tgbotkit/runtime/events"
	"github.com/tgbotkit/runtime/handlers"
	"go.uber.org/fx"
)

type commandSessionManager interface {
	CreateSession(ctx context.Context, sessionCtx session.SessionContext, agentName string) error
	GetAgentInfo(agentName string) (string, []string)
	ProviderIDs() []string
	RootProviderID() string
	StopSession(locator session.SessionLocator)
	ValidateAgent(agentName string) error
}

// CommandHandler handles relay commands like /new and /close.
type CommandHandler struct {
	ownerStore     *auth.OwnerStore
	channel        *relaytelegram.Adapter
	sessionManager commandSessionManager
	turnDispatcher turnQueue
	messenger      *messenger.Messenger
	userHandler    *userHandler
}

func BuildAgentWelcomeMessage(agentName, sessionID, agentDesc string, mcpServers []string) string {
	return relaywelcome.BuildAgentWelcomeMessage(agentName, sessionID, agentDesc, mcpServers)
}

type commandHandlerParams struct {
	fx.In

	OwnerStore     *auth.OwnerStore
	Channel        *relaytelegram.Adapter
	SessionManager *session.Manager
	TurnDispatcher *TurnDispatcher
	Messenger      *messenger.Messenger
	UserHandler    *userHandler
}

// NewCommandHandler creates a new relay command handler.
func NewCommandHandler(params commandHandlerParams) *CommandHandler {
	return &CommandHandler{
		ownerStore:     params.OwnerStore,
		channel:        params.Channel,
		sessionManager: params.SessionManager,
		turnDispatcher: params.TurnDispatcher,
		messenger:      params.Messenger,
		userHandler:    params.UserHandler,
	}
}

// Register registers the handler with the registry.
func (h *CommandHandler) Register(registry handlers.RegistryInterface) {
	registry.OnCommand(h.onCommand)
}

func (h *CommandHandler) onCommand(ctx context.Context, event *events.CommandEvent) error {
	commandCtx, ok := h.channel.CommandContextFromEvent(event)
	if !ok {
		return nil
	}

	switch commandCtx.Command {
	case "new":
		return h.onNewCommand(ctx, commandCtx)
	case "close":
		return h.onCloseCommand(ctx, commandCtx)
	case "cancel":
		return h.onCancelCommand(ctx, commandCtx)
	case "user":
		// Route to UserHandler
		return h.userHandler.HandleUserCommand(ctx, commandCtx)
	default:
		return nil
	}
}

func (h *CommandHandler) onNewCommand(ctx context.Context, commandCtx relaytelegram.CommandContext) error {
	if !h.ownerStore.HasOwner() || !h.ownerStore.IsOwner(commandCtx.UserID) {
		if err := h.channel.SendPlain(ctx, commandCtx.Locator, "Only the bot owner can use this command."); err != nil {
			return err
		}
		return nil
	}

	if !commandCtx.IsDM {
		if err := h.channel.SendPlain(ctx, commandCtx.Locator, "This command is only available in direct messages."); err != nil {
			return err
		}
		return nil
	}

	providerID := strings.TrimSpace(commandCtx.Args)
	if providerID == "" {
		providerID = h.sessionManager.RootProviderID()
	}
	if providerID == "" {
		if err := h.channel.SendPlain(ctx, commandCtx.Locator, h.newCommandUsageMessage(true)); err != nil {
			return err
		}
		return nil
	}

	log.Info().
		Int64("user_id", commandCtx.UserID).
		Int64("chat_id", commandCtx.ChatID).
		Str("provider", providerID).
		Msg("Creating new topic with provider")

	if err := h.sessionManager.ValidateAgent(providerID); err != nil {
		log.Error().Err(err).Str("provider", providerID).Msg("provider validation failed, not creating topic")
		if sendErr := h.channel.SendPlain(ctx, commandCtx.Locator, fmt.Sprintf("Failed to create provider session: provider %q not available: %v", providerID, err)); sendErr != nil {
			return sendErr
		}
		return nil
	}

	topicLocator, err := h.channel.CreateTopicLocator(ctx, commandCtx.ChatID, fmt.Sprintf("Relay: %s", providerID))
	if err != nil {
		log.Error().Err(err).Str("provider", providerID).Msg("Failed to create topic with provider")
		if sendErr := h.channel.SendPlain(ctx, commandCtx.Locator, fmt.Sprintf("Failed to create provider session: %v", err)); sendErr != nil {
			return sendErr
		}
		return nil
	}
	if err := h.sessionManager.CreateSession(ctx, session.SessionContext{
		Locator: topicLocator,
		UserID:  session.TelegramUserID(commandCtx.UserID),
	}, providerID); err != nil {
		log.Error().Err(err).Str("provider", providerID).Msg("Failed to create provider session after topic creation")
		_ = h.channel.Close(ctx, topicLocator)
		if sendErr := h.channel.SendPlain(ctx, commandCtx.Locator, fmt.Sprintf("Failed to create provider session: %v", err)); sendErr != nil {
			return sendErr
		}
		return nil
	}

	agentDesc, mcpServers := h.sessionManager.GetAgentInfo(providerID)

	welcomeMsg := BuildAgentWelcomeMessage(providerID, topicLocator.SessionID, agentDesc, mcpServers)
	if err := h.channel.SendMarkdown(ctx, topicLocator, welcomeMsg); err != nil {
		log.Error().Err(err).Msg("Failed to send welcome message")
		return err
	}

	return nil
}

func (h *CommandHandler) onCloseCommand(ctx context.Context, commandCtx relaytelegram.CommandContext) error {
	if !h.ownerStore.HasOwner() || !h.ownerStore.IsOwner(commandCtx.UserID) {
		if err := h.channel.SendPlain(ctx, commandCtx.Locator, "Only the bot owner can use this command."); err != nil {
			return err
		}
		return nil
	}

	if !commandCtx.IsDM {
		if err := h.channel.SendPlain(ctx, commandCtx.Locator, "This command is only available in direct messages."); err != nil {
			return err
		}
		return nil
	}

	if strings.TrimSpace(commandCtx.Args) != "" {
		if err := h.channel.SendPlain(ctx, commandCtx.Locator, "Usage: /close"); err != nil {
			return err
		}
		return nil
	}

	if commandCtx.TopicID > 0 {
		if h.turnDispatcher != nil {
			_, _, _ = h.turnDispatcher.CancelSession(commandCtx.Locator, true)
		}
		if err := h.channel.SendPlain(ctx, commandCtx.Locator, "Closing this topic and stopping agent session."); err != nil {
			log.Warn().Err(err).Int64("chat_id", commandCtx.ChatID).Int("topic_id", commandCtx.TopicID).Msg("failed to send /close confirmation")
		}
		if err := h.channel.Close(ctx, commandCtx.Locator); err != nil {
			log.Warn().Err(err).Int64("chat_id", commandCtx.ChatID).Int("topic_id", commandCtx.TopicID).Msg("failed to close topic")
		}
		h.sessionManager.StopSession(commandCtx.Locator)
		return nil
	}

	if h.turnDispatcher != nil {
		_, _, _ = h.turnDispatcher.CancelSession(commandCtx.Locator, true)
	}
	if err := h.channel.SendPlain(ctx, commandCtx.Locator, "Stopping root provider session. It will be recreated on your next message."); err != nil {
		log.Warn().Err(err).Int64("chat_id", commandCtx.ChatID).Msg("failed to send /close root confirmation")
	}
	h.sessionManager.StopSession(commandCtx.Locator)
	return nil
}

func (h *CommandHandler) onCancelCommand(ctx context.Context, commandCtx relaytelegram.CommandContext) error {
	if !h.ownerStore.HasOwner() || !h.ownerStore.IsOwner(commandCtx.UserID) {
		if err := h.channel.SendPlain(ctx, commandCtx.Locator, "Only the bot owner can use this command."); err != nil {
			return err
		}
		return nil
	}

	if strings.TrimSpace(commandCtx.Args) != "" {
		if err := h.channel.SendPlain(ctx, commandCtx.Locator, "Usage: /cancel"); err != nil {
			return err
		}
		return nil
	}

	if h.turnDispatcher == nil {
		if err := h.channel.SendPlain(ctx, commandCtx.Locator, "Cancel is unavailable right now. Please try again."); err != nil {
			return err
		}
		return nil
	}

	hadInFlight, dropped, err := h.turnDispatcher.CancelSession(commandCtx.Locator, true)
	if err != nil {
		log.Warn().Err(err).Str("session_id", commandCtx.Locator.SessionID).Msg("failed to cancel session turns")
		if sendErr := h.channel.SendPlain(ctx, commandCtx.Locator, fmt.Sprintf("Failed to cancel current turn: %v", err)); sendErr != nil {
			return sendErr
		}
		return nil
	}

	if !hadInFlight && dropped == 0 {
		if err := h.channel.SendPlain(ctx, commandCtx.Locator, "No running or queued turns for this session."); err != nil {
			return err
		}
		return nil
	}

	response := "Canceled current turn."
	if !hadInFlight {
		response = "No running turn to cancel."
	}
	if dropped > 0 {
		response = fmt.Sprintf("%s Dropped %d queued message(s).", response, dropped)
	}
	if err := h.channel.SendPlain(ctx, commandCtx.Locator, response); err != nil {
		return err
	}
	return nil
}

func (h *CommandHandler) newCommandUsageMessage(rootMissing bool) string {
	usage := "Usage: /new [provider_id]"
	providerIDs := h.sessionManager.ProviderIDs()
	if len(providerIDs) == 0 {
		if rootMissing {
			return usage + "\n\nrelay.provider is not configured.\nNo providers configured under runtime.providers in relay config."
		}
		return usage + "\n\nNo providers configured under runtime.providers in relay config."
	}

	lines := []string{usage}
	if rootMissing {
		lines = append(lines, "", "relay.provider is not configured.")
	} else if rootProviderID := strings.TrimSpace(h.sessionManager.RootProviderID()); rootProviderID != "" {
		lines = append(lines, "", "Default provider: "+rootProviderID)
	}
	lines = append(lines, "", "Available providers: "+strings.Join(providerIDs, ", "))
	return strings.Join(lines, "\n")
}

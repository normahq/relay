package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	relaytelegram "github.com/normahq/relay/internal/apps/relay/channel/telegram"
	relaysession "github.com/normahq/relay/internal/apps/relay/session"
	"github.com/rs/zerolog/log"
)

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
		h.logger.Info().Int64("bot_user_id", botUserID).Str("bot_username", username).Bool("owner_auth_available", true).Msg("relay owner auth available")
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

package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/normahq/relay/internal/apps/relay/auth"
	"github.com/normahq/relay/internal/apps/relay/messenger"
	relaystate "github.com/normahq/relay/internal/apps/relay/state"
	relaywelcome "github.com/normahq/relay/internal/apps/relay/welcome"
	"github.com/normahq/relay/internal/apps/relaymcp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type relayChannelRuntime interface {
	CreateTopicLocator(ctx context.Context, chatID int64, topicName string) (SessionLocator, error)
	Close(ctx context.Context, locator SessionLocator) error
	SendMarkdown(ctx context.Context, locator SessionLocator, text string) error
}

type relayMCPServer struct {
	manager   *Manager
	channel   relayChannelRuntime
	messenger *messenger.Messenger
	owners    relayOwnerStore
	logger    zerolog.Logger
}

type relayOwnerStore interface {
	GetOwner() *auth.Owner
}

// NewRelayMCPServer wraps a session Manager as a RelayService.
func NewRelayMCPServer(manager *Manager, channel relayChannelRuntime, msg *messenger.Messenger, owners relayOwnerStore) relaymcp.RelayService {
	return &relayMCPServer{
		manager:   manager,
		channel:   channel,
		messenger: msg,
		owners:    owners,
		logger:    log.With().Str("component", "relay.mcp").Logger(),
	}
}

type relayActorContext struct {
	SessionID string
	UserID    string
	ChatID    int64
	IsOwner   bool
}

func (s *relayMCPServer) StartAgent(ctx context.Context, req relaymcp.StartRequest) (relaymcp.AgentInfo, error) {
	agentName := strings.TrimSpace(req.AgentName)
	if agentName == "" {
		return relaymcp.AgentInfo{}, fmt.Errorf("agent_name is required")
	}

	actorCtx, err := s.resolveActorContext(ctx, req.CallerSessionID)
	if err != nil {
		return relaymcp.AgentInfo{}, err
	}

	targetLocator, err := sessionLocatorFromStartLocator(req.Locator)
	if err != nil {
		return relaymcp.AgentInfo{}, err
	}
	targetAddress, ok, err := targetLocator.TelegramAddress()
	if err != nil {
		return relaymcp.AgentInfo{}, err
	}
	if !ok {
		return relaymcp.AgentInfo{}, fmt.Errorf("unsupported channel type %q", targetLocator.ChannelType)
	}
	if targetAddress.ChatID != actorCtx.ChatID {
		return relaymcp.AgentInfo{}, fmt.Errorf("locator.address.chat_id %d is outside caller session chat scope", targetAddress.ChatID)
	}

	s.logger.Info().
		Str("caller_session_id", actorCtx.SessionID).
		Str("caller_user_id", actorCtx.UserID).
		Int64("chat_id", targetAddress.ChatID).
		Str("agent", agentName).
		Msg("MCP: relay.agents.start called")

	if err := s.manager.ValidateAgent(agentName); err != nil {
		return relaymcp.AgentInfo{}, fmt.Errorf("agent %q not available: %w", agentName, err)
	}

	locator, err := s.channel.CreateTopicLocator(ctx, targetAddress.ChatID, fmt.Sprintf("Relay: %s", agentName))
	if err != nil {
		s.logger.Error().
			Err(err).
			Int64("chat_id", targetAddress.ChatID).
			Str("agent", agentName).
			Msg("MCP: relay.agents.start failed")
		return relaymcp.AgentInfo{}, err
	}
	if err := s.manager.CreateSession(ctx, SessionContext{
		Locator: locator,
		UserID:  actorCtx.UserID,
	}, agentName); err != nil {
		_ = s.channel.Close(ctx, locator)
		return relaymcp.AgentInfo{}, err
	}

	address, ok, err := locator.TelegramAddress()
	if err != nil {
		return relaymcp.AgentInfo{}, err
	}
	if !ok {
		return relaymcp.AgentInfo{}, fmt.Errorf("unsupported channel type %q", locator.ChannelType)
	}
	agentDesc, mcpServers := s.manager.GetAgentInfo(agentName)

	if s.messenger != nil {
		welcomeMsg := relaywelcome.BuildAgentWelcomeMessage(agentName, locator.SessionID, agentDesc, mcpServers)
		if sendErr := s.channel.SendMarkdown(ctx, locator, welcomeMsg); sendErr != nil {
			s.logger.Warn().
				Err(sendErr).
				Int64("chat_id", targetAddress.ChatID).
				Int("topic_id", address.TopicID).
				Str("agent", agentName).
				Str("session_id", locator.SessionID).
				Msg("MCP: failed to send welcome message to topic")
		}
	}

	s.logger.Info().
		Int64("chat_id", targetAddress.ChatID).
		Int("topic_id", address.TopicID).
		Str("agent", agentName).
		Str("session_id", locator.SessionID).
		Msg("MCP: relay.agents.start succeeded")

	return relaymcp.AgentInfo{
		ChannelType: locator.ChannelType,
		AddressKey:  locator.AddressKey,
		SessionID:   locator.SessionID,
		UserID:      actorCtx.UserID,
		AgentName:   agentName,
		ChatID:      targetAddress.ChatID,
		TopicID:     address.TopicID,
		Description: agentDesc,
		MCPServers:  mcpServers,
	}, nil
}

func (s *relayMCPServer) resolveActorContext(ctx context.Context, callerSessionID string) (relayActorContext, error) {
	trimmedSessionID := strings.TrimSpace(callerSessionID)
	if trimmedSessionID == "" {
		return relayActorContext{}, fmt.Errorf("caller session identity is required")
	}

	info, err := s.manager.GetSessionInfo(ctx, trimmedSessionID)
	if err != nil {
		return relayActorContext{}, fmt.Errorf("resolve caller session context: %w", err)
	}

	address, ok, err := info.Locator.TelegramAddress()
	if err != nil {
		return relayActorContext{}, fmt.Errorf("decode caller session locator: %w", err)
	}
	if !ok {
		return relayActorContext{}, fmt.Errorf("unsupported caller channel type %q", info.ChannelType)
	}

	userID := strings.TrimSpace(info.UserID)
	if userID == "" {
		return relayActorContext{}, fmt.Errorf("caller session %q has no user identity", trimmedSessionID)
	}

	owner := s.owner()
	return relayActorContext{
		SessionID: trimmedSessionID,
		UserID:    userID,
		ChatID:    address.ChatID,
		IsOwner:   owner != nil && userID == TelegramUserID(owner.UserID),
	}, nil
}

func authorizeActorForSessionMutation(actor relayActorContext, target relaymcp.AgentInfo) error {
	targetUserID := strings.TrimSpace(target.UserID)
	if targetUserID == "" {
		if actor.IsOwner {
			return nil
		}
		return fmt.Errorf("session %q has unknown owner; only relay owner can mutate it", target.SessionID)
	}
	if targetUserID != actor.UserID {
		return fmt.Errorf("session %q is owned by another actor", target.SessionID)
	}
	return nil
}

func sessionLocatorFromStartLocator(locator *relaymcp.StartLocator) (SessionLocator, error) {
	if locator == nil {
		return SessionLocator{}, fmt.Errorf("locator is required")
	}
	switch strings.TrimSpace(locator.ChannelType) {
	case relaystate.ChannelTypeTelegram:
		raw, err := json.Marshal(locator.Address)
		if err != nil {
			return SessionLocator{}, fmt.Errorf("marshal locator.address: %w", err)
		}
		var address TelegramAddress
		if err := json.Unmarshal(raw, &address); err != nil {
			return SessionLocator{}, fmt.Errorf("decode Telegram locator.address: %w", err)
		}
		if address.ChatID == 0 {
			return SessionLocator{}, fmt.Errorf("telegram locator.address.chat_id is required")
		}
		if address.TopicID != 0 {
			return SessionLocator{}, fmt.Errorf("telegram locator.address.topic_id must be omitted or 0 for relay.agents.start")
		}
		return NewTelegramSessionLocator(address.ChatID, 0), nil
	case "":
		return SessionLocator{}, fmt.Errorf("locator.channel_type is required")
	default:
		return SessionLocator{}, fmt.Errorf("unsupported locator.channel_type %q", locator.ChannelType)
	}
}

func (s *relayMCPServer) owner() *auth.Owner {
	if s.owners == nil {
		return nil
	}
	return s.owners.GetOwner()
}

func (s *relayMCPServer) StopAgent(ctx context.Context, req relaymcp.StopRequest) error {
	sessionID := strings.TrimSpace(req.SessionID)
	callerSessionID := strings.TrimSpace(req.CallerSessionID)
	s.logger.Info().
		Str("session_id", sessionID).
		Str("caller_session_id", callerSessionID).
		Msg("MCP: relay.agents.stop called")

	actorCtx, err := s.resolveActorContext(ctx, callerSessionID)
	if err != nil {
		s.logger.Error().Err(err).Str("session_id", sessionID).Msg("MCP: relay.agents.stop failed")
		return err
	}
	info, err := s.GetSession(ctx, sessionID)
	if err != nil {
		s.logger.Error().Err(err).Str("session_id", sessionID).Msg("MCP: relay.agents.stop failed")
		return err
	}
	if err := authorizeActorForSessionMutation(actorCtx, info); err != nil {
		s.logger.Error().Err(err).Str("session_id", sessionID).Msg("MCP: relay.agents.stop denied")
		return err
	}

	if err := s.manager.StopSessionByID(ctx, sessionID); err != nil {
		s.logger.Error().Err(err).Str("session_id", sessionID).Msg("MCP: relay.agents.stop failed")
		return err
	}

	s.logger.Info().Str("session_id", sessionID).Msg("MCP: relay.agents.stop succeeded")
	return nil
}

func (s *relayMCPServer) ListAgents(ctx context.Context) ([]relaymcp.AgentInfo, error) {
	infos, err := s.manager.ListSessionInfos(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]relaymcp.AgentInfo, 0, len(infos))

	s.logger.Debug().Int("count", len(infos)).Msg("MCP: relay.agents.list called")

	for _, info := range infos {
		out = append(out, relaymcp.AgentInfo{
			ChannelType: info.ChannelType,
			AddressKey:  info.Locator.AddressKey,
			SessionID:   info.SessionID,
			UserID:      info.UserID,
			AgentName:   info.AgentName,
			ChatID:      info.ChatID,
			TopicID:     info.TopicID,
			WorkingDir:  info.WorkspaceDir,
			Status:      info.Status,
		})
	}
	return out, nil
}

func (s *relayMCPServer) GetSession(ctx context.Context, sessionID string) (relaymcp.AgentInfo, error) {
	s.logger.Debug().Str("session_id", sessionID).Msg("MCP: relay.agents.get called")

	info, err := s.manager.GetSessionInfo(ctx, sessionID)
	if err != nil {
		s.logger.Warn().Err(err).Str("session_id", sessionID).Msg("MCP: relay.agents.get failed - session not found")
		return relaymcp.AgentInfo{}, err
	}

	return relaymcp.AgentInfo{
		ChannelType: info.ChannelType,
		AddressKey:  info.Locator.AddressKey,
		SessionID:   info.SessionID,
		UserID:      info.UserID,
		AgentName:   info.AgentName,
		ChatID:      info.ChatID,
		TopicID:     info.TopicID,
		WorkingDir:  info.WorkspaceDir,
		Status:      info.Status,
	}, nil
}

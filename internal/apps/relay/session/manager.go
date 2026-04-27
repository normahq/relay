package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	relayagent "github.com/normahq/relay/internal/apps/relay/agent"
	relaystate "github.com/normahq/relay/internal/apps/relay/state"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
	adksession "google.golang.org/adk/session"
)

const cleanupTimeout = 10 * time.Second

const sessionStatusPersisted = "persisted"

var ErrNoPersistedSession = errors.New("no persisted session")

type agentBuilder interface {
	CreateRuntimeSession(
		ctx context.Context,
		runtime *relayagent.BuiltRuntime,
		agentName string,
		userID string,
		sessionID string,
		workspaceDir string,
	) (adksession.Session, error)
	ValidateAgent(agentName string) error
	GetAgentInfo(agentName string) (string, []string)
	GetAgentMetadata(agentName string) relayagent.AgentMetadata
	ProviderIDs() []string
}

type relayRuntimeManager interface {
	Runtime(ctx context.Context) (*relayagent.BuiltRuntime, error)
	ProviderID() string
}

type AgentMetadata = relayagent.AgentMetadata

// Manager manages relay ADK sessions and persists session metadata.
type Manager struct {
	agentBuilder      agentBuilder
	runtimeManager    relayRuntimeManager
	relayMCPServerIDs []string
	relayProviderName string
	workingDir        string
	workspaces        *relayagent.WorkspaceManager
	workspaceEnabled  bool
	workspaceBaseRef  string
	sessionStore      relaystate.SessionStore
	logger            zerolog.Logger

	mu              sync.RWMutex
	sessions        map[string]*TopicSession
	agentSessionSeq uint64
}

// ManagerParams provides dependencies for Manager.
type ManagerParams struct {
	fx.In

	LC                fx.Lifecycle
	AgentBuilder      *relayagent.Builder
	RuntimeManager    *relayagent.RuntimeManager
	RelayMCPServerIDs []string `name:"relay_mcp_servers"`
	RelayProviderID   string   `name:"relay_provider"`
	WorkingDir        string
	StateDir          string `name:"relay_state_dir"`
	WorkspaceEnabled  bool   `name:"relay_workspace_enabled"`
	WorkspaceBaseRef  string `name:"relay_workspace_base_branch"`
	StateProvider     relaystate.Provider
	Logger            zerolog.Logger
}

// NewManager creates a session Manager.
func NewManager(p ManagerParams) (*Manager, error) {
	if p.StateProvider == nil {
		return nil, fmt.Errorf("relay state provider is required")
	}

	m := &Manager{
		agentBuilder:      p.AgentBuilder,
		runtimeManager:    p.RuntimeManager,
		relayMCPServerIDs: append([]string(nil), p.RelayMCPServerIDs...),
		relayProviderName: strings.TrimSpace(p.RelayProviderID),
		workingDir:        p.WorkingDir,
		workspaces:        relayagent.NewWorkspaceManager(p.WorkingDir, p.StateDir, p.WorkspaceBaseRef),
		workspaceEnabled:  p.WorkspaceEnabled,
		workspaceBaseRef:  p.WorkspaceBaseRef,
		sessionStore:      p.StateProvider.Sessions(),
		logger:            p.Logger.With().Str("component", "relay.session_manager").Logger(),
		sessions:          make(map[string]*TopicSession),
	}

	p.LC.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			m.logger.Info().Str("relay_provider", m.getProviderName()).Msg("session manager ready")
			return nil
		},
		OnStop: func(ctx context.Context) error {
			m.logger.Info().Int("active_sessions", len(m.sessions)).Msg("session manager stopping")
			m.stopAllWithContext(ctx)
			return nil
		},
	})

	return m, nil
}

// ValidateAgent checks if an agent with the given name exists in the config.
func (m *Manager) ValidateAgent(agentName string) error {
	m.mu.RLock()
	builder := m.agentBuilder
	m.mu.RUnlock()
	if builder == nil {
		return fmt.Errorf("agent builder is required")
	}
	return builder.ValidateAgent(agentName)
}

// GetAgentInfo returns the description and list of MCP server names for an agent.
func (m *Manager) GetAgentInfo(agentName string) (string, []string) {
	m.mu.RLock()
	builder := m.agentBuilder
	relayMCPServerIDs := append([]string(nil), m.relayMCPServerIDs...)
	m.mu.RUnlock()
	if builder == nil {
		return agentName, relayMCPServerIDs
	}
	description, mcpServers := builder.GetAgentInfo(agentName)
	return description, mergeUniqueStringIDs(mcpServers, relayMCPServerIDs)
}

// GetAgentMetadata returns relay-provider metadata with provider-scoped MCP IDs.
func (m *Manager) GetAgentMetadata(agentName string) AgentMetadata {
	m.mu.RLock()
	builder := m.agentBuilder
	relayMCPServerIDs := append([]string(nil), m.relayMCPServerIDs...)
	m.mu.RUnlock()
	if builder == nil {
		return AgentMetadata{}
	}
	meta := builder.GetAgentMetadata(agentName)
	meta.MCPServers = mergeUniqueStringIDs(meta.MCPServers, relayMCPServerIDs)
	return meta
}

// ProviderIDs returns configured runtime provider IDs sorted lexicographically.
func (m *Manager) ProviderIDs() []string {
	m.mu.RLock()
	builder := m.agentBuilder
	m.mu.RUnlock()
	if builder == nil {
		return nil
	}
	return builder.ProviderIDs()
}

// RelayProviderID returns the configured relay provider ID.
func (m *Manager) RelayProviderID() string {
	return m.getProviderName()
}

// CreateSession builds an agent for the given locator and stores it in memory.
func (m *Manager) CreateSession(ctx context.Context, sessionCtx SessionContext, agentName string) error {
	locator := sessionCtx.Locator
	userID := strings.TrimSpace(sessionCtx.UserID)
	if userID == "" {
		return fmt.Errorf("user id is required")
	}

	addr, ok, err := locator.TelegramAddress()
	if err != nil {
		return fmt.Errorf("decode session locator: %w", err)
	}
	if !ok {
		return fmt.Errorf("unsupported channel type %q", locator.ChannelType)
	}

	sessionID := strings.TrimSpace(locator.SessionID)
	chatID := addr.ChatID
	topicID := addr.TopicID
	m.mu.RLock()
	builder := m.agentBuilder
	m.mu.RUnlock()
	if builder == nil {
		return fmt.Errorf("agent builder is required")
	}

	m.logger.Info().
		Int64("chat_id", chatID).
		Int("topic_id", topicID).
		Str("user_id", userID).
		Str("agent", agentName).
		Str("session_id", sessionID).
		Str("channel_type", locator.ChannelType).
		Msg("creating session")

	m.mu.Lock()
	if _, exists := m.sessions[sessionID]; exists {
		m.mu.Unlock()
		m.logger.Warn().Str("session_id", sessionID).Msg("session already exists")
		return fmt.Errorf("session already exists for %s", locator.AddressKey)
	}
	m.mu.Unlock()

	branchName := ""
	workspaceDir := m.workingDir
	if m.workspaceEnabled {
		branchName = m.SessionBranchName(sessionID)
		workspaceDir, err = m.workspaces.EnsureWorkspace(ctx, sessionID, branchName, "")
		if err != nil {
			m.logger.Error().Err(err).Str("session_id", sessionID).Msg("failed to create workspace")
			return fmt.Errorf("create workspace: %w", err)
		}
		m.logger.Debug().Str("session_id", sessionID).Str("workspace", workspaceDir).Msg("workspace created")
	}

	runtimeManager := m.runtimeManager
	if runtimeManager == nil {
		if m.workspaceEnabled {
			_ = m.workspaces.CleanupWorkspace(ctx, workspaceDir)
		}
		return fmt.Errorf("relay runtime manager is required")
	}

	rootRuntime, err := runtimeManager.Runtime(ctx)
	if err != nil {
		if m.workspaceEnabled {
			_ = m.workspaces.CleanupWorkspace(ctx, workspaceDir)
		}
		return err
	}
	relayProvider := strings.TrimSpace(runtimeManager.ProviderID())
	if relayProvider == "" {
		relayProvider = m.getProviderName()
	}

	agentSessionID := m.newAgentSessionID(sessionID)
	sess, err := builder.CreateRuntimeSession(
		ctx,
		rootRuntime,
		relayProvider,
		userID,
		agentSessionID,
		workspaceDir,
	)
	if err != nil {
		m.logger.Error().
			Err(err).
			Str("session_id", sessionID).
			Str("agent_session_id", agentSessionID).
			Str("agent", relayProvider).
			Str("label", agentName).
			Msg("failed to create runtime session")
		if m.workspaceEnabled {
			_ = m.workspaces.CleanupWorkspace(ctx, workspaceDir)
		}
		return err
	}

	ts := &TopicSession{
		sessionID:      sessionID,
		agentSessionID: agentSessionID,
		userID:         userID,
		locator:        locator,
		topicID:        topicID,
		agentName:      agentName,
		agent:          rootRuntime.Agent,
		runner:         rootRuntime.Runner,
		sessionSvc:     rootRuntime.SessionSvc,
		sess:           sess,
		chatID:         chatID,
		workspaceDir:   workspaceDir,
		branchName:     branchName,
	}

	if err := m.persistSessionRecord(ctx, ts, relaystate.SessionStatusActive); err != nil {
		if closeErr := m.closeTopicSession(ctx, ts); closeErr != nil {
			m.logger.Warn().Err(closeErr).Str("session_id", sessionID).Msg("failed to rollback session after persist error")
		}
		return fmt.Errorf("persist session metadata: %w", err)
	}

	m.mu.Lock()
	m.sessions[sessionID] = ts
	m.mu.Unlock()

	m.logger.Info().
		Int64("chat_id", chatID).
		Int("topic_id", topicID).
		Str("user_id", userID).
		Str("agent", agentName).
		Str("session_id", sessionID).
		Str("channel_type", locator.ChannelType).
		Msg("session created successfully")

	return nil
}

// StopSession removes a session from memory and cleans up.
func (m *Manager) StopSession(locator SessionLocator) {
	sessionID := strings.TrimSpace(locator.SessionID)

	m.logger.Info().
		Str("session_id", sessionID).
		Str("channel_type", locator.ChannelType).
		Str("address_key", locator.AddressKey).
		Msg("stopping session")

	m.mu.Lock()
	ts, exists := m.sessions[sessionID]
	if exists {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	if !exists {
		m.logger.Warn().Str("session_id", sessionID).Msg("session not found for stop")
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	if err := m.closeTopicSession(cleanupCtx, ts); err != nil {
		m.logger.Warn().Err(err).Str("session_id", sessionID).Msg("failed to close topic session")
	}
	if err := m.sessionStore.DeleteBySessionID(cleanupCtx, sessionID); err != nil {
		m.logger.Warn().Err(err).Str("session_id", sessionID).Msg("failed to delete persisted session metadata")
	}

	m.logger.Info().Str("session_id", sessionID).Msg("session stopped")
}

// StopTelegramSession removes a Telegram session from memory and cleans up.
func (m *Manager) StopTelegramSession(chatID int64, topicID int) {
	m.StopSession(NewTelegramSessionLocator(chatID, topicID))
}

// StopAll closes all sessions.
func (m *Manager) StopAll() {
	m.stopAllWithContext(context.Background())
}

func (m *Manager) stopAllWithContext(ctx context.Context) {
	m.mu.Lock()
	sessions := make([]*TopicSession, 0, len(m.sessions))
	for _, ts := range m.sessions {
		sessions = append(sessions, ts)
	}
	m.sessions = make(map[string]*TopicSession)
	m.mu.Unlock()

	m.logger.Info().Int("count", len(sessions)).Msg("stopping all sessions")

	for _, ts := range sessions {
		if err := m.closeTopicSession(ctx, ts); err != nil {
			m.logger.Warn().Err(err).Str("session_id", ts.sessionID).Msg("failed to close topic session")
		}
	}

	m.logger.Info().Msg("all sessions stopped")
}

func (m *Manager) closeTopicSession(ctx context.Context, ts *TopicSession) error {
	var firstErr error
	if ts != nil && ts.sessionSvc != nil {
		sessionID := strings.TrimSpace(ts.GetAgentSessionID())
		userID := strings.TrimSpace(ts.userID)
		appName := "norma-relay"
		if ts.sess != nil {
			if sessionAppName := strings.TrimSpace(ts.sess.AppName()); sessionAppName != "" {
				appName = sessionAppName
			}
			if sessionUserID := strings.TrimSpace(ts.sess.UserID()); sessionUserID != "" {
				userID = sessionUserID
			}
		}
		if sessionID != "" && userID != "" {
			if err := ts.sessionSvc.Delete(ctx, &adksession.DeleteRequest{
				AppName:   appName,
				UserID:    userID,
				SessionID: sessionID,
			}); err != nil {
				firstErr = fmt.Errorf("delete adk session: %w", err)
			}
		}
	}
	if ts != nil && m.workspaceEnabled && ts.workspaceDir != "" {
		if err := m.workspaces.CleanupWorkspace(ctx, ts.workspaceDir); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *Manager) persistSessionRecord(ctx context.Context, ts *TopicSession, status string) error {
	if ts == nil {
		return fmt.Errorf("topic session is required")
	}
	if strings.TrimSpace(status) == "" {
		status = relaystate.SessionStatusActive
	}

	return m.sessionStore.Upsert(ctx, relaystate.SessionRecord{
		SessionID:    ts.sessionID,
		UserID:       ts.userID,
		ChannelType:  ts.locator.ChannelType,
		AddressKey:   ts.locator.AddressKey,
		AddressJSON:  ts.locator.AddressJSON,
		AgentName:    ts.agentName,
		WorkspaceDir: ts.workspaceDir,
		BranchName:   ts.branchName,
		Status:       status,
	})
}

func (m *Manager) newAgentSessionID(sessionID string) string {
	seq := atomic.AddUint64(&m.agentSessionSeq, 1)
	return fmt.Sprintf("%s-a%d", strings.TrimSpace(sessionID), seq)
}

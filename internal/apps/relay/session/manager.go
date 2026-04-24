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
	"github.com/normahq/relay/internal/git"
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

// SessionBranchName returns the git branch name for a relay session.
func (m *Manager) SessionBranchName(sessionID string) string {
	return fmt.Sprintf("norma/relay/%s", sessionID)
}

func mergeUniqueStringIDs(base, extra []string) []string {
	if len(extra) == 0 {
		return append([]string(nil), base...)
	}

	out := make([]string, 0, len(base)+len(extra))
	seen := make(map[string]struct{}, len(base)+len(extra))
	appendUnique := func(raw string) {
		id := strings.TrimSpace(raw)
		if id == "" {
			return
		}
		if _, exists := seen[id]; exists {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}

	for _, id := range base {
		appendUnique(id)
	}
	for _, id := range extra {
		appendUnique(id)
	}

	return out
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

// GetSession returns the in-memory session for the given locator.
func (m *Manager) GetSession(locator SessionLocator) (*TopicSession, error) {
	sessionID := strings.TrimSpace(locator.SessionID)

	m.mu.RLock()
	ts := m.sessions[sessionID]
	m.mu.RUnlock()

	if ts == nil {
		m.logger.Debug().
			Str("session_id", sessionID).
			Str("channel_type", locator.ChannelType).
			Str("address_key", locator.AddressKey).
			Int("active_sessions", len(m.sessions)).
			Msg("session not found")
		return nil, fmt.Errorf("no session for %s", locator.AddressKey)
	}

	return ts, nil
}

// HasSession reports whether a session exists in memory or persisted metadata.
// It does not create or restore sessions.
func (m *Manager) HasSession(ctx context.Context, locator SessionLocator) (bool, error) {
	sessionID := strings.TrimSpace(locator.SessionID)
	if sessionID == "" {
		return false, nil
	}

	m.mu.RLock()
	_, active := m.sessions[sessionID]
	m.mu.RUnlock()
	if active {
		return true, nil
	}

	if m.sessionStore == nil {
		return false, nil
	}

	record, ok, err := m.sessionStore.GetByAddress(ctx, locator.ChannelType, locator.AddressKey)
	if err != nil {
		return false, fmt.Errorf("read session metadata: %w", err)
	}
	if !ok {
		return false, nil
	}

	if status := strings.TrimSpace(record.Status); status != "" && status != relaystate.SessionStatusActive {
		return false, nil
	}
	return true, nil
}

// GetTelegramSession returns the in-memory session for the given Telegram tuple.
func (m *Manager) GetTelegramSession(chatID int64, topicID int) (*TopicSession, error) {
	return m.GetSession(NewTelegramSessionLocator(chatID, topicID))
}

// EnsureSession returns the existing session or creates a new one if it doesn't exist.
func (m *Manager) EnsureSession(ctx context.Context, sessionCtx SessionContext, agentName string) (*TopicSession, error) {
	sessionID := strings.TrimSpace(sessionCtx.Locator.SessionID)

	m.mu.RLock()
	ts := m.sessions[sessionID]
	m.mu.RUnlock()

	if ts != nil {
		m.logger.Debug().Str("session_id", sessionID).Msg("returning existing session")
		return ts, nil
	}

	if err := m.CreateSession(ctx, sessionCtx, agentName); err != nil {
		return nil, err
	}
	return m.GetSession(sessionCtx.Locator)
}

// EnsureTelegramSession returns the existing Telegram session or creates a new one.
func (m *Manager) EnsureTelegramSession(ctx context.Context, chatID int64, topicID int, userID int64, agentName string) (*TopicSession, error) {
	return m.EnsureSession(ctx, SessionContext{
		Locator: NewTelegramSessionLocator(chatID, topicID),
		UserID:  TelegramUserID(userID),
	}, agentName)
}

// RestoreSession restores a session from persisted metadata when it is not active in memory.
func (m *Manager) RestoreSession(ctx context.Context, sessionCtx SessionContext) (*TopicSession, error) {
	locator := sessionCtx.Locator
	sessionID := strings.TrimSpace(locator.SessionID)

	m.mu.RLock()
	if ts := m.sessions[sessionID]; ts != nil {
		m.mu.RUnlock()
		return ts, nil
	}
	m.mu.RUnlock()

	record, ok, err := m.sessionStore.GetByAddress(ctx, locator.ChannelType, locator.AddressKey)
	if err != nil {
		return nil, fmt.Errorf("read session metadata: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("%w for %s", ErrNoPersistedSession, locator.AddressKey)
	}
	if strings.TrimSpace(record.Status) != "" && record.Status != relaystate.SessionStatusActive {
		return nil, fmt.Errorf("persisted session for %s is not active", locator.AddressKey)
	}

	recordLocator, err := LocatorFromRecord(record)
	if err != nil {
		return nil, fmt.Errorf("decode persisted session locator: %w", err)
	}
	sessionLabel := strings.TrimSpace(record.AgentName)
	if sessionLabel == "" {
		sessionLabel = "auto"
	}

	m.logger.Info().
		Str("session_id", sessionID).
		Str("channel_type", recordLocator.ChannelType).
		Str("address_key", recordLocator.AddressKey).
		Str("label", sessionLabel).
		Msg("restoring session from persisted metadata")

	return m.EnsureSession(ctx, SessionContext{
		Locator: recordLocator,
		UserID:  sessionCtx.UserID,
	}, sessionLabel)
}

// RestoreTelegramSession restores a Telegram session from persisted metadata.
func (m *Manager) RestoreTelegramSession(ctx context.Context, chatID int64, topicID int, userID int64) (*TopicSession, error) {
	return m.RestoreSession(ctx, SessionContext{
		Locator:                    NewTelegramSessionLocator(chatID, topicID),
		UserID:                     TelegramUserID(userID),
		AllowRelayProviderFallback: true,
	})
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

// ListSessions returns info about all active sessions.
func (m *Manager) ListSessions() []TopicSessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]TopicSessionInfo, 0, len(m.sessions))
	for _, ts := range m.sessions {
		out = append(out, topicSessionInfo(ts, relaystate.SessionStatusActive))
	}
	return out
}

type TopicSessionInfo struct {
	SessionID    string
	UserID       string
	Locator      SessionLocator
	ChannelType  string
	AgentName    string
	ChatID       int64
	TopicID      int
	WorkspaceDir string
	BranchName   string
	Status       string
}

func (m *Manager) GetSessionInfo(ctx context.Context, sessionID string) (TopicSessionInfo, error) {
	trimmedID := strings.TrimSpace(sessionID)
	if trimmedID == "" {
		return TopicSessionInfo{}, fmt.Errorf("session_id is required")
	}

	m.mu.RLock()
	ts := m.sessions[trimmedID]
	m.mu.RUnlock()
	if ts != nil {
		return topicSessionInfo(ts, relaystate.SessionStatusActive), nil
	}

	record, ok, err := m.sessionStore.GetBySessionID(ctx, trimmedID)
	if err != nil {
		return TopicSessionInfo{}, fmt.Errorf("read session metadata: %w", err)
	}
	if !ok {
		return TopicSessionInfo{}, fmt.Errorf("session %q not found", trimmedID)
	}

	info, err := topicSessionInfoFromRecord(record)
	if err != nil {
		return TopicSessionInfo{}, err
	}
	info.Status = sessionStatusForInactiveRecord(record.Status)
	return info, nil
}

func (m *Manager) ListSessionInfos(ctx context.Context) ([]TopicSessionInfo, error) {
	persisted, err := m.sessionStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list session metadata: %w", err)
	}

	infos := make(map[string]TopicSessionInfo, len(persisted))
	for _, record := range persisted {
		info, err := topicSessionInfoFromRecord(record)
		if err != nil {
			return nil, err
		}
		info.Status = sessionStatusForInactiveRecord(record.Status)
		infos[info.SessionID] = info
	}

	for _, info := range m.ListSessions() {
		info.Status = relaystate.SessionStatusActive
		infos[info.SessionID] = info
	}

	out := make([]TopicSessionInfo, 0, len(infos))
	for _, info := range infos {
		out = append(out, info)
	}
	return out, nil
}

func (m *Manager) StopSessionByID(ctx context.Context, sessionID string) error {
	info, err := m.GetSessionInfo(ctx, sessionID)
	if err != nil {
		return err
	}
	if info.Status == relaystate.SessionStatusActive {
		m.StopSession(info.Locator)
		return nil
	}

	if m.workspaceEnabled && strings.TrimSpace(info.WorkspaceDir) != "" {
		if err := m.workspaces.CleanupWorkspace(ctx, info.WorkspaceDir); err != nil {
			return fmt.Errorf("cleanup workspace: %w", err)
		}
	}
	if err := m.sessionStore.DeleteBySessionID(ctx, info.SessionID); err != nil {
		return fmt.Errorf("delete session metadata: %w", err)
	}
	return nil
}

func topicSessionInfo(ts *TopicSession, status string) TopicSessionInfo {
	if ts == nil {
		return TopicSessionInfo{}
	}
	return TopicSessionInfo{
		SessionID:    ts.sessionID,
		UserID:       ts.userID,
		Locator:      ts.locator,
		ChannelType:  ts.locator.ChannelType,
		AgentName:    ts.agentName,
		ChatID:       ts.chatID,
		TopicID:      ts.topicID,
		WorkspaceDir: ts.workspaceDir,
		BranchName:   ts.branchName,
		Status:       status,
	}
}

func topicSessionInfoFromRecord(record relaystate.SessionRecord) (TopicSessionInfo, error) {
	locator, err := LocatorFromRecord(record)
	if err != nil {
		return TopicSessionInfo{}, fmt.Errorf("decode persisted session locator for %q: %w", record.SessionID, err)
	}

	info := TopicSessionInfo{
		SessionID:    record.SessionID,
		UserID:       record.UserID,
		Locator:      locator,
		ChannelType:  locator.ChannelType,
		AgentName:    record.AgentName,
		WorkspaceDir: record.WorkspaceDir,
		BranchName:   record.BranchName,
	}
	if address, ok, err := locator.TelegramAddress(); err != nil {
		return TopicSessionInfo{}, fmt.Errorf("decode telegram address for %q: %w", record.SessionID, err)
	} else if ok {
		info.ChatID = address.ChatID
		info.TopicID = address.TopicID
	}
	return info, nil
}

func sessionStatusForInactiveRecord(recordStatus string) string {
	if strings.TrimSpace(recordStatus) == "" || recordStatus == relaystate.SessionStatusActive {
		return sessionStatusPersisted
	}
	return recordStatus
}

func (m *Manager) getProviderName() string {
	if m.runtimeManager != nil {
		if providerID := strings.TrimSpace(m.runtimeManager.ProviderID()); providerID != "" {
			return providerID
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.relayProviderName
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

func (m *Manager) CommitWorkspace(ctx context.Context, chatID int64, topicID int) error {
	if !m.workspaceEnabled {
		return fmt.Errorf("workspace mode is disabled")
	}

	sessionID := NewTelegramSessionLocator(chatID, topicID).SessionID

	m.mu.RLock()
	ts, exists := m.sessions[sessionID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no session for topic %d", topicID)
	}

	workspaceDir := ts.workspaceDir
	if workspaceDir == "" {
		return fmt.Errorf("no workspace for topic %d", topicID)
	}

	statusOut, err := git.GitRunCmdOutput(ctx, workspaceDir, "git", "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("read workspace status: %w", err)
	}
	if status := statusOut; len(status) == 0 {
		return nil
	}

	if err := git.GitRunCmdErr(ctx, workspaceDir, "git", "add", "-A"); err != nil {
		return fmt.Errorf("stage workspace changes: %w", err)
	}

	commitMsg := fmt.Sprintf("chore: relay session %d/%d", chatID, topicID)
	if err := git.GitRunCmdErr(ctx, workspaceDir, "git", "commit", "-m", commitMsg); err != nil {
		return fmt.Errorf("commit workspace changes: %w", err)
	}

	return nil
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

package session

import (
	"context"
	"strings"
	"testing"

	"github.com/normahq/relay/internal/apps/relay/auth"
	relaystate "github.com/normahq/relay/internal/apps/relay/state"
	"github.com/normahq/relay/internal/apps/relaymcp"
	"github.com/rs/zerolog"
)

func TestRelayMCPListAgents_IncludesPersistedSessions(t *testing.T) {
	store := &fakeSessionStore{
		listRecords: []relaystate.SessionRecord{
			{
				SessionID:    "tg-7-8",
				UserID:       "tg-101",
				ChannelType:  relaystate.ChannelTypeTelegram,
				AddressKey:   "7:8",
				AddressJSON:  `{"chat_id":7,"topic_id":8}`,
				AgentName:    "opencode",
				WorkspaceDir: "/tmp/persisted",
				BranchName:   "norma/relay/tg-7-8",
				Status:       relaystate.SessionStatusActive,
			},
		},
	}

	manager := &Manager{
		logger:       zerolog.Nop(),
		sessionStore: store,
		sessions:     map[string]*TopicSession{},
	}
	svc := &relayMCPServer{manager: manager, logger: zerolog.Nop()}

	agents, err := svc.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("ListAgents() len = %d, want 1", len(agents))
	}
	if agents[0].SessionID != "tg-7-8" || agents[0].Status != sessionStatusPersisted {
		t.Fatalf("ListAgents()[0] = %+v, want persisted tg-7-8", agents[0])
	}
}

func TestRelayMCPStopAgent_StopsOwnedPersistedSession(t *testing.T) {
	store := &fakeSessionStore{
		recordsByID: map[string]relaystate.SessionRecord{
			"tg-5-6": {
				SessionID:    "tg-5-6",
				UserID:       "tg-111",
				ChannelType:  relaystate.ChannelTypeTelegram,
				AddressKey:   "5:6",
				AddressJSON:  `{"chat_id":5,"topic_id":6}`,
				AgentName:    "opencode",
				WorkspaceDir: "",
				BranchName:   "",
				Status:       relaystate.SessionStatusActive,
			},
		},
	}

	manager := &Manager{
		logger:       zerolog.Nop(),
		sessionStore: store,
		sessions: map[string]*TopicSession{
			"tg-5-0": {
				sessionID: "tg-5-0",
				userID:    "tg-111",
				locator:   NewTelegramSessionLocator(5, 0),
				agentName: "root",
				chatID:    5,
				topicID:   0,
			},
		},
	}
	svc := &relayMCPServer{manager: manager, logger: zerolog.Nop()}

	if err := svc.StopAgent(context.Background(), relaymcp.StopRequest{
		SessionID:       "tg-5-6",
		CallerSessionID: "tg-5-0",
	}); err != nil {
		t.Fatalf("StopAgent() error = %v", err)
	}
	if store.deletedSessionID != "tg-5-6" {
		t.Fatalf("DeleteBySessionID called with %q, want %q", store.deletedSessionID, "tg-5-6")
	}
}

func TestRelayMCPStopAgent_DeniesDifferentActor(t *testing.T) {
	store := &fakeSessionStore{
		recordsByID: map[string]relaystate.SessionRecord{
			"tg-5-6": {
				SessionID:    "tg-5-6",
				UserID:       "tg-222",
				ChannelType:  relaystate.ChannelTypeTelegram,
				AddressKey:   "5:6",
				AddressJSON:  `{"chat_id":5,"topic_id":6}`,
				AgentName:    "opencode",
				WorkspaceDir: "",
				BranchName:   "",
				Status:       relaystate.SessionStatusActive,
			},
		},
	}

	manager := &Manager{
		logger:       zerolog.Nop(),
		sessionStore: store,
		sessions: map[string]*TopicSession{
			"tg-5-0": {
				sessionID: "tg-5-0",
				userID:    "tg-111",
				locator:   NewTelegramSessionLocator(5, 0),
				agentName: "root",
				chatID:    5,
				topicID:   0,
			},
		},
	}
	svc := &relayMCPServer{manager: manager, logger: zerolog.Nop()}

	err := svc.StopAgent(context.Background(), relaymcp.StopRequest{
		SessionID:       "tg-5-6",
		CallerSessionID: "tg-5-0",
	})
	if err == nil {
		t.Fatal("StopAgent() error = nil, want permission error")
	}
	if !strings.Contains(err.Error(), "owned by another actor") {
		t.Fatalf("StopAgent() error = %q, want actor ownership message", err.Error())
	}
}

func TestRelayMCPStopAgent_AllowsOwnerForLegacyUnknownOwnerSession(t *testing.T) {
	store := &fakeSessionStore{
		recordsByID: map[string]relaystate.SessionRecord{
			"tg-5-6": {
				SessionID:    "tg-5-6",
				UserID:       "",
				ChannelType:  relaystate.ChannelTypeTelegram,
				AddressKey:   "5:6",
				AddressJSON:  `{"chat_id":5,"topic_id":6}`,
				AgentName:    "opencode",
				WorkspaceDir: "",
				BranchName:   "",
				Status:       relaystate.SessionStatusActive,
			},
		},
	}

	manager := &Manager{
		logger:       zerolog.Nop(),
		sessionStore: store,
		sessions: map[string]*TopicSession{
			"tg-5-0": {
				sessionID: "tg-5-0",
				userID:    "tg-2317500",
				locator:   NewTelegramSessionLocator(5, 0),
				agentName: "root",
				chatID:    5,
				topicID:   0,
			},
		},
	}
	svc := &relayMCPServer{
		manager: manager,
		logger:  zerolog.Nop(),
		owners:  fakeRelayOwnerStore{owner: &auth.Owner{UserID: 2317500}},
	}

	if err := svc.StopAgent(context.Background(), relaymcp.StopRequest{
		SessionID:       "tg-5-6",
		CallerSessionID: "tg-5-0",
	}); err != nil {
		t.Fatalf("StopAgent() error = %v", err)
	}
	if store.deletedSessionID != "tg-5-6" {
		t.Fatalf("DeleteBySessionID called with %q, want %q", store.deletedSessionID, "tg-5-6")
	}
}

func TestResolveActorContext_UsesCallerSessionContext(t *testing.T) {
	manager := &Manager{
		logger: zerolog.Nop(),
		sessions: map[string]*TopicSession{
			"tg-5-0": {
				sessionID: "tg-5-0",
				userID:    "tg-101",
				locator:   NewTelegramSessionLocator(5, 0),
				agentName: "root",
				chatID:    5,
				topicID:   0,
			},
		},
	}
	svc := &relayMCPServer{manager: manager, logger: zerolog.Nop()}

	actorCtx, err := svc.resolveActorContext(context.Background(), "tg-5-0")
	if err != nil {
		t.Fatalf("resolveActorContext() error = %v", err)
	}
	if actorCtx.ChatID != 5 {
		t.Fatalf("resolveActorContext() chat_id = %d, want 5", actorCtx.ChatID)
	}
	if actorCtx.UserID != "tg-101" {
		t.Fatalf("resolveActorContext() user_id = %q, want tg-101", actorCtx.UserID)
	}
}

func TestSessionLocatorFromStartLocator_TelegramRequiresChatIDAndNoTopic(t *testing.T) {
	_, err := sessionLocatorFromStartLocator(&relaymcp.StartLocator{
		ChannelType: relaystate.ChannelTypeTelegram,
		Address: map[string]any{
			"topic_id": float64(7),
		},
	})
	if err == nil {
		t.Fatal("sessionLocatorFromStartLocator() error = nil, want validation error")
	}
}

type fakeRelayOwnerStore struct {
	owner *auth.Owner
}

func (f fakeRelayOwnerStore) GetOwner() *auth.Owner {
	return f.owner
}

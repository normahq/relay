package handlers

import (
	"context"
	"strings"
	"testing"

	relayagent "github.com/normahq/relay/internal/apps/relay/agent"
	"github.com/normahq/relay/internal/apps/relay/auth"
	relaytelegram "github.com/normahq/relay/internal/apps/relay/channel/telegram"
	"github.com/normahq/relay/internal/apps/relay/messenger"
	relaysession "github.com/normahq/relay/internal/apps/relay/session"
	relaystate "github.com/normahq/relay/internal/apps/relay/state"
	"github.com/rs/zerolog"
	"github.com/tgbotkit/client"
	"github.com/tgbotkit/runtime/events"
	"github.com/tgbotkit/runtime/messagetype"
	adksession "google.golang.org/adk/session"
)

func TestRelayHandlerOnMessage_PublicTopicRestoreWelcomeUsesRelayName(t *testing.T) {
	topicID := 77
	locator := relaysession.NewTelegramSessionLocator(9001, topicID)
	store := &fakeRelayRestoreSessionStore{
		record: relaystate.SessionRecord{
			SessionID:   locator.SessionID,
			UserID:      "tg-101",
			ChannelType: locator.ChannelType,
			AddressKey:  locator.AddressKey,
			AddressJSON: locator.AddressJSON,
			AgentName:   "codex",
			Status:      relaystate.SessionStatusActive,
		},
		foundByAddress: true,
	}

	handler, turns, tgClient := newRelayRestoreHandlerHarness(t, store)

	event := newPublicTopicMessageEvent(topicID, "@testbot restore this topic")
	if err := handler.onMessage(context.Background(), event); err != nil {
		t.Fatalf("onMessage() error = %v", err)
	}

	if len(turns.enqueueCalls) != 1 {
		t.Fatalf("Enqueue calls = %d, want 1", len(turns.enqueueCalls))
	}
	assertLastSentContains(t, tgClient, "***Name:*** `relay`")
	if strings.Contains(lastSentText(t, tgClient), "***Name:*** `codex`") {
		t.Fatalf("last message unexpectedly contains persisted label: %q", lastSentText(t, tgClient))
	}
	if got := store.lastUpsert.AgentName; got != "codex" {
		t.Fatalf("persisted label = %q, want codex", got)
	}
}

func TestRelayHandlerOnMessage_PublicTopicAutoCreateWelcomeUsesRelayName(t *testing.T) {
	topicID := 88
	store := &fakeRelayRestoreSessionStore{
		foundByAddress: false,
	}

	handler, turns, tgClient := newRelayRestoreHandlerHarness(t, store)

	event := newPublicTopicMessageEvent(topicID, "@testbot create this topic")
	if err := handler.onMessage(context.Background(), event); err != nil {
		t.Fatalf("onMessage() error = %v", err)
	}

	if len(turns.enqueueCalls) != 1 {
		t.Fatalf("Enqueue calls = %d, want 1", len(turns.enqueueCalls))
	}
	assertLastSentContains(t, tgClient, "***Name:*** `relay`")
	if strings.Contains(lastSentText(t, tgClient), "***Name:*** `auto`") {
		t.Fatalf("last message unexpectedly contains auto label: %q", lastSentText(t, tgClient))
	}
	if got := store.lastUpsert.AgentName; got != "auto" {
		t.Fatalf("persisted label = %q, want auto", got)
	}
}

func TestRelayHandlerOnMessage_PublicMainChatAutoCreateEnqueuesTurn(t *testing.T) {
	chatID := int64(-5173524191)
	locator := relaysession.NewTelegramSessionLocator(chatID, 0)
	store := &fakeRelayRestoreSessionStore{
		foundByAddress: false,
	}

	handler, turns, tgClient := newRelayRestoreHandlerHarness(t, store)

	event := newPublicMainChatMessageEvent(chatID, "@testbot create main chat")
	if err := handler.onMessage(context.Background(), event); err != nil {
		t.Fatalf("onMessage() error = %v", err)
	}

	if len(turns.enqueueCalls) != 1 {
		t.Fatalf("Enqueue calls = %d, want 1", len(turns.enqueueCalls))
	}
	if got := turns.enqueueCalls[0].SessionID; got != locator.SessionID {
		t.Fatalf("Enqueue session = %q, want %q", got, locator.SessionID)
	}
	assertLastSentContains(t, tgClient, "***Name:*** `relay`")
	if got := store.lastUpsert.AgentName; got != "auto" {
		t.Fatalf("persisted label = %q, want auto", got)
	}
	if got := store.lastUpsert.SessionID; got != locator.SessionID {
		t.Fatalf("persisted session = %q, want %q", got, locator.SessionID)
	}
}

func TestRelayHandlerOnMessage_PublicMainChatAutoCreateWithUnrelatedActiveSession(t *testing.T) {
	chatID := int64(-5173524191)
	locator := relaysession.NewTelegramSessionLocator(chatID, 0)
	unrelatedLocator := relaysession.NewTelegramSessionLocator(chatID, 77)
	store := &fakeRelayRestoreSessionStore{
		foundByAddress: false,
	}

	handler, turns, _ := newRelayRestoreHandlerHarness(t, store)
	setUnexportedField(t, handler.sessionManager, "sessions", map[string]*relaysession.TopicSession{
		unrelatedLocator.SessionID: newRelayTopicSession(t, unrelatedLocator.SessionID),
	})

	event := newPublicMainChatMessageEvent(chatID, "@testbot use main chat")
	if err := handler.onMessage(context.Background(), event); err != nil {
		t.Fatalf("onMessage() error = %v", err)
	}

	if len(turns.enqueueCalls) != 1 {
		t.Fatalf("Enqueue calls = %d, want 1", len(turns.enqueueCalls))
	}
	if got := turns.enqueueCalls[0].SessionID; got != locator.SessionID {
		t.Fatalf("Enqueue session = %q, want %q", got, locator.SessionID)
	}
	if got := store.lastUpsert.SessionID; got != locator.SessionID {
		t.Fatalf("persisted session = %q, want %q", got, locator.SessionID)
	}
}

func TestRelayHandlerOnMessage_OwnerDMCreatesOwnerSession(t *testing.T) {
	locator := relaysession.NewTelegramSessionLocator(9001, 0)
	store := &fakeRelayRestoreSessionStore{
		foundByAddress: false,
	}

	handler, turns, tgClient := newRelayRestoreHandlerHarness(t, store)

	event := newPrivateMessageEvent(9001, "hello owner session")
	if err := handler.onMessage(context.Background(), event); err != nil {
		t.Fatalf("onMessage() error = %v", err)
	}

	if len(turns.enqueueCalls) != 1 {
		t.Fatalf("Enqueue calls = %d, want 1", len(turns.enqueueCalls))
	}
	if got := turns.enqueueCalls[0].SessionID; got != locator.SessionID {
		t.Fatalf("Enqueue session = %q, want %q", got, locator.SessionID)
	}
	assertLastSentContains(t, tgClient, "***Name:*** `relay`")
	if got := store.lastUpsert.AgentName; got != "relay" {
		t.Fatalf("persisted label = %q, want relay", got)
	}
}

func newRelayRestoreHandlerHarness(t *testing.T, store *fakeRelayRestoreSessionStore) (*RelayHandler, *fakeTurnDispatcher, *fakeTelegramClient) {
	t.Helper()

	ownerStore, err := auth.NewOwnerStore(&fakeOwnerKVStore{})
	if err != nil {
		t.Fatalf("NewOwnerStore(): %v", err)
	}
	if _, err := ownerStore.RegisterOwner(101, 9001, "owner", "Owner", "", true); err != nil {
		t.Fatalf("RegisterOwner(): %v", err)
	}

	builder := &fakeRelayRestoreAgentBuilder{
		metadata: relayagent.AgentMetadata{
			Type:       "opencode_acp",
			Model:      "opencode/minimax-m2.5-free",
			MCPServers: []string{"relay", "azure_devops"},
		},
	}
	runtimeManager := &fakeRelayRestoreRuntimeManager{providerID: "relay-provider"}
	sessionManager := newRelayRestoreSessionManager(t, builder, runtimeManager, store)

	tgClient := &fakeTelegramClient{}
	msg := messenger.NewMessenger(tgClient, zerolog.Nop())
	turnDispatcher := &fakeTurnDispatcher{}

	handler := &RelayHandler{
		ownerStore: ownerStore,
		channel: relaytelegram.NewAdapter(relaytelegram.AdapterParams{
			Messenger: msg,
			TGClient:  tgClient,
			Logger:    zerolog.Nop(),
		}),
		sessionManager: sessionManager,
		turnDispatcher: turnDispatcher,
		logger:         zerolog.Nop(),
		authorizer:     &fakeRelayAuthorizer{ownerID: 101},
	}
	handler.SetOwner(101, 9001)
	setUnexportedField(t, handler, "relayProviderName", "relay-provider")
	handler.botUsername = "testbot"
	handler.botUserID = 4242

	return handler, turnDispatcher, tgClient
}

func newRelayRestoreSessionManager(
	t *testing.T,
	builder *fakeRelayRestoreAgentBuilder,
	runtimeManager *fakeRelayRestoreRuntimeManager,
	store *fakeRelayRestoreSessionStore,
) *relaysession.Manager {
	t.Helper()

	m := &relaysession.Manager{}
	setUnexportedField(t, m, "agentBuilder", builder)
	setUnexportedField(t, m, "runtimeManager", runtimeManager)
	setUnexportedField(t, m, "relayProviderName", "relay-provider")
	setUnexportedField(t, m, "sessionStore", store)
	setUnexportedField(t, m, "logger", zerolog.Nop())
	setUnexportedField(t, m, "sessions", map[string]*relaysession.TopicSession{})
	return m
}

func newPublicTopicMessageEvent(topicID int, text string) *events.MessageEvent {
	entities := []client.MessageEntity{{Type: "mention", Offset: 0, Length: len("@testbot")}}
	return &events.MessageEvent{
		Type: messagetype.Text,
		Message: &client.Message{
			Chat: client.Chat{
				Id:   9001,
				Type: "supergroup",
			},
			MessageThreadId: &topicID,
			Text:            &text,
			Entities:        &entities,
			From:            &client.User{Id: 101},
		},
	}
}

func newPublicMainChatMessageEvent(chatID int64, text string) *events.MessageEvent {
	entities := []client.MessageEntity{{Type: "mention", Offset: 0, Length: len("@testbot")}}
	return &events.MessageEvent{
		Type: messagetype.Text,
		Message: &client.Message{
			Chat: client.Chat{
				Id:   chatID,
				Type: "supergroup",
			},
			Text:     &text,
			Entities: &entities,
			From:     &client.User{Id: 101},
		},
	}
}

func newPrivateMessageEvent(chatID int64, text string) *events.MessageEvent {
	return &events.MessageEvent{
		Type: messagetype.Text,
		Message: &client.Message{
			Chat: client.Chat{
				Id:   chatID,
				Type: "private",
			},
			Text: &text,
			From: &client.User{Id: 101},
		},
	}
}

func lastSentText(t *testing.T, tgClient *fakeTelegramClient) string {
	t.Helper()
	if len(tgClient.messages) == 0 {
		t.Fatal("sent messages = 0, want at least one")
	}
	return tgClient.messages[len(tgClient.messages)-1].Text
}

type fakeRelayRestoreSessionStore struct {
	record         relaystate.SessionRecord
	foundByAddress bool
	lastUpsert     relaystate.SessionRecord
}

func (f *fakeRelayRestoreSessionStore) Upsert(_ context.Context, record relaystate.SessionRecord) error {
	f.lastUpsert = record
	f.record = record
	f.foundByAddress = true
	return nil
}

func (f *fakeRelayRestoreSessionStore) GetByAddress(_ context.Context, channelType, addressKey string) (relaystate.SessionRecord, bool, error) {
	if !f.foundByAddress {
		return relaystate.SessionRecord{}, false, nil
	}
	if f.record.ChannelType != channelType || f.record.AddressKey != addressKey {
		return relaystate.SessionRecord{}, false, nil
	}
	return f.record, true, nil
}

func (f *fakeRelayRestoreSessionStore) GetBySessionID(_ context.Context, sessionID string) (relaystate.SessionRecord, bool, error) {
	if !f.foundByAddress || f.record.SessionID != sessionID {
		return relaystate.SessionRecord{}, false, nil
	}
	return f.record, true, nil
}

func (*fakeRelayRestoreSessionStore) DeleteBySessionID(context.Context, string) error {
	return nil
}

func (f *fakeRelayRestoreSessionStore) List(context.Context) ([]relaystate.SessionRecord, error) {
	if !f.foundByAddress {
		return nil, nil
	}
	return []relaystate.SessionRecord{f.record}, nil
}

type fakeRelayRestoreAgentBuilder struct {
	metadata relayagent.AgentMetadata
}

func (*fakeRelayRestoreAgentBuilder) CreateRuntimeSession(
	context.Context,
	*relayagent.BuiltRuntime,
	string,
	string,
	string,
	string,
) (adksession.Session, error) {
	return nil, nil
}

func (*fakeRelayRestoreAgentBuilder) ValidateAgent(string) error {
	return nil
}

func (*fakeRelayRestoreAgentBuilder) GetAgentInfo(string) (string, []string) {
	return "", nil
}

func (f *fakeRelayRestoreAgentBuilder) GetAgentMetadata(string) relayagent.AgentMetadata {
	return f.metadata
}

func (*fakeRelayRestoreAgentBuilder) ProviderIDs() []string {
	return []string{"relay-provider"}
}

type fakeRelayRestoreRuntimeManager struct {
	providerID string
}

func (*fakeRelayRestoreRuntimeManager) Runtime(context.Context) (*relayagent.BuiltRuntime, error) {
	return &relayagent.BuiltRuntime{}, nil
}

func (f *fakeRelayRestoreRuntimeManager) ProviderID() string {
	return f.providerID
}

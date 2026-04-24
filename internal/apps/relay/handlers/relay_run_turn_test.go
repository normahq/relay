package handlers

import (
	"context"
	"iter"
	"net/http"
	"strings"
	"testing"

	relaychannel "github.com/normahq/relay/internal/apps/relay/channel"
	relaytelegram "github.com/normahq/relay/internal/apps/relay/channel/telegram"
	"github.com/normahq/relay/internal/apps/relay/messenger"
	relaysession "github.com/normahq/relay/internal/apps/relay/session"
	"github.com/rs/zerolog"
	"github.com/tgbotkit/client"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
)

type relayRunTurnTelegramClient struct {
	client.ClientWithResponsesInterface
	drafts      []client.SendMessageDraftJSONRequestBody
	messages    []client.SendMessageJSONRequestBody
	chatActions []client.SendChatActionJSONRequestBody
}

func (c *relayRunTurnTelegramClient) SendMessageWithResponse(
	_ context.Context,
	body client.SendMessageJSONRequestBody,
	_ ...client.RequestEditorFn,
) (*client.SendMessageResponse, error) {
	c.messages = append(c.messages, body)
	return &client.SendMessageResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusOK, Status: "200 OK"},
		JSON200: &struct {
			Ok     client.SendMessage200Ok `json:"ok"`
			Result client.Message          `json:"result"`
		}{
			Ok:     true,
			Result: client.Message{MessageId: len(c.messages)},
		},
	}, nil
}

func (c *relayRunTurnTelegramClient) SendMessageDraftWithResponse(
	_ context.Context,
	body client.SendMessageDraftJSONRequestBody,
	_ ...client.RequestEditorFn,
) (*client.SendMessageDraftResponse, error) {
	c.drafts = append(c.drafts, body)
	return &client.SendMessageDraftResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusOK, Status: "200 OK"},
		JSON200: &struct {
			Ok     client.SendMessageDraft200Ok `json:"ok"`
			Result bool                         `json:"result"`
		}{
			Ok:     true,
			Result: true,
		},
	}, nil
}

func (c *relayRunTurnTelegramClient) SendChatActionWithResponse(
	_ context.Context,
	body client.SendChatActionJSONRequestBody,
	_ ...client.RequestEditorFn,
) (*client.SendChatActionResponse, error) {
	c.chatActions = append(c.chatActions, body)
	return &client.SendChatActionResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusOK, Status: "200 OK"},
		JSON200: &struct {
			Ok     client.SendChatAction200Ok `json:"ok"`
			Result bool                       `json:"result"`
		}{
			Ok:     true,
			Result: true,
		},
	}, nil
}

func TestRunTurn_SendsProgressForNonTerminalEventsInDM(t *testing.T) {
	t.Parallel()

	tgClient := &relayRunTurnTelegramClient{}
	msg := messenger.NewMessenger(tgClient, zerolog.Nop())
	channel := relaytelegram.NewAdapter(relaytelegram.AdapterParams{
		Messenger: msg,
		TGClient:  tgClient,
		Logger:    zerolog.Nop(),
	})
	h := &RelayHandler{
		channel: channel,
		logger:  zerolog.Nop(),
	}

	adkRunner, sessionID := newRelayRunTurnTestRunner(t)
	locator := relaysession.NewTelegramSessionLocator(9001, 77)
	progressPolicy := relaychannel.ProgressPolicy{Typing: true, Thinking: true}
	if err := h.runTurn(context.Background(), "hello", adkRunner, "tg-101", sessionID, sessionID, locator, 41, progressPolicy); err != nil {
		t.Fatalf("runTurn() error = %v", err)
	}

	if len(tgClient.drafts) != 3 {
		t.Fatalf("draft calls = %d, want 3", len(tgClient.drafts))
	}
	if got := tgClient.drafts[0].Text; got != "Thinking." {
		t.Fatalf("draft[0].text = %q, want Thinking.", got)
	}
	if got := tgClient.drafts[1].Text; got != "Thinking.." {
		t.Fatalf("draft[1].text = %q, want Thinking..", got)
	}
	if got := tgClient.drafts[2].Text; got != "Thinking..." {
		t.Fatalf("draft[2].text = %q, want Thinking...", got)
	}
	for i, draft := range tgClient.drafts {
		if draft.MessageThreadId == nil || *draft.MessageThreadId != 77 {
			t.Fatalf("draft[%d].message_thread_id = %v, want 77", i, draft.MessageThreadId)
		}
	}

	if len(tgClient.chatActions) != 3 {
		t.Fatalf("chat action calls = %d, want 3", len(tgClient.chatActions))
	}
	for i, action := range tgClient.chatActions {
		if action.Action != "typing" {
			t.Fatalf("chatActions[%d].action = %q, want typing", i, action.Action)
		}
		if action.ChatId != 9001 {
			t.Fatalf("chatActions[%d].chat_id = %d, want 9001", i, action.ChatId)
		}
		if action.MessageThreadId == nil || *action.MessageThreadId != 77 {
			t.Fatalf("chatActions[%d].message_thread_id = %v, want 77", i, action.MessageThreadId)
		}
	}

	if len(tgClient.messages) != 1 {
		t.Fatalf("message calls = %d, want 1", len(tgClient.messages))
	}
	if !strings.Contains(tgClient.messages[0].Text, "final answer") {
		t.Fatalf("message text = %q, want to contain final answer", tgClient.messages[0].Text)
	}
	if tgClient.messages[0].ParseMode == nil || *tgClient.messages[0].ParseMode != "MarkdownV2" {
		t.Fatalf("parse_mode = %v, want MarkdownV2", tgClient.messages[0].ParseMode)
	}
}

func TestRunTurn_SkipsTypingAndDraftWhenAllProgressDisabled(t *testing.T) {
	t.Parallel()

	tgClient := &relayRunTurnTelegramClient{}
	msg := messenger.NewMessenger(tgClient, zerolog.Nop())
	channel := relaytelegram.NewAdapter(relaytelegram.AdapterParams{
		Messenger: msg,
		TGClient:  tgClient,
		Logger:    zerolog.Nop(),
	})
	h := &RelayHandler{
		channel: channel,
		logger:  zerolog.Nop(),
	}

	adkRunner, sessionID := newRelayRunTurnTestRunner(t)
	locator := relaysession.NewTelegramSessionLocator(9001, 77)
	if err := h.runTurn(context.Background(), "hello", adkRunner, "tg-101", sessionID, sessionID, locator, 41, relaychannel.ProgressPolicy{}); err != nil {
		t.Fatalf("runTurn() error = %v", err)
	}

	if len(tgClient.drafts) != 0 {
		t.Fatalf("draft calls = %d, want 0", len(tgClient.drafts))
	}
	if len(tgClient.chatActions) != 0 {
		t.Fatalf("chat action calls = %d, want 0", len(tgClient.chatActions))
	}
	if len(tgClient.messages) != 1 {
		t.Fatalf("message calls = %d, want 1", len(tgClient.messages))
	}
	if !strings.Contains(tgClient.messages[0].Text, "final answer") {
		t.Fatalf("message text = %q, want to contain final answer", tgClient.messages[0].Text)
	}
}

func TestRunTurn_SendsTypingWithoutThinkingDraftInPublicChat(t *testing.T) {
	t.Parallel()

	tgClient := &relayRunTurnTelegramClient{}
	msg := messenger.NewMessenger(tgClient, zerolog.Nop())
	channel := relaytelegram.NewAdapter(relaytelegram.AdapterParams{
		Messenger: msg,
		TGClient:  tgClient,
		Logger:    zerolog.Nop(),
	})
	h := &RelayHandler{
		channel: channel,
		logger:  zerolog.Nop(),
	}

	adkRunner, sessionID := newRelayRunTurnTestRunner(t)
	locator := relaysession.NewTelegramSessionLocator(9001, 77)
	progressPolicy := relaychannel.ProgressPolicy{Typing: true, Thinking: false}
	if err := h.runTurn(context.Background(), "hello", adkRunner, "tg-101", sessionID, sessionID, locator, 41, progressPolicy); err != nil {
		t.Fatalf("runTurn() error = %v", err)
	}

	if len(tgClient.drafts) != 0 {
		t.Fatalf("draft calls = %d, want 0", len(tgClient.drafts))
	}
	if len(tgClient.chatActions) != 3 {
		t.Fatalf("chat action calls = %d, want 3", len(tgClient.chatActions))
	}
	for i, action := range tgClient.chatActions {
		if action.Action != "typing" {
			t.Fatalf("chatActions[%d].action = %q, want typing", i, action.Action)
		}
	}
}

func TestRunTurn_SendsProgressForNonThoughtEvents(t *testing.T) {
	t.Parallel()

	tgClient := &relayRunTurnTelegramClient{}
	msg := messenger.NewMessenger(tgClient, zerolog.Nop())
	msg.SetAgentReplyFormattingMode("none")
	channel := relaytelegram.NewAdapter(relaytelegram.AdapterParams{
		Messenger: msg,
		TGClient:  tgClient,
		Logger:    zerolog.Nop(),
	})
	h := &RelayHandler{
		channel: channel,
		logger:  zerolog.Nop(),
	}

	adkRunner, sessionID := newRelayRunTurnTestRunnerWithEvents(t, func(invocationID string) []*adksession.Event {
		toolCall := adksession.NewEvent(invocationID)
		toolCall.Content = &genai.Content{
			Role: genai.RoleModel,
			Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{Name: "tool.one"}},
			},
		}

		partial := adksession.NewEvent(invocationID)
		partial.Partial = true
		partial.Content = genai.NewContentFromText("visible", genai.RoleModel)

		done := adksession.NewEvent(invocationID)
		done.TurnComplete = true

		return []*adksession.Event{toolCall, partial, done}
	})
	locator := relaysession.NewTelegramSessionLocator(9001, 77)
	progressPolicy := relaychannel.ProgressPolicy{Typing: true}
	if err := h.runTurn(context.Background(), "hello", adkRunner, "tg-101", sessionID, sessionID, locator, 41, progressPolicy); err != nil {
		t.Fatalf("runTurn() error = %v", err)
	}

	if len(tgClient.chatActions) != 2 {
		t.Fatalf("chat action calls = %d, want 2", len(tgClient.chatActions))
	}
	if len(tgClient.drafts) != 0 {
		t.Fatalf("draft calls = %d, want 0", len(tgClient.drafts))
	}
	if len(tgClient.messages) != 1 {
		t.Fatalf("message calls = %d, want 1", len(tgClient.messages))
	}
	if got := strings.TrimSpace(tgClient.messages[0].Text); got != "visible" {
		t.Fatalf("message text = %q, want visible", tgClient.messages[0].Text)
	}
}

func TestRunTurn_SendsFinalResponseWithoutParseModeWhenConfiguredNone(t *testing.T) {
	t.Parallel()

	tgClient := &relayRunTurnTelegramClient{}
	msg := messenger.NewMessenger(tgClient, zerolog.Nop())
	msg.SetAgentReplyFormattingMode("none")
	channel := relaytelegram.NewAdapter(relaytelegram.AdapterParams{
		Messenger: msg,
		TGClient:  tgClient,
		Logger:    zerolog.Nop(),
	})
	h := &RelayHandler{
		channel: channel,
		logger:  zerolog.Nop(),
	}

	adkRunner, sessionID := newRelayRunTurnTestRunner(t)
	locator := relaysession.NewTelegramSessionLocator(9001, 77)
	if err := h.runTurn(context.Background(), "hello", adkRunner, "tg-101", sessionID, sessionID, locator, 41, relaychannel.ProgressPolicy{}); err != nil {
		t.Fatalf("runTurn() error = %v", err)
	}

	if len(tgClient.messages) != 1 {
		t.Fatalf("message calls = %d, want 1", len(tgClient.messages))
	}
	if tgClient.messages[0].ParseMode != nil {
		t.Fatalf("parse_mode = %v, want nil", *tgClient.messages[0].ParseMode)
	}
}

func TestRunTurn_SendsOnlyFinalTextOnTurnComplete(t *testing.T) {
	t.Parallel()

	tgClient := &relayRunTurnTelegramClient{}
	msg := messenger.NewMessenger(tgClient, zerolog.Nop())
	channel := relaytelegram.NewAdapter(relaytelegram.AdapterParams{
		Messenger: msg,
		TGClient:  tgClient,
		Logger:    zerolog.Nop(),
	})
	h := &RelayHandler{
		channel: channel,
		logger:  zerolog.Nop(),
	}

	adkRunner, sessionID := newRelayRunTurnTestRunnerWithEvents(t, func(invocationID string) []*adksession.Event {
		partial := adksession.NewEvent(invocationID)
		partial.Partial = true
		partial.Content = genai.NewContentFromText("progress update", genai.RoleModel)

		final := adksession.NewEvent(invocationID)
		final.Content = genai.NewContentFromText("final answer", genai.RoleModel)

		done := adksession.NewEvent(invocationID)
		done.TurnComplete = true

		return []*adksession.Event{partial, final, done}
	})
	locator := relaysession.NewTelegramSessionLocator(9001, 77)
	if err := h.runTurn(context.Background(), "hello", adkRunner, "tg-101", sessionID, sessionID, locator, 41, relaychannel.ProgressPolicy{}); err != nil {
		t.Fatalf("runTurn() error = %v", err)
	}

	if len(tgClient.messages) != 1 {
		t.Fatalf("message calls = %d, want 1", len(tgClient.messages))
	}
	if got := strings.TrimSpace(tgClient.messages[0].Text); got != "final answer" {
		t.Fatalf("message text = %q, want final answer", tgClient.messages[0].Text)
	}
}

func TestRunTurn_UsesLastNonPartialFallbackOnTurnComplete(t *testing.T) {
	t.Parallel()

	tgClient := &relayRunTurnTelegramClient{}
	msg := messenger.NewMessenger(tgClient, zerolog.Nop())
	channel := relaytelegram.NewAdapter(relaytelegram.AdapterParams{
		Messenger: msg,
		TGClient:  tgClient,
		Logger:    zerolog.Nop(),
	})
	h := &RelayHandler{
		channel: channel,
		logger:  zerolog.Nop(),
	}

	adkRunner, sessionID := newRelayRunTurnTestRunnerWithEvents(t, func(invocationID string) []*adksession.Event {
		nonFinalOne := adksession.NewEvent(invocationID)
		nonFinalOne.Content = &genai.Content{
			Role: genai.RoleModel,
			Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{Name: "tool.one"}},
				{Text: "old fallback"},
			},
		}

		nonFinalTwo := adksession.NewEvent(invocationID)
		nonFinalTwo.Content = &genai.Content{
			Role: genai.RoleModel,
			Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{Name: "tool.two"}},
				{Text: "new fallback"},
			},
		}

		done := adksession.NewEvent(invocationID)
		done.TurnComplete = true

		return []*adksession.Event{nonFinalOne, nonFinalTwo, done}
	})
	locator := relaysession.NewTelegramSessionLocator(9001, 77)
	if err := h.runTurn(context.Background(), "hello", adkRunner, "tg-101", sessionID, sessionID, locator, 41, relaychannel.ProgressPolicy{}); err != nil {
		t.Fatalf("runTurn() error = %v", err)
	}

	if len(tgClient.messages) != 1 {
		t.Fatalf("message calls = %d, want 1", len(tgClient.messages))
	}
	if got := strings.TrimSpace(tgClient.messages[0].Text); got != "new fallback" {
		t.Fatalf("message text = %q, want new fallback", tgClient.messages[0].Text)
	}
}

func TestRunTurn_DoesNotSendWithoutTurnComplete(t *testing.T) {
	t.Parallel()

	tgClient := &relayRunTurnTelegramClient{}
	msg := messenger.NewMessenger(tgClient, zerolog.Nop())
	msg.SetAgentReplyFormattingMode("none")
	channel := relaytelegram.NewAdapter(relaytelegram.AdapterParams{
		Messenger: msg,
		TGClient:  tgClient,
		Logger:    zerolog.Nop(),
	})
	h := &RelayHandler{
		channel: channel,
		logger:  zerolog.Nop(),
	}

	adkRunner, sessionID := newRelayRunTurnTestRunnerWithEvents(t, func(invocationID string) []*adksession.Event {
		final := adksession.NewEvent(invocationID)
		final.Content = genai.NewContentFromText("final answer", genai.RoleModel)
		return []*adksession.Event{final}
	})
	locator := relaysession.NewTelegramSessionLocator(9001, 77)
	if err := h.runTurn(context.Background(), "hello", adkRunner, "tg-101", sessionID, sessionID, locator, 41, relaychannel.ProgressPolicy{}); err != nil {
		t.Fatalf("runTurn() error = %v", err)
	}

	if len(tgClient.messages) != 0 {
		t.Fatalf("message calls = %d, want 0", len(tgClient.messages))
	}
}

func TestRunTurn_UsesPartialDeltaFallbackOnTurnComplete(t *testing.T) {
	t.Parallel()

	tgClient := &relayRunTurnTelegramClient{}
	msg := messenger.NewMessenger(tgClient, zerolog.Nop())
	msg.SetAgentReplyFormattingMode("none")
	channel := relaytelegram.NewAdapter(relaytelegram.AdapterParams{
		Messenger: msg,
		TGClient:  tgClient,
		Logger:    zerolog.Nop(),
	})
	h := &RelayHandler{
		channel: channel,
		logger:  zerolog.Nop(),
	}

	adkRunner, sessionID := newRelayRunTurnTestRunnerWithEvents(t, func(invocationID string) []*adksession.Event {
		partialOne := adksession.NewEvent(invocationID)
		partialOne.Partial = true
		partialOne.Content = genai.NewContentFromText("Doing", genai.RoleModel)

		partialTwo := adksession.NewEvent(invocationID)
		partialTwo.Partial = true
		partialTwo.Content = genai.NewContentFromText(" well", genai.RoleModel)

		done := adksession.NewEvent(invocationID)
		done.TurnComplete = true

		return []*adksession.Event{partialOne, partialTwo, done}
	})
	locator := relaysession.NewTelegramSessionLocator(9001, 77)
	if err := h.runTurn(context.Background(), "hello", adkRunner, "tg-101", sessionID, sessionID, locator, 41, relaychannel.ProgressPolicy{}); err != nil {
		t.Fatalf("runTurn() error = %v", err)
	}

	if len(tgClient.messages) != 1 {
		t.Fatalf("message calls = %d, want 1", len(tgClient.messages))
	}
	if got := strings.TrimSpace(tgClient.messages[0].Text); got != "Doing well" {
		t.Fatalf("message text = %q, want Doing well", tgClient.messages[0].Text)
	}
}

func TestRunTurn_UsesPartialCumulativeFallbackOnTurnComplete(t *testing.T) {
	t.Parallel()

	tgClient := &relayRunTurnTelegramClient{}
	msg := messenger.NewMessenger(tgClient, zerolog.Nop())
	msg.SetAgentReplyFormattingMode("none")
	channel := relaytelegram.NewAdapter(relaytelegram.AdapterParams{
		Messenger: msg,
		TGClient:  tgClient,
		Logger:    zerolog.Nop(),
	})
	h := &RelayHandler{
		channel: channel,
		logger:  zerolog.Nop(),
	}

	adkRunner, sessionID := newRelayRunTurnTestRunnerWithEvents(t, func(invocationID string) []*adksession.Event {
		partialOne := adksession.NewEvent(invocationID)
		partialOne.Partial = true
		partialOne.Content = genai.NewContentFromText("Doing", genai.RoleModel)

		partialTwo := adksession.NewEvent(invocationID)
		partialTwo.Partial = true
		partialTwo.Content = genai.NewContentFromText("Doing well", genai.RoleModel)

		partialThree := adksession.NewEvent(invocationID)
		partialThree.Partial = true
		partialThree.Content = genai.NewContentFromText("Doing well.", genai.RoleModel)

		done := adksession.NewEvent(invocationID)
		done.TurnComplete = true

		return []*adksession.Event{partialOne, partialTwo, partialThree, done}
	})
	locator := relaysession.NewTelegramSessionLocator(9001, 77)
	if err := h.runTurn(context.Background(), "hello", adkRunner, "tg-101", sessionID, sessionID, locator, 41, relaychannel.ProgressPolicy{}); err != nil {
		t.Fatalf("runTurn() error = %v", err)
	}

	if len(tgClient.messages) != 1 {
		t.Fatalf("message calls = %d, want 1", len(tgClient.messages))
	}
	if got := strings.TrimSpace(tgClient.messages[0].Text); got != "Doing well." {
		t.Fatalf("message text = %q, want Doing well.", tgClient.messages[0].Text)
	}
}

func TestRunTurn_PartialFallbackSkipsThoughtText(t *testing.T) {
	t.Parallel()

	tgClient := &relayRunTurnTelegramClient{}
	msg := messenger.NewMessenger(tgClient, zerolog.Nop())
	msg.SetAgentReplyFormattingMode("none")
	channel := relaytelegram.NewAdapter(relaytelegram.AdapterParams{
		Messenger: msg,
		TGClient:  tgClient,
		Logger:    zerolog.Nop(),
	})
	h := &RelayHandler{
		channel: channel,
		logger:  zerolog.Nop(),
	}

	adkRunner, sessionID := newRelayRunTurnTestRunnerWithEvents(t, func(invocationID string) []*adksession.Event {
		thought := adksession.NewEvent(invocationID)
		thought.Partial = true
		thought.Content = &genai.Content{
			Role: genai.RoleModel,
			Parts: []*genai.Part{
				{Thought: true, Text: "secret"},
			},
		}

		partial := adksession.NewEvent(invocationID)
		partial.Partial = true
		partial.Content = genai.NewContentFromText("visible", genai.RoleModel)

		done := adksession.NewEvent(invocationID)
		done.TurnComplete = true

		return []*adksession.Event{thought, partial, done}
	})
	locator := relaysession.NewTelegramSessionLocator(9001, 77)
	if err := h.runTurn(context.Background(), "hello", adkRunner, "tg-101", sessionID, sessionID, locator, 41, relaychannel.ProgressPolicy{}); err != nil {
		t.Fatalf("runTurn() error = %v", err)
	}

	if len(tgClient.messages) != 1 {
		t.Fatalf("message calls = %d, want 1", len(tgClient.messages))
	}
	if got := strings.TrimSpace(tgClient.messages[0].Text); got != "visible" {
		t.Fatalf("message text = %q, want visible", tgClient.messages[0].Text)
	}
}

func newRelayRunTurnTestRunner(t *testing.T) (*runner.Runner, string) {
	t.Helper()

	return newRelayRunTurnTestRunnerWithEvents(t, func(invocationID string) []*adksession.Event {
		thoughtOne := adksession.NewEvent(invocationID)
		thoughtOne.Content = &genai.Content{
			Role:  genai.RoleModel,
			Parts: []*genai.Part{{Thought: true}},
		}

		thoughtTwo := adksession.NewEvent(invocationID)
		thoughtTwo.Content = &genai.Content{
			Role:  genai.RoleModel,
			Parts: []*genai.Part{{Thought: true}},
		}

		reply := adksession.NewEvent(invocationID)
		reply.Content = genai.NewContentFromText("final answer", genai.RoleModel)

		done := adksession.NewEvent(invocationID)
		done.TurnComplete = true

		return []*adksession.Event{thoughtOne, thoughtTwo, reply, done}
	})
}

func newRelayRunTurnTestRunnerWithEvents(
	t *testing.T,
	eventsFn func(invocationID string) []*adksession.Event,
) (*runner.Runner, string) {
	t.Helper()

	relayAgent, err := adkagent.New(adkagent.Config{
		Name:        "RelayRunTurnTestAgent",
		Description: "Emits scripted events for relay runTurn tests",
		Run: func(ctx adkagent.InvocationContext) iter.Seq2[*adksession.Event, error] {
			return func(yield func(*adksession.Event, error) bool) {
				for _, ev := range eventsFn(ctx.InvocationID()) {
					if !yield(ev, nil) {
						return
					}
				}
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New() error = %v", err)
	}

	sessionService := adksession.InMemoryService()
	adkRunner, err := runner.New(runner.Config{
		AppName:        "relay-run-turn-test",
		Agent:          relayAgent,
		SessionService: sessionService,
	})
	if err != nil {
		t.Fatalf("runner.New() error = %v", err)
	}

	sess, err := sessionService.Create(context.Background(), &adksession.CreateRequest{
		AppName: "relay-run-turn-test",
		UserID:  "tg-101",
	})
	if err != nil {
		t.Fatalf("session.Create() error = %v", err)
	}

	return adkRunner, sess.Session.ID()
}

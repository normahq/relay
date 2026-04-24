package messenger

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/normahq/relay/internal/apps/relay/telegramfmt"
	"github.com/rs/zerolog"
	"github.com/tgbotkit/client"
)

const testParseModeHTML = "HTML"

type fakeChatActionClient struct {
	client.ClientWithResponsesInterface
	chatActions        []client.SendChatActionJSONRequestBody
	chatActionResults  []sendChatActionResult
	messages           []client.SendMessageJSONRequestBody
	sendMessageResults []sendMessageResult
}

type sendChatActionResult struct {
	resp *client.SendChatActionResponse
	err  error
}

type sendMessageResult struct {
	resp *client.SendMessageResponse
	err  error
}

func (f *fakeChatActionClient) SendChatActionWithResponse(
	_ context.Context,
	body client.SendChatActionJSONRequestBody,
	_ ...client.RequestEditorFn,
) (*client.SendChatActionResponse, error) {
	f.chatActions = append(f.chatActions, body)
	if len(f.chatActionResults) > 0 {
		result := f.chatActionResults[0]
		f.chatActionResults = f.chatActionResults[1:]
		return result.resp, result.err
	}
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

func (f *fakeChatActionClient) SendMessageWithResponse(
	_ context.Context,
	body client.SendMessageJSONRequestBody,
	_ ...client.RequestEditorFn,
) (*client.SendMessageResponse, error) {
	f.messages = append(f.messages, body)
	if len(f.sendMessageResults) > 0 {
		result := f.sendMessageResults[0]
		f.sendMessageResults = f.sendMessageResults[1:]
		return result.resp, result.err
	}
	return &client.SendMessageResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusOK, Status: "200 OK"},
		JSON200: &struct {
			Ok     client.SendMessage200Ok `json:"ok"`
			Result client.Message          `json:"result"`
		}{
			Ok:     true,
			Result: client.Message{MessageId: len(f.messages)},
		},
	}, nil
}

func TestSendChatAction_IncludesMessageThreadIDWhenTopicProvided(t *testing.T) {
	t.Parallel()

	tgClient := &fakeChatActionClient{}
	m := NewMessenger(tgClient, zerolog.Nop())

	if err := m.SendChatAction(context.Background(), 9001, 77, "typing"); err != nil {
		t.Fatalf("SendChatAction() error = %v", err)
	}

	if len(tgClient.chatActions) != 1 {
		t.Fatalf("chatActions calls = %d, want 1", len(tgClient.chatActions))
	}
	got := tgClient.chatActions[0]
	if got.ChatId != 9001 {
		t.Fatalf("chat_id = %d, want 9001", got.ChatId)
	}
	if got.Action != "typing" {
		t.Fatalf("action = %q, want typing", got.Action)
	}
	if got.MessageThreadId == nil || *got.MessageThreadId != 77 {
		t.Fatalf("message_thread_id = %v, want 77", got.MessageThreadId)
	}
}

func TestSendChatAction_OmitsMessageThreadIDForRootChat(t *testing.T) {
	t.Parallel()

	tgClient := &fakeChatActionClient{}
	m := NewMessenger(tgClient, zerolog.Nop())

	if err := m.SendChatAction(context.Background(), 9001, 0, "typing"); err != nil {
		t.Fatalf("SendChatAction() error = %v", err)
	}

	if len(tgClient.chatActions) != 1 {
		t.Fatalf("chatActions calls = %d, want 1", len(tgClient.chatActions))
	}
	if tgClient.chatActions[0].MessageThreadId != nil {
		t.Fatalf("message_thread_id = %v, want nil", tgClient.chatActions[0].MessageThreadId)
	}
}

func TestSendChatAction_AllowsEmptySuccessResponseBody(t *testing.T) {
	t.Parallel()

	tgClient := &fakeChatActionClient{
		chatActionResults: []sendChatActionResult{
			{
				resp: &client.SendChatActionResponse{
					HTTPResponse: &http.Response{StatusCode: http.StatusOK, Status: "200 OK"},
				},
			},
		},
	}
	m := NewMessenger(tgClient, zerolog.Nop())

	if err := m.SendChatAction(context.Background(), -5173524191, 0, "typing"); err != nil {
		t.Fatalf("SendChatAction() error = %v, want nil", err)
	}
}

func TestSendChatAction_ReturnsTelegramErrorResponse(t *testing.T) {
	t.Parallel()

	tgClient := &fakeChatActionClient{
		chatActionResults: []sendChatActionResult{
			{
				resp: &client.SendChatActionResponse{
					HTTPResponse: &http.Response{StatusCode: http.StatusBadRequest, Status: "400 Bad Request"},
					JSON400:      &client.ErrorResponse{Description: "Bad Request: chat not found"},
				},
			},
		},
	}
	m := NewMessenger(tgClient, zerolog.Nop())

	err := m.SendChatAction(context.Background(), 9001, 0, "typing")
	if err == nil {
		t.Fatal("SendChatAction() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("SendChatAction() error = %v, want chat not found", err)
	}
}

func TestSendAgentReply_UsesConfiguredFormattingMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mode      string
		wantParse *string
	}{
		{
			name: "markdownv2",
			mode: telegramfmt.ModeMarkdownV2,
			wantParse: func() *string {
				v := "MarkdownV2"
				return &v
			}(),
		},
		{
			name: "html",
			mode: telegramfmt.ModeHTML,
			wantParse: func() *string {
				v := testParseModeHTML
				return &v
			}(),
		},
		{
			name:      "none",
			mode:      telegramfmt.ModeNone,
			wantParse: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tgClient := &fakeChatActionClient{}
			m := NewMessenger(tgClient, zerolog.Nop())
			m.SetAgentReplyFormattingMode(tt.mode)

			if err := m.SendAgentReply(context.Background(), 9001, "**final answer**", 77); err != nil {
				t.Fatalf("SendAgentReply() error = %v", err)
			}

			if len(tgClient.messages) != 1 {
				t.Fatalf("messages calls = %d, want 1", len(tgClient.messages))
			}
			got := tgClient.messages[0].ParseMode
			switch {
			case tt.wantParse == nil && got != nil:
				t.Fatalf("parse_mode = %v, want nil", *got)
			case tt.wantParse != nil && (got == nil || *got != *tt.wantParse):
				if got == nil {
					t.Fatalf("parse_mode = nil, want %q", *tt.wantParse)
				}
				t.Fatalf("parse_mode = %q, want %q", *got, *tt.wantParse)
			}
		})
	}
}

func TestSendAgentReply_RetriesWithoutParseModeOnTelegramParseError(t *testing.T) {
	t.Parallel()

	parseErrorResp := &client.SendMessageResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusBadRequest, Status: "400 Bad Request"},
		JSON400: &client.ErrorResponse{
			Description: "Bad Request: can't parse entities: Character '_' is reserved and must be escaped",
		},
	}
	tgClient := &fakeChatActionClient{
		sendMessageResults: []sendMessageResult{
			{resp: parseErrorResp},
		},
	}
	m := NewMessenger(tgClient, zerolog.Nop())
	m.SetAgentReplyFormattingMode(telegramfmt.ModeHTML)

	if err := m.SendAgentReply(context.Background(), 9001, "Hello _world_", 77); err != nil {
		t.Fatalf("SendAgentReply() error = %v", err)
	}
	if len(tgClient.messages) != 2 {
		t.Fatalf("messages calls = %d, want 2", len(tgClient.messages))
	}
	if tgClient.messages[0].ParseMode == nil || *tgClient.messages[0].ParseMode != testParseModeHTML {
		t.Fatalf("first parse_mode = %v, want %s", tgClient.messages[0].ParseMode, testParseModeHTML)
	}
	if tgClient.messages[1].ParseMode != nil {
		t.Fatalf("second parse_mode = %v, want nil", *tgClient.messages[1].ParseMode)
	}
}

func TestSendAgentReply_DoesNotRetryWithoutParseModeOnNonParseBadRequest(t *testing.T) {
	t.Parallel()

	badReqResp := &client.SendMessageResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusBadRequest, Status: "400 Bad Request"},
		JSON400: &client.ErrorResponse{
			Description: "Bad Request: chat not found",
		},
	}
	tgClient := &fakeChatActionClient{
		sendMessageResults: []sendMessageResult{
			{resp: badReqResp},
		},
	}
	m := NewMessenger(tgClient, zerolog.Nop())
	m.SetAgentReplyFormattingMode(telegramfmt.ModeHTML)

	err := m.SendAgentReply(context.Background(), 9001, "hello", 77)
	if err == nil {
		t.Fatal("SendAgentReply() error = nil, want non-nil")
	}
	if len(tgClient.messages) != 1 {
		t.Fatalf("messages calls = %d, want 1", len(tgClient.messages))
	}
}

func TestSendAgentReply_RetriesWithoutParseModeOnTransportError(t *testing.T) {
	t.Parallel()

	tgClient := &fakeChatActionClient{
		sendMessageResults: []sendMessageResult{
			{err: errors.New("network timeout")},
		},
	}
	m := NewMessenger(tgClient, zerolog.Nop())
	m.SetAgentReplyFormattingMode(telegramfmt.ModeHTML)

	if err := m.SendAgentReply(context.Background(), 9001, "hello", 77); err != nil {
		t.Fatalf("SendAgentReply() error = %v", err)
	}
	if len(tgClient.messages) != 2 {
		t.Fatalf("messages calls = %d, want 2", len(tgClient.messages))
	}
	if tgClient.messages[0].ParseMode == nil || *tgClient.messages[0].ParseMode != testParseModeHTML {
		t.Fatalf("first parse_mode = %v, want %s", tgClient.messages[0].ParseMode, testParseModeHTML)
	}
	if tgClient.messages[1].ParseMode != nil {
		t.Fatalf("second parse_mode = %v, want nil", *tgClient.messages[1].ParseMode)
	}
}

func TestSendAgentReply_MarkdownV2PreservesSoftLineBreaks(t *testing.T) {
	t.Parallel()

	tgClient := &fakeChatActionClient{}
	m := NewMessenger(tgClient, zerolog.Nop())
	m.SetAgentReplyFormattingMode(telegramfmt.ModeMarkdownV2)

	if err := m.SendAgentReply(context.Background(), 9001, "Hey there\nWhat do you want to work on?", 77); err != nil {
		t.Fatalf("SendAgentReply() error = %v", err)
	}

	if len(tgClient.messages) != 1 {
		t.Fatalf("messages calls = %d, want 1", len(tgClient.messages))
	}
	if !strings.Contains(tgClient.messages[0].Text, "Hey there\nWhat do you want to work on?") {
		t.Fatalf("message text = %q, want preserved line break", tgClient.messages[0].Text)
	}
}

func TestSendAgentReply_MarkdownV2PreservesExistingHardLineBreaks(t *testing.T) {
	t.Parallel()

	tgClient := &fakeChatActionClient{}
	m := NewMessenger(tgClient, zerolog.Nop())
	m.SetAgentReplyFormattingMode(telegramfmt.ModeMarkdownV2)

	if err := m.SendAgentReply(context.Background(), 9001, "Hey there  \nWhat do you want to work on?", 77); err != nil {
		t.Fatalf("SendAgentReply() error = %v", err)
	}

	if len(tgClient.messages) != 1 {
		t.Fatalf("messages calls = %d, want 1", len(tgClient.messages))
	}
	if !strings.Contains(tgClient.messages[0].Text, "Hey there\nWhat do you want to work on?") {
		t.Fatalf("message text = %q, want preserved line break", tgClient.messages[0].Text)
	}
}

func TestSendAgentReply_MarkdownV2PreservesBlankLines(t *testing.T) {
	t.Parallel()

	tgClient := &fakeChatActionClient{}
	m := NewMessenger(tgClient, zerolog.Nop())
	m.SetAgentReplyFormattingMode(telegramfmt.ModeMarkdownV2)

	if err := m.SendAgentReply(context.Background(), 9001, "First paragraph\n\nSecond paragraph", 77); err != nil {
		t.Fatalf("SendAgentReply() error = %v", err)
	}

	if len(tgClient.messages) != 1 {
		t.Fatalf("messages calls = %d, want 1", len(tgClient.messages))
	}
	if !strings.Contains(tgClient.messages[0].Text, "First paragraph\n\nSecond paragraph") {
		t.Fatalf("message text = %q, want preserved blank line", tgClient.messages[0].Text)
	}
}

func TestSendAgentReply_MarkdownV2DoesNotRewriteFencedCodeLineBreaks(t *testing.T) {
	t.Parallel()

	tgClient := &fakeChatActionClient{}
	m := NewMessenger(tgClient, zerolog.Nop())
	m.SetAgentReplyFormattingMode(telegramfmt.ModeMarkdownV2)

	input := "```txt\none\ntwo\n```"
	if err := m.SendAgentReply(context.Background(), 9001, input, 77); err != nil {
		t.Fatalf("SendAgentReply() error = %v", err)
	}

	if len(tgClient.messages) != 1 {
		t.Fatalf("messages calls = %d, want 1", len(tgClient.messages))
	}
	if strings.Contains(tgClient.messages[0].Text, "one  \n") || strings.Contains(tgClient.messages[0].Text, "two  \n") {
		t.Fatalf("message text = %q, code block line breaks were rewritten", tgClient.messages[0].Text)
	}
	if !strings.Contains(tgClient.messages[0].Text, "one\ntwo\n") {
		t.Fatalf("message text = %q, want original code block line breaks", tgClient.messages[0].Text)
	}
}

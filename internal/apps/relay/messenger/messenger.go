package messenger

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	tgmd "github.com/Mad-Pixels/goldmark-tgmd"
	"github.com/normahq/relay/internal/apps/relay/telegramfmt"
	"github.com/rs/zerolog"
	"github.com/tgbotkit/client"
)

// Messenger handles all Telegram message sending for the relay.
type Messenger struct {
	client                   client.ClientWithResponsesInterface
	logger                   zerolog.Logger
	agentReplyFormattingMode string
}

// NewMessenger creates a new Messenger.
func NewMessenger(client client.ClientWithResponsesInterface, logger zerolog.Logger) *Messenger {
	return &Messenger{
		client:                   client,
		logger:                   logger.With().Str("component", "relay.messenger").Logger(),
		agentReplyFormattingMode: telegramfmt.ModeMarkdownV2,
	}
}

// SetAgentReplyFormattingMode sets relay.telegram.formatting_mode for final agent responses.
func (m *Messenger) SetAgentReplyFormattingMode(mode string) {
	m.agentReplyFormattingMode = telegramfmt.NormalizeMode(mode)
}

// SendDraftPlain sends a plain-text draft (no parse_mode).
func (m *Messenger) SendDraftPlain(ctx context.Context, chatID int64, draftID int, text string, topicID int) error {
	m.logger.Debug().
		Int64("chat_id", chatID).
		Int("draft_id", draftID).
		Str("draft_text", text).
		Msg("sending plain draft")
	req := client.SendMessageDraftJSONRequestBody{
		ChatId:  chatID,
		DraftId: draftID,
		Text:    text,
	}
	if topicID != 0 {
		req.MessageThreadId = &topicID
	}

	resp, err := m.client.SendMessageDraftWithResponse(ctx, req)
	if err != nil {
		return fmt.Errorf("sending plain draft to chat %d: %w", chatID, err)
	}
	if resp.JSON400 != nil {
		return fmt.Errorf("sending plain draft to chat %d: %s", chatID, resp.JSON400.Description)
	}
	if resp.JSON200 == nil {
		return fmt.Errorf("sending plain draft to chat %d: no response body", chatID)
	}
	return nil
}

// SendPlain sends a plain-text message.
func (m *Messenger) SendPlain(ctx context.Context, chatID int64, text string, topicID int) error {
	req := client.SendMessageJSONRequestBody{
		ChatId: chatID,
		Text:   text,
	}
	if topicID != 0 {
		req.MessageThreadId = &topicID
	}
	_, err := m.client.SendMessageWithResponse(ctx, req)
	if err != nil {
		return fmt.Errorf("sending message to chat %d: %w", chatID, err)
	}
	return nil
}

// SendMarkdown converts standard Markdown to Telegram MarkdownV2 and sends.
func (m *Messenger) SendMarkdown(ctx context.Context, chatID int64, text string, topicID int) error {
	var buf bytes.Buffer
	md := tgmd.TGMD()
	if err := md.Convert([]byte(text), &buf); err != nil {
		m.logger.Warn().Err(err).Msg("failed to convert markdown to telegram format, falling back to escaped literal")
		return m.sendMessageWithMode(ctx, chatID, escapeMarkdownV2(text), topicID, "MarkdownV2", "send message with MarkdownV2")
	}
	return m.sendMessageWithMode(ctx, chatID, cleanTelegramMarkdownV2Payload(buf.String()), topicID, "MarkdownV2", "send message with MarkdownV2")
}

// SendAgentReply sends final model output with relay.telegram.formatting_mode.
func (m *Messenger) SendAgentReply(ctx context.Context, chatID int64, text string, topicID int) error {
	switch telegramfmt.NormalizeMode(m.agentReplyFormattingMode) {
	case telegramfmt.ModeHTML:
		return m.sendMessageWithMode(ctx, chatID, text, topicID, telegramfmt.TelegramParseMode(telegramfmt.ModeHTML), "send message with HTML")
	case telegramfmt.ModeNone:
		return m.sendMessageWithMode(ctx, chatID, text, topicID, telegramfmt.TelegramParseMode(telegramfmt.ModeNone), "send message without parse_mode")
	default:
		return m.SendMarkdown(ctx, chatID, text, topicID)
	}
}

func (m *Messenger) sendMessageWithMode(ctx context.Context, chatID int64, text string, topicID int, mode, logMsg string) error {
	m.logger.Debug().
		Int64("chat_id", chatID).
		Str("mode", mode).
		Str("telegram_payload", text).
		Msg("sending telegram message")
	req := client.SendMessageJSONRequestBody{
		ChatId: chatID,
		Text:   text,
	}
	if mode != "" {
		req.ParseMode = &mode
	}
	if topicID != 0 {
		req.MessageThreadId = &topicID
	}
	resp, err := m.client.SendMessageWithResponse(ctx, req)
	if shouldRetryWithoutParseMode(mode, resp, err) {
		retryReason := "transport error"
		if err == nil && resp != nil && resp.JSON400 != nil {
			retryReason = "telegram parse error"
		}
		m.logger.Warn().Err(err).Int64("chat_id", chatID).Str("retry_reason", retryReason).Msg(logMsg + " failed, retrying without parse_mode")
		req.ParseMode = nil
		resp, err = m.client.SendMessageWithResponse(ctx, req)
		if err != nil {
			return fmt.Errorf("%s to chat %d: %w", logMsg, chatID, err)
		}
	}
	if resp.JSON400 != nil {
		return fmt.Errorf("%s to chat %d: %s", logMsg, chatID, resp.JSON400.Description)
	}
	if resp.JSON200 == nil {
		return fmt.Errorf("%s to chat %d: no response body", logMsg, chatID)
	}
	return nil
}

func shouldRetryWithoutParseMode(mode string, resp *client.SendMessageResponse, err error) bool {
	if strings.TrimSpace(mode) == "" {
		return false
	}
	if err != nil {
		return true
	}
	if resp == nil || resp.JSON400 == nil {
		return false
	}
	return isTelegramParseEntitiesError(resp.JSON400.Description)
}

func isTelegramParseEntitiesError(description string) bool {
	desc := strings.ToLower(strings.TrimSpace(description))
	if desc == "" {
		return false
	}
	if strings.Contains(desc, "can't parse entities") || strings.Contains(desc, "cant parse entities") {
		return true
	}
	return strings.Contains(desc, "parse entities") && strings.Contains(desc, "entity")
}

func cleanTelegramMarkdownV2Payload(text string) string {
	text = strings.Trim(text, "\r\n")
	if text == "" {
		return text
	}

	lines := strings.Split(text, "\n")
	inFence := false
	for i, line := range lines {
		if isMarkdownFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}

		switch {
		case strings.HasPrefix(line, "  • "):
			lines[i] = strings.TrimPrefix(line, "  ")
		case strings.HasPrefix(line, "    ‣ "):
			lines[i] = strings.TrimPrefix(line, "  ")
		case strings.HasPrefix(line, "      ◦ "):
			lines[i] = strings.TrimPrefix(line, "  ")
		}
	}
	return strings.Join(lines, "\n")
}

func isMarkdownFenceLine(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return false
	}
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// SendChatAction sends a chat action (e.g., "typing").
func (m *Messenger) SendChatAction(ctx context.Context, chatID int64, topicID int, action string) error {
	if chatID == 0 {
		return nil
	}
	req := client.SendChatActionJSONRequestBody{
		ChatId: chatID,
		Action: action,
	}
	if topicID != 0 {
		req.MessageThreadId = &topicID
	}

	resp, err := m.client.SendChatActionWithResponse(ctx, req)
	if err != nil {
		return fmt.Errorf("sending chat action %q to chat %d: %w", action, chatID, err)
	}
	if resp == nil {
		return fmt.Errorf("sending chat action %q to chat %d: no response body", action, chatID)
	}
	if resp.JSON400 != nil {
		return fmt.Errorf("sending chat action %q to chat %d: %s", action, chatID, resp.JSON400.Description)
	}
	if resp.JSON200 == nil {
		if resp.HTTPResponse != nil && resp.HTTPResponse.StatusCode >= http.StatusOK && resp.HTTPResponse.StatusCode < http.StatusMultipleChoices {
			return nil
		}
		return fmt.Errorf("sending chat action %q to chat %d: no response body", action, chatID)
	}
	return nil
}

// KeepTyping sends typing action every 4 seconds until context is canceled.
func (m *Messenger) KeepTyping(ctx context.Context, chatID int64, topicID int) {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.SendChatAction(ctx, chatID, topicID, "typing"); err != nil {
				m.logger.Warn().Err(err).Int64("chat_id", chatID).Int("topic_id", topicID).Msg("failed to send typing chat action")
			}
		}
	}
}

func escapeMarkdownV2(text string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
		"\\", "\\\\",
	)
	return replacer.Replace(text)
}

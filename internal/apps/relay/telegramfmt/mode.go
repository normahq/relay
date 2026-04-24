package telegramfmt

import (
	"fmt"
	"strings"
)

const (
	ModeMarkdownV2 = "markdownv2"
	ModeHTML       = "html"
	ModeNone       = "none"
)

// NormalizeMode normalizes relay.telegram.formatting_mode.
// Empty input falls back to the default mode.
func NormalizeMode(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return ModeMarkdownV2
	}
	return trimmed
}

// ValidateMode normalizes and validates relay.telegram.formatting_mode.
func ValidateMode(raw string) (string, error) {
	mode := NormalizeMode(raw)
	switch mode {
	case ModeMarkdownV2, ModeHTML, ModeNone:
		return mode, nil
	default:
		return "", fmt.Errorf(
			"invalid relay.telegram.formatting_mode %q: allowed values are %q, %q, %q",
			strings.TrimSpace(raw),
			ModeMarkdownV2,
			ModeHTML,
			ModeNone,
		)
	}
}

// TelegramParseMode returns the Telegram parse_mode value for normalized mode.
// Empty string means parse_mode should be omitted.
func TelegramParseMode(mode string) string {
	switch NormalizeMode(mode) {
	case ModeHTML:
		return "HTML"
	case ModeNone:
		return ""
	default:
		return "MarkdownV2"
	}
}

// PromptRuleAndExample returns concise mode-specific instruction text.
func PromptRuleAndExample(mode string) (rule string, example string) {
	switch NormalizeMode(mode) {
	case ModeHTML:
		return "Use Telegram HTML parse mode. Supported tags: b/strong, i/em, u/ins, s/strike/del, tg-spoiler or span class=\"tg-spoiler\", a, code, pre, blockquote, tg-emoji, tg-time. Escape raw <, >, & as entities.", "<b>Build:</b> success. Run <code>relay start</code>."
	case ModeNone:
		return "Use plain text only. Do not use Markdown or HTML markup.", "Build: success. Run relay start."
	default:
		return "Use Telegram MarkdownV2 parse mode. Escape these reserved characters with backslash when needed: _ * [ ] ( ) ~ ` > # + - = | { } . !", "*Build:* success. Run `relay start`."
	}
}

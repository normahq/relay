package telegramfmt

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	tgmd "github.com/Mad-Pixels/goldmark-tgmd"
)

// MarkdownV2 converts normal Markdown/plain text to Telegram MarkdownV2.
func MarkdownV2(text string) (string, error) {
	md := tgmd.TGMD()
	return markdownV2WithConverter(text, func(source []byte, writer io.Writer) error {
		return md.Convert(source, writer)
	})
}

// SplitMarkdownMessageChunks splits agent Markdown on standalone thematic-break separators.
func SplitMarkdownMessageChunks(text string) []string {
	lines := strings.SplitAfter(text, "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}

	var chunks []string
	var current strings.Builder
	inFence := false
	for _, line := range lines {
		lineWithoutBreak := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if isMarkdownFenceLine(lineWithoutBreak) {
			inFence = !inFence
			current.WriteString(line)
			continue
		}
		if !inFence && strings.TrimSpace(lineWithoutBreak) == "---" {
			appendMarkdownChunk(&chunks, current.String())
			current.Reset()
			continue
		}
		current.WriteString(line)
	}
	appendMarkdownChunk(&chunks, current.String())
	return chunks
}

func markdownV2WithConverter(text string, convert func(source []byte, writer io.Writer) error) (string, error) {
	var buf bytes.Buffer
	if err := convert([]byte(text), &buf); err != nil {
		return "", fmt.Errorf("converting markdown to Telegram MarkdownV2: %w", err)
	}
	return cleanMarkdownV2Payload(buf.String()), nil
}

func appendMarkdownChunk(chunks *[]string, chunk string) {
	chunk = strings.Trim(chunk, "\r\n")
	if strings.TrimSpace(chunk) == "" {
		return
	}
	*chunks = append(*chunks, chunk)
}

// EscapeMarkdownV2 escapes a literal string for Telegram MarkdownV2.
func EscapeMarkdownV2(text string) string {
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

func cleanMarkdownV2Payload(text string) string {
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
		lines[i] = normalizePreEscapedMarkdownV2(lines[i])
	}
	return strings.Join(lines, "\n")
}

func isMarkdownFenceLine(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return false
	}
	if strings.HasPrefix(trimmed, "```") {
		return true
	}
	return strings.HasPrefix(trimmed, "~~~") && (len(trimmed) == 3 || !strings.HasSuffix(trimmed[3:], "~~~"))
}

func normalizePreEscapedMarkdownV2(line string) string {
	replacer := strings.NewReplacer(
		"\\\\_", "\\_",
		"\\\\*", "\\*",
		"\\\\[", "\\[",
		"\\\\]", "\\]",
		"\\\\(", "\\(",
		"\\\\)", "\\)",
		"\\\\~", "\\~",
		"\\\\`", "\\`",
		"\\\\>", "\\>",
		"\\\\#", "\\#",
		"\\\\+", "\\+",
		"\\\\-", "\\-",
		"\\\\=", "\\=",
		"\\\\|", "\\|",
		"\\\\{", "\\{",
		"\\\\}", "\\}",
		"\\\\.", "\\.",
		"\\\\!", "\\!",
	)
	return replacer.Replace(line)
}

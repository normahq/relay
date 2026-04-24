package telegramfmt

import (
	"os"
	"strings"
	"testing"
)

func TestTelegramFormattingDocsCoverFormatterContract(t *testing.T) {
	t.Parallel()

	doc := readRepoDoc(t, "docs/telegram-formatting.md")
	for _, want := range []string{
		"`markdownv2` (default)",
		"`html`",
		"`none`",
		"agent output is normal Markdown or plain text",
		"Relay converts it to Telegram MarkdownV2",
		"Splits final agent replies into multiple Telegram messages on standalone `---` separator lines outside fenced code blocks.",
		"Do not pre-escape Telegram MarkdownV2 reserved characters",
		"Relay escapes unsafe raw text while preserving supported Telegram HTML tags",
		`<blockquote expandable>`,
		`<tg-time unix="..." format="...">`,
		`<pre><code class="language-...">...</code></pre>`,
		`<tg-time datetime="...">`,
		"Standalone",
		`<code class="language-...">`,
		"Arbitrary HTML tags",
		`<div>`,
		`<script>`,
		`&lt;`, `&gt;`, `&amp;`, `&quot;`,
		"decimal numeric entities",
		"hex numeric entities",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("telegram formatting docs missing %q", want)
		}
	}
}

func TestUserDocsLinkTelegramFormattingGuide(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"README.md", "docs/relay.md"} {
		doc := readRepoDoc(t, path)
		if !strings.Contains(doc, "telegram-formatting.md") {
			t.Fatalf("%s does not link telegram-formatting.md", path)
		}
	}
}

func readRepoDoc(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile("../../../../" + path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

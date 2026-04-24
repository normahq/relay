package telegramfmt

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestMarkdownV2ConvertsFailingACPSample(t *testing.T) {
	t.Parallel()

	input := "Привет\\! Вот короткий пример доступного форматирования:\n\n" +
		"# Заголовок\n\n" +
		"**Жирный текст**  \n" +
		"*Курсив*  \n" +
		"~~Зачёркнутый~~  \n" +
		"`inline code`\n\n" +
		"- Пункт списка 1\n" +
		"- Пункт списка 2\n" +
		"  - Подпункт\n\n" +
		"1. Нумерованный пункт\n" +
		"2. Нумерованный пункт\n\n" +
		"> Цитата в сообщении\n\n" +
		"[Ссылка](https://example.com)\n\n" +
		"```bash\n" +
		"echo \"Пример блока кода\"\n" +
		"```"

	got, err := MarkdownV2(input)
	if err != nil {
		t.Fatalf("MarkdownV2() error = %v", err)
	}

	for _, unwanted := range []string{
		"\n  • ",
		"\n    ‣ ",
		"\n      ◦ ",
		"Привет\\\\!",
	} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("MarkdownV2() = %q, contains unwanted fragment %q", got, unwanted)
		}
	}
	for _, want := range []string{
		"Привет\\! Вот короткий пример доступного форматирования:",
		"***\\# Заголовок***",
		"***Жирный текст***",
		"_Курсив_",
		"~~~Зачёркнутый~~~",
		"`inline code`",
		"\n• Пункт списка 1",
		"\n• Пункт списка 2",
		"\n  ‣ Подпункт",
		"\n• Нумерованный пункт",
		">Цитата в сообщении",
		"[Ссылка](https://example.com)",
		"```bash\necho \"Пример блока кода\"\n```",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("MarkdownV2() = %q, want to contain %q", got, want)
		}
	}
	if strings.HasPrefix(got, "\n") || strings.HasSuffix(got, "\n") {
		t.Fatalf("MarkdownV2() = %q, want no leading or trailing newline", got)
	}
}

func TestMarkdownV2PreservesParagraphAndCodeStructure(t *testing.T) {
	t.Parallel()

	got, err := MarkdownV2("First paragraph\n\nSecond paragraph\n\n```txt\none\ntwo\n```")
	if err != nil {
		t.Fatalf("MarkdownV2() error = %v", err)
	}
	for _, want := range []string{
		"First paragraph\n\nSecond paragraph",
		"```txt\none\ntwo\n```",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("MarkdownV2() = %q, want to contain %q", got, want)
		}
	}
}

func TestMarkdownV2ReturnsConverterError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	_, err := markdownV2WithConverter("text", func(_ []byte, _ io.Writer) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("markdownV2WithConverter() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestEscapeMarkdownV2EscapesReservedCharacters(t *testing.T) {
	t.Parallel()

	const input = `_ * [ ] ( ) ~ ` + "`" + ` > # + - = | { } . ! \`
	const want = `\_ \* \[ \] \( \) \~ \` + "`" + ` \> \# \+ \- \= \| \{ \} \. \! \\`
	if got := EscapeMarkdownV2(input); got != want {
		t.Fatalf("EscapeMarkdownV2() = %q, want %q", got, want)
	}
}

func TestCleanMarkdownV2Payload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty",
			in:   "\r\n",
			want: "",
		},
		{
			name: "trims only outer newlines",
			in:   "\n  kept spaces  \n",
			want: "  kept spaces  ",
		},
		{
			name: "normalizes list prefixes outside fences",
			in: "\n  • top\n" +
				"    ‣ nested\n" +
				"      ◦ deeper\n",
			want: "• top\n" +
				"  ‣ nested\n" +
				"    ◦ deeper",
		},
		{
			name: "preserves backtick fence content",
			in: "  • before\n" +
				"```txt\n" +
				"  • code\n" +
				"    ‣ code\n" +
				"```\n" +
				"  • after",
			want: "• before\n" +
				"```txt\n" +
				"  • code\n" +
				"    ‣ code\n" +
				"```\n" +
				"• after",
		},
		{
			name: "preserves tilde fence content",
			in: "~~~txt\n" +
				"  • code\n" +
				"~~~\n" +
				"  • after",
			want: "~~~txt\n" +
				"  • code\n" +
				"~~~\n" +
				"• after",
		},
		{
			name: "does not treat over indented fence marker as fence",
			in: "    ```txt\n" +
				"  • rewritten\n" +
				"    ```",
			want: "    ```txt\n" +
				"• rewritten\n" +
				"    ```",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := cleanMarkdownV2Payload(tt.in); got != tt.want {
				t.Fatalf("cleanMarkdownV2Payload() = %q, want %q", got, tt.want)
			}
		})
	}
}

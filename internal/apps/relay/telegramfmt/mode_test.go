package telegramfmt

import (
	"strings"
	"testing"
)

func TestNormalizeMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty defaults to markdownv2", in: "", want: ModeMarkdownV2},
		{name: "whitespace defaults to markdownv2", in: " \t\n ", want: ModeMarkdownV2},
		{name: "trim and lowercase html", in: "  HTml ", want: ModeHTML},
		{name: "keeps unknown normalized", in: "  MD ", want: "md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeMode(tt.in); got != tt.want {
				t.Fatalf("NormalizeMode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "default for empty", in: "", want: ModeMarkdownV2},
		{name: "markdownv2", in: "markdownv2", want: ModeMarkdownV2},
		{name: "trim and lowercase", in: "  HTml ", want: ModeHTML},
		{name: "none", in: "none", want: ModeNone},
		{name: "invalid", in: "md", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ValidateMode(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateMode(%q) error = nil, want non-nil", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateMode(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ValidateMode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTelegramParseMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: ModeMarkdownV2, want: "MarkdownV2"},
		{in: ModeHTML, want: "HTML"},
		{in: ModeNone, want: ""},
		{in: "", want: "MarkdownV2"},
	}
	for _, tt := range tests {
		if got := TelegramParseMode(tt.in); got != tt.want {
			t.Fatalf("TelegramParseMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPromptRuleAndExample(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mode        string
		wantRule    string
		wantExample string
	}{
		{
			name:        "markdownv2",
			mode:        ModeMarkdownV2,
			wantRule:    "Relay converts it to Telegram MarkdownV2",
			wantExample: "**Build:** success. Run `relay start`.",
		},
		{
			name:        "html",
			mode:        ModeHTML,
			wantRule:    "Use Telegram HTML parse mode",
			wantExample: "<b>Build:</b> success. Run <code>relay start</code>.",
		},
		{
			name:        "none",
			mode:        ModeNone,
			wantRule:    "Use plain text only",
			wantExample: "Build: success. Run relay start.",
		},
		{
			name:        "unknown defaults to markdownv2",
			mode:        "md",
			wantRule:    "Relay converts it to Telegram MarkdownV2",
			wantExample: "**Build:** success. Run `relay start`.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotRule, gotExample := PromptRuleAndExample(tt.mode)
			if !strings.Contains(gotRule, tt.wantRule) {
				t.Fatalf("PromptRuleAndExample(%q) rule = %q, want to contain %q", tt.mode, gotRule, tt.wantRule)
			}
			if gotExample != tt.wantExample {
				t.Fatalf("PromptRuleAndExample(%q) example = %q, want %q", tt.mode, gotExample, tt.wantExample)
			}
		})
	}
}

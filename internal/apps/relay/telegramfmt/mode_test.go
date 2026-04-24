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
		name          string
		mode          string
		wantRuleParts []string
		denyRuleParts []string
		wantExample   string
	}{
		{
			name: "markdownv2",
			mode: ModeMarkdownV2,
			wantRuleParts: []string{
				"Write normal Markdown or plain text",
				"Relay converts it to Telegram MarkdownV2",
				"do not pre-escape Telegram MarkdownV2 reserved characters",
			},
			denyRuleParts: []string{
				"Use Telegram HTML parse mode",
				"Escape raw <, >, & as entities",
			},
			wantExample: "**Build:** success. Run `relay start`.",
		},
		{
			name: "html",
			mode: ModeHTML,
			wantRuleParts: []string{
				"Use Telegram HTML parse mode",
				"Supported tags: b/strong, i/em, u/ins, s/strike/del",
				"Escape raw <, >, & as entities",
			},
			denyRuleParts: []string{
				"Relay converts it to Telegram MarkdownV2",
				"do not pre-escape Telegram MarkdownV2 reserved characters",
			},
			wantExample: "<b>Build:</b> success. Run <code>relay start</code>.",
		},
		{
			name:          "none",
			mode:          ModeNone,
			wantRuleParts: []string{"Use plain text only", "Do not use Markdown or HTML markup"},
			denyRuleParts: []string{"Telegram MarkdownV2", "Telegram HTML parse mode"},
			wantExample:   "Build: success. Run relay start.",
		},
		{
			name: "unknown defaults to markdownv2",
			mode: "md",
			wantRuleParts: []string{
				"Write normal Markdown or plain text",
				"Relay converts it to Telegram MarkdownV2",
				"do not pre-escape Telegram MarkdownV2 reserved characters",
			},
			denyRuleParts: []string{"Use Telegram HTML parse mode"},
			wantExample:   "**Build:** success. Run `relay start`.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotRule, gotExample := PromptRuleAndExample(tt.mode)
			for _, want := range tt.wantRuleParts {
				if !strings.Contains(gotRule, want) {
					t.Fatalf("PromptRuleAndExample(%q) rule = %q, want to contain %q", tt.mode, gotRule, want)
				}
			}
			for _, denied := range tt.denyRuleParts {
				if strings.Contains(gotRule, denied) {
					t.Fatalf("PromptRuleAndExample(%q) rule = %q, should not contain %q", tt.mode, gotRule, denied)
				}
			}
			if gotExample != tt.wantExample {
				t.Fatalf("PromptRuleAndExample(%q) example = %q, want %q", tt.mode, gotExample, tt.wantExample)
			}
		})
	}
}

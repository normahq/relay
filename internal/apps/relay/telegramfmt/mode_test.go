package telegramfmt

import "testing"

func TestValidateMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "default for empty", in: "", want: ModeMarkdownV2},
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

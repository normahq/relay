package handlers

import (
	"strings"
	"testing"
)

func TestBuildAgentWelcomeMessage_FormatsStableKVLine(t *testing.T) {
	msg := BuildAgentWelcomeMessage("topic-alpha", "tg-1-2", "opencode_acp", "gpt-5", []string{"mcp.one", "mcp.two"})
	want := "name=topic-alpha session=tg-1-2 type=opencode_acp model=gpt-5 mcp=mcp.one,mcp.two"
	if msg != want {
		t.Fatalf("message = %q, want %q", msg, want)
	}
}

func TestBuildAgentWelcomeMessage_UsesNonePlaceholders(t *testing.T) {
	msg := BuildAgentWelcomeMessage(" ", " ", " ", " ", nil)
	if !strings.Contains(msg, "name=none") {
		t.Fatalf("message = %q, want name=none", msg)
	}
	if !strings.Contains(msg, "session=none") {
		t.Fatalf("message = %q, want session=none", msg)
	}
	if !strings.Contains(msg, "type=none") {
		t.Fatalf("message = %q, want type=none", msg)
	}
	if !strings.Contains(msg, "model=none") {
		t.Fatalf("message = %q, want model=none", msg)
	}
	if !strings.Contains(msg, "mcp=none") {
		t.Fatalf("message = %q, want mcp=none", msg)
	}
}

package handlers

import "testing"

func TestBuildInviteLink(t *testing.T) {
	t.Run("with username", func(t *testing.T) {
		got := buildInviteLink("NormaBot", "invite123")
		want := "https://t.me/NormaBot?start=invite_invite123"
		if got != want {
			t.Fatalf("buildInviteLink() = %q, want %q", got, want)
		}
	})

	t.Run("fallback username placeholder", func(t *testing.T) {
		got := buildInviteLink(" ", "invite123")
		want := "https://t.me/<bot_username>?start=invite_invite123"
		if got != want {
			t.Fatalf("buildInviteLink() = %q, want %q", got, want)
		}
	})
}

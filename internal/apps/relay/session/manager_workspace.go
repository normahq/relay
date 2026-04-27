package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/normahq/relay/internal/git"
)

// SessionBranchName returns the git branch name for a relay session.
func (m *Manager) SessionBranchName(sessionID string) string {
	return fmt.Sprintf("norma/relay/%s", sessionID)
}

func mergeUniqueStringIDs(base, extra []string) []string {
	if len(extra) == 0 {
		return append([]string(nil), base...)
	}

	out := make([]string, 0, len(base)+len(extra))
	seen := make(map[string]struct{}, len(base)+len(extra))
	appendUnique := func(raw string) {
		id := strings.TrimSpace(raw)
		if id == "" {
			return
		}
		if _, exists := seen[id]; exists {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}

	for _, id := range base {
		appendUnique(id)
	}
	for _, id := range extra {
		appendUnique(id)
	}

	return out
}

func (m *Manager) CommitWorkspace(ctx context.Context, chatID int64, topicID int) error {
	if !m.workspaceEnabled {
		return fmt.Errorf("workspace mode is disabled")
	}

	sessionID := NewTelegramSessionLocator(chatID, topicID).SessionID

	m.mu.RLock()
	ts, exists := m.sessions[sessionID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no session for topic %d", topicID)
	}

	workspaceDir := ts.workspaceDir
	if workspaceDir == "" {
		return fmt.Errorf("no workspace for topic %d", topicID)
	}

	statusOut, err := git.GitRunCmdOutput(ctx, workspaceDir, "git", "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("read workspace status: %w", err)
	}
	if status := statusOut; len(status) == 0 {
		return nil
	}

	if err := git.GitRunCmdErr(ctx, workspaceDir, "git", "add", "-A"); err != nil {
		return fmt.Errorf("stage workspace changes: %w", err)
	}

	commitMsg := fmt.Sprintf("chore: relay session %d/%d", chatID, topicID)
	if err := git.GitRunCmdErr(ctx, workspaceDir, "git", "commit", "-m", commitMsg); err != nil {
		return fmt.Errorf("commit workspace changes: %w", err)
	}

	return nil
}

package git

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
)

func MountWorktree(ctx context.Context, workingDir, workspaceDir, branchName, baseBranch string) (string, error) {
	// Ensure we prune any stale worktrees before adding a new one.
	_ = GitRunCmdErr(ctx, workingDir, "git", "worktree", "prune")

	// Check if we are in a git repo
	if !Available(ctx, workingDir) {
		return "", fmt.Errorf("not a git repository: %s", workingDir)
	}

	// Check if branch already exists
	branchExists := BranchExists(ctx, workingDir, branchName)

	args := []string{"worktree", "add", "-b", branchName, workspaceDir}
	if branchExists {
		// Allow restoring/starting sessions even when the same branch is
		// currently checked out in another worktree (including the main repo).
		args = []string{"worktree", "add", "--force", workspaceDir, branchName}
	} else if baseBranch != "" {
		args = append(args, baseBranch)
	}

	// Create worktree
	err := GitRunCmdErr(ctx, workingDir, "git", args...)
	if err != nil {
		return "", fmt.Errorf("git worktree add: %w", err)
	}

	return workspaceDir, nil
}

// BranchExists reports whether the given local branch exists in the repository.
func BranchExists(ctx context.Context, workingDir, branchName string) bool {
	return GitRunCmdErr(ctx, workingDir, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+branchName) == nil
}

func RemoveWorktree(ctx context.Context, workingDir, workspaceDir string) error {
	// Remove worktree only, keep the branch for restartable progress
	err := GitRunCmdErr(ctx, workingDir, "git", "worktree", "remove", "--force", workspaceDir)
	if err != nil {
		log.Warn().Err(err).Str("workspace_dir", workspaceDir).Msg("failed to remove git worktree")
	}

	return err
}

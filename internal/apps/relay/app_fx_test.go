package relay

import (
	"context"
	"testing"

	runtimeconfig "github.com/normahq/runtime/appconfig"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

func TestValidateApp(t *testing.T) {
	ctx := context.Background()
	workingDir := t.TempDir()
	runGitForRelay(t, ctx, workingDir, "init")

	cfg := Config{
		Relay: RelayConfig{
			Telegram: TelegramConfig{
				Token: "test-token",
			},
			WorkingDir: workingDir,
			StateDir:   ".config/relay",
			Workspace: WorkspaceConfig{
				Mode: string(WorkspaceModeAuto),
			},
		},
	}

	err := fx.ValidateApp(
		Module(
			cfg,
			runtimeconfig.RuntimeConfig{},
			"test-owner-token",
			runtimeconfig.RuntimeLoadOptions{WorkingDir: workingDir},
			nil,
		),
	)

	require.NoError(t, err)
}

func TestValidateApp_InvalidTelegramFormattingModeFails(t *testing.T) {
	ctx := context.Background()
	workingDir := t.TempDir()
	runGitForRelay(t, ctx, workingDir, "init")

	cfg := Config{
		Relay: RelayConfig{
			Telegram: TelegramConfig{
				Token:          "test-token",
				FormattingMode: "markdown",
			},
			WorkingDir: workingDir,
			StateDir:   ".config/relay",
			Workspace: WorkspaceConfig{
				Mode: string(WorkspaceModeAuto),
			},
		},
	}

	err := fx.ValidateApp(
		Module(
			cfg,
			runtimeconfig.RuntimeConfig{},
			"test-owner-token",
			runtimeconfig.RuntimeLoadOptions{WorkingDir: workingDir},
			nil,
		),
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid relay.telegram.formatting_mode")
}

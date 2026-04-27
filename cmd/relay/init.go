package main

import (
	"bufio"
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
)

const (
	relayConfigFileName   = "config.yaml"
	relayConfigRelDir     = ".config/relay"
	relayRuntimeStatePath = ".config/relay"
	relayDotEnvFileName   = ".env"
)

const relayConfigGitignoreContent = "*\n!.gitignore\n"

const (
	relayInitCodexModel      = "gpt-5.3-codex"
	relayInitClaudeCodeModel = "claude-sonnet-4-6"
)

const relayInitGlobalInstructionExample = "You are my relay agent.\nPrefer concise, actionable answers.\nUse relay.providers.start without a locator when you want a subagent in the current chat context.\nUse relay.workspace import/export instead of manual branch landing when workspace mode is enabled."

type relayTokenStorageMode string

const (
	relayTokenStorageEnv    relayTokenStorageMode = "env"
	relayTokenStorageConfig relayTokenStorageMode = "config"
)

type relayInitAgentTemplate struct {
	ID           string
	Type         string
	Model        string
	DetectBinary []string
}

var relayInitAgentTemplates = []relayInitAgentTemplate{
	{ID: "codex", Type: "codex_acp", Model: relayInitCodexModel, DetectBinary: []string{"codex"}},
	{ID: "opencode", Type: "opencode_acp", Model: "opencode/big-pickle", DetectBinary: []string{"opencode"}},
	{ID: "copilot", Type: "copilot_acp", Model: "gpt-5-codex", DetectBinary: []string{"copilot"}},
	{ID: "gemini", Type: "gemini_acp", Model: "gemini-3-flash-preview", DetectBinary: []string{"gemini"}},
	{ID: "claude_code", Type: "claude_code_acp", Model: relayInitClaudeCodeModel, DetectBinary: []string{"claudecode", "claude"}},
}

func initCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize relay config in the current repository",
		Long:  "Create .config/relay/config.yaml with relay defaults and autodetected runtime agents.",
		RunE: func(_ *cobra.Command, _ []string) error {
			workingDir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			relayConfigDir := filepath.Join(workingDir, relayConfigRelDir)
			if err := os.MkdirAll(relayConfigDir, 0o700); err != nil {
				return fmt.Errorf("create relay config directory: %w", err)
			}
			if err := ensureRelayConfigGitignore(relayConfigDir); err != nil {
				return err
			}

			configPath := filepath.Join(relayConfigDir, relayConfigFileName)
			if _, err := os.Stat(configPath); err == nil {
				return fmt.Errorf("%s already exists", configPath)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("stat %s: %w", configPath, err)
			}

			doc, agentIDs, err := buildRelayInitDocument(workingDir)
			if err != nil {
				return err
			}

			interactive := relayInitIsInteractive()
			inputReader := bufio.NewReader(relayInitInput)
			selectedRelayProvider, err := chooseRelayProvider(agentIDs, inputReader, relayInitOutput, interactive)
			if err != nil {
				return err
			}

			if err := setRelayProvider(doc, selectedRelayProvider); err != nil {
				return err
			}
			if err := setRelayGlobalInstructionExample(doc); err != nil {
				return err
			}
			telegramToken, bot, promptErr := promptRelayTelegramToken(inputReader, relayInitOutput, interactive)
			if promptErr != nil {
				return promptErr
			}
			tokenStorageMode, err := chooseRelayTelegramTokenStorage(inputReader, relayInitOutput, interactive)
			if err != nil {
				return err
			}
			storageTarget, err := storeRelayTelegramToken(doc, workingDir, telegramToken, tokenStorageMode)
			if err != nil {
				return err
			}

			stateDirRaw, err := relayStateDirFromInitDocument(doc)
			if err != nil {
				return err
			}
			stateDir, err := resolveRelayStateDir(workingDir, stateDirRaw)
			if err != nil {
				return fmt.Errorf("resolve relay state_dir: %w", err)
			}
			if err := os.MkdirAll(stateDir, 0o700); err != nil {
				return fmt.Errorf("create relay runtime state directory: %w", err)
			}
			dbPath := filepath.Join(stateDir, relayStateDBFileName)

			ownerToken, err := loadOrCreateRelayOwnerToken(context.Background(), dbPath)
			if err != nil {
				return fmt.Errorf("bootstrap relay owner token: %w", err)
			}

			content, err := yaml.Marshal(doc)
			if err != nil {
				return fmt.Errorf("marshal relay config: %w", err)
			}

			if err := os.WriteFile(configPath, content, 0o600); err != nil {
				return fmt.Errorf("write %s: %w", configPath, err)
			}

			_, _ = fmt.Fprintf(relayInitOutput, "relay initialized successfully\n")
			_, _ = fmt.Fprintf(relayInitOutput, "config: %s\n", configPath)
			_, _ = fmt.Fprintf(relayInitOutput, "state db: %s\n", dbPath)
			_, _ = fmt.Fprintf(relayInitOutput, "relay provider: %s\n", selectedRelayProvider)
			_, _ = fmt.Fprintf(relayInitOutput, "telegram token stored in: %s\n", storageTarget)
			_, _ = fmt.Fprintf(relayInitOutput, "start command: relay start\n")
			_, _ = fmt.Fprintf(relayInitOutput, "auth command: /start %s\n", ownerToken)
			_, _ = fmt.Fprintf(relayInitOutput, "auth url: %s\n", buildAuthURL(bot.username, ownerToken))

			return nil
		},
	}

	return cmd
}

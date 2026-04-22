package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/normahq/relay/internal/git"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	relayConfigFileName   = "config.yaml"
	relayConfigRelDir     = ".config/relay"
	relayRuntimeStatePath = ".config/relay"
	relayDotEnvFileName   = ".env"
)

const (
	relayInitCodexModel      = "gpt-5.3-codex"
	relayInitClaudeCodeModel = "claude-sonnet-4-6"
)

const relayInitSystemInstructionExample = "You are my relay agent.\nPrefer concise, actionable answers.\nUse relay.providers.start without a locator when you want a subagent in the current chat context.\nUse relay.workspace import/export instead of manual branch landing when workspace mode is enabled."

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

var (
	relayInitInput           io.Reader = os.Stdin
	relayInitOutput          io.Writer = os.Stdout
	relayInitIsInteractive             = defaultRelayInitIsInteractive
	relayInitLookPath                  = exec.LookPath
	relayInitCurrentBranch             = detectRelayInitCurrentBranch
	relayInitLoadBotIdentity           = loadBotIdentityFromToken
)

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

			configPath := filepath.Join(relayConfigDir, relayConfigFileName)
			if _, err := os.Stat(configPath); err == nil {
				return fmt.Errorf("%s already exists", configPath)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("stat %s: %w", configPath, err)
			}
			if err := writeRelayConfigGitignore(relayConfigDir); err != nil {
				return err
			}

			doc, agentIDs, err := buildRelayInitDocument(workingDir)
			if err != nil {
				return err
			}

			interactive := relayInitIsInteractive()
			inputReader := bufio.NewReader(relayInitInput)
			selectedRootProvider, err := chooseRelayProvider(agentIDs, inputReader, relayInitOutput, interactive)
			if err != nil {
				return err
			}

			if err := setRelayProvider(doc, selectedRootProvider); err != nil {
				return err
			}
			if err := setRelayAgentSystemInstructionExample(doc, selectedRootProvider); err != nil {
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
			_, _ = fmt.Fprintf(relayInitOutput, "root provider: %s\n", selectedRootProvider)
			_, _ = fmt.Fprintf(relayInitOutput, "telegram token stored in: %s\n", storageTarget)
			_, _ = fmt.Fprintf(relayInitOutput, "start command: relay start\n")
			_, _ = fmt.Fprintf(relayInitOutput, "auth command: /start %s\n", ownerToken)
			_, _ = fmt.Fprintf(relayInitOutput, "auth url: %s\n", buildAuthURL(bot.username, ownerToken))

			return nil
		},
	}

	return cmd
}

func writeRelayConfigGitignore(configDir string) error {
	gitignorePath := filepath.Join(configDir, ".gitignore")
	content := []byte("*\n!.gitignore\n!config.yaml\n")
	if err := os.WriteFile(gitignorePath, content, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", gitignorePath, err)
	}
	return nil
}

func buildRelayInitDocument(workingDir string) (map[string]any, []string, error) {
	detectedAgents := detectRelayInitAgents()
	if len(detectedAgents) == 0 {
		return nil, nil, fmt.Errorf(
			"no supported agent CLI detected in PATH; install at least one of: codex, opencode, copilot, gemini, claudecode/claude",
		)
	}

	var relayDefaults map[string]any
	if err := yaml.Unmarshal(defaultRelayConfig, &relayDefaults); err != nil {
		return nil, nil, fmt.Errorf("parse default relay config template: %w", err)
	}

	relaySection, ok := toStringAnyMap(relayDefaults["relay"])
	if !ok {
		return nil, nil, fmt.Errorf("default relay template is missing relay section")
	}
	ensureRelayMCPServersDefault(relaySection)
	relayBaseBranch, err := relayInitCurrentBranch(workingDir)
	if err != nil {
		relayBaseBranch = ""
	}
	if err := setRelayWorkspaceBaseBranch(relaySection, relayBaseBranch); err != nil {
		return nil, nil, err
	}

	agentIDs := make([]string, 0, len(detectedAgents))
	for _, detected := range detectedAgents {
		agentIDs = append(agentIDs, detected.ID)
	}

	doc := map[string]any{
		"runtime": map[string]any{
			"providers":   buildRelayInitAgents(detectedAgents),
			"mcp_servers": map[string]any{},
		},
		"relay":    relaySection,
		"profiles": buildRelayInitProfiles(agentIDs),
	}

	return doc, agentIDs, nil
}

func detectRelayInitCurrentBranch(workingDir string) (string, error) {
	return git.CurrentBranch(context.Background(), workingDir)
}

func detectRelayInitAgents() []relayInitAgentTemplate {
	detected := make([]relayInitAgentTemplate, 0, len(relayInitAgentTemplates))
	for _, template := range relayInitAgentTemplates {
		for _, binary := range template.DetectBinary {
			if _, err := relayInitLookPath(binary); err == nil {
				detected = append(detected, template)
				break
			}
		}
	}
	return detected
}

func buildRelayInitAgents(detected []relayInitAgentTemplate) map[string]any {
	agents := make(map[string]any, len(detected)+1)
	poolMembers := make([]any, 0, len(detected))

	for _, agentTemplate := range detected {
		agentBlock := map[string]any{"type": agentTemplate.Type}
		typeConfig := map[string]any{}
		if strings.TrimSpace(agentTemplate.Model) != "" {
			typeConfig["model"] = agentTemplate.Model
		}
		agentBlock[agentTemplate.Type] = typeConfig
		agents[agentTemplate.ID] = agentBlock
		poolMembers = append(poolMembers, agentTemplate.ID)
	}

	agents["pool"] = map[string]any{
		"type": "pool",
		"pool": map[string]any{
			"members": poolMembers,
		},
	}

	return agents
}

func buildRelayInitProfiles(agentIDs []string) map[string]any {
	profiles := make(map[string]any, len(agentIDs))
	if len(agentIDs) == 0 {
		return profiles
	}

	for _, id := range agentIDs {
		profiles[id] = map[string]any{
			"relay": map[string]any{
				"provider": id,
			},
		}
	}

	return profiles
}

func ensureRelayMCPServersDefault(relaySection map[string]any) {
	raw, exists := relaySection["mcp_servers"]
	if !exists || raw == nil {
		relaySection["mcp_servers"] = []any{}
		return
	}
	if _, ok := raw.([]any); ok {
		return
	}
	if _, ok := raw.([]string); ok {
		return
	}
	relaySection["mcp_servers"] = []any{}
}

func chooseRelayProvider(agentIDs []string, in io.Reader, out io.Writer, interactive bool) (string, error) {
	if len(agentIDs) == 0 {
		return "", fmt.Errorf("no provider ids are available for relay.provider selection")
	}
	if !interactive {
		return agentIDs[0], nil
	}
	return promptRelayProvider(agentIDs, in, out)
}

func promptRelayProvider(agentIDs []string, in io.Reader, out io.Writer) (string, error) {
	if len(agentIDs) == 0 {
		return "", fmt.Errorf("no provider ids are available for relay.provider selection")
	}

	_, _ = fmt.Fprintln(out, "Select relay.provider:")
	for i, id := range agentIDs {
		_, _ = fmt.Fprintf(out, "  %d) %s\n", i+1, id)
	}
	_, _ = fmt.Fprintf(out, "Enter number or provider id [1]: ")

	reader := asBufferedReader(in)
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("read relay.provider selection: %w", err)
		}

		value := strings.TrimSpace(line)
		if value == "" {
			return agentIDs[0], nil
		}

		if idx, parseErr := strconv.Atoi(value); parseErr == nil && idx >= 1 && idx <= len(agentIDs) {
			return agentIDs[idx-1], nil
		}

		if contains(agentIDs, value) {
			return value, nil
		}

		if err == io.EOF {
			return "", fmt.Errorf("invalid relay.provider selection %q", value)
		}

		_, _ = fmt.Fprintf(
			out,
			"Invalid selection %q. Enter number 1-%d or one of: %s\n",
			value,
			len(agentIDs),
			strings.Join(agentIDs, ", "),
		)
		_, _ = fmt.Fprintf(out, "Enter number or provider id [1]: ")
	}
}

func promptRelayTelegramToken(in io.Reader, out io.Writer, interactive bool) (string, botIdentity, error) {
	reader := asBufferedReader(in)
	for {
		_, _ = fmt.Fprint(out, "Enter Telegram bot token (required): ")
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", botIdentity{}, fmt.Errorf("read relay.telegram.token: %w", err)
		}

		token := strings.TrimSpace(line)
		if token == "" {
			if err == io.EOF || !interactive {
				return "", botIdentity{}, fmt.Errorf("relay.telegram.token is required")
			}
			_, _ = fmt.Fprintln(out, "Token is required.")
			continue
		}

		identity, validateErr := relayInitLoadBotIdentity(context.Background(), token)
		if validateErr == nil {
			return token, identity, nil
		}

		if err == io.EOF || !interactive {
			return "", botIdentity{}, fmt.Errorf("validate relay.telegram.token: %w", validateErr)
		}

		_, _ = fmt.Fprintf(out, "Token validation failed: %v\n", validateErr)
	}
}

func chooseRelayTelegramTokenStorage(in io.Reader, out io.Writer, interactive bool) (relayTokenStorageMode, error) {
	if !interactive {
		return relayTokenStorageEnv, nil
	}

	reader := asBufferedReader(in)
	_, _ = fmt.Fprintln(out, "Where should Telegram token be stored?")
	_, _ = fmt.Fprintln(out, "  1) .env (default)")
	_, _ = fmt.Fprintln(out, "  2) relay config file")
	_, _ = fmt.Fprint(out, "Enter choice [1]: ")

	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("read telegram token storage selection: %w", err)
		}

		value := strings.ToLower(strings.TrimSpace(line))
		switch value {
		case "", "1", ".env", "env":
			return relayTokenStorageEnv, nil
		case "2", "config", "config file":
			return relayTokenStorageConfig, nil
		}

		if err == io.EOF {
			return "", fmt.Errorf("invalid telegram token storage selection %q", value)
		}
		_, _ = fmt.Fprintf(out, "Invalid selection %q. Enter 1 (.env) or 2 (config file).\n", value)
		_, _ = fmt.Fprint(out, "Enter choice [1]: ")
	}
}

func storeRelayTelegramToken(
	doc map[string]any,
	workingDir string,
	token string,
	mode relayTokenStorageMode,
) (string, error) {
	switch mode {
	case relayTokenStorageEnv:
		if err := setRelayTelegramToken(doc, ""); err != nil {
			return "", err
		}
		dotEnvPath := filepath.Join(workingDir, relayDotEnvFileName)
		if err := upsertRelayTelegramTokenEnv(dotEnvPath, token); err != nil {
			return "", err
		}
		return dotEnvPath, nil
	case relayTokenStorageConfig:
		if err := setRelayTelegramToken(doc, token); err != nil {
			return "", err
		}
		return filepath.Join(workingDir, relayConfigRelDir, relayConfigFileName), nil
	default:
		return "", fmt.Errorf("unsupported telegram token storage mode %q", mode)
	}
}

func upsertRelayTelegramTokenEnv(dotEnvPath string, token string) error {
	line := "RELAY_TELEGRAM_TOKEN=" + strings.TrimSpace(token)

	content, err := os.ReadFile(dotEnvPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read %s: %w", dotEnvPath, err)
		}
		if writeErr := os.WriteFile(dotEnvPath, []byte(line+"\n"), 0o600); writeErr != nil {
			return fmt.Errorf("write %s: %w", dotEnvPath, writeErr)
		}
		return nil
	}

	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	out := make([]string, 0, len(lines))
	replaced := false
	for _, rawLine := range lines {
		if isRelayTelegramTokenEnvLine(rawLine) {
			if !replaced {
				out = append(out, line)
				replaced = true
			}
			continue
		}
		out = append(out, rawLine)
	}

	if !replaced {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, line)
	}

	updated := strings.Join(out, "\n")
	if !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}

	if err := os.WriteFile(dotEnvPath, []byte(updated), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", dotEnvPath, err)
	}
	return nil
}

func isRelayTelegramTokenEnvLine(rawLine string) bool {
	trimmed := strings.TrimSpace(rawLine)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	if strings.HasPrefix(trimmed, "export ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
	}
	idx := strings.Index(trimmed, "=")
	if idx < 0 {
		return false
	}
	key := strings.TrimSpace(trimmed[:idx])
	return key == "RELAY_TELEGRAM_TOKEN"
}

func setRelayProvider(doc map[string]any, providerID string) error {
	relaySection, ok := toStringAnyMap(doc["relay"])
	if !ok {
		return fmt.Errorf("relay section is missing from generated config")
	}
	relaySection["provider"] = providerID
	doc["relay"] = relaySection

	return nil
}

func setRelayTelegramToken(doc map[string]any, token string) error {
	relaySection, ok := toStringAnyMap(doc["relay"])
	if !ok {
		return fmt.Errorf("relay section is missing from generated config")
	}
	telegramSection, ok := toStringAnyMap(relaySection["telegram"])
	if !ok {
		return fmt.Errorf("relay.telegram section is missing from generated config")
	}
	telegramSection["token"] = token
	relaySection["telegram"] = telegramSection
	doc["relay"] = relaySection
	return nil
}

func setRelayAgentSystemInstructionExample(doc map[string]any, rootAgent string) error {
	relaySection, ok := toStringAnyMap(doc["relay"])
	if !ok {
		return fmt.Errorf("relay section is missing from generated config")
	}

	if existing, exists := relaySection["system_instructions"]; !exists || strings.TrimSpace(fmt.Sprintf("%v", existing)) == "" {
		relaySection["system_instructions"] = relayInitSystemInstructionExample
	}

	doc["relay"] = relaySection
	return nil
}

func setRelayWorkspaceBaseBranch(relaySection map[string]any, baseBranch string) error {
	workspaceSection, ok := toStringAnyMap(relaySection["workspace"])
	if !ok {
		return fmt.Errorf("relay.workspace section is missing from generated config")
	}
	workspaceSection["base_branch"] = strings.TrimSpace(baseBranch)
	relaySection["workspace"] = workspaceSection
	return nil
}

func relayStateDirFromInitDocument(doc map[string]any) (string, error) {
	relaySection, ok := toStringAnyMap(doc["relay"])
	if !ok {
		return "", fmt.Errorf("relay section is missing from generated config")
	}
	stateDirRaw, ok := relaySection["state_dir"]
	if !ok {
		return relayRuntimeStatePath, nil
	}
	stateDir := strings.TrimSpace(fmt.Sprintf("%v", stateDirRaw))
	if stateDir == "" {
		return "", fmt.Errorf("relay.state_dir is required")
	}
	return stateDir, nil
}

func toStringAnyMap(raw any) (map[string]any, bool) {
	switch v := raw.(type) {
	case map[string]any:
		return v, true
	case map[any]any:
		m := make(map[string]any, len(v))
		for key, value := range v {
			k, ok := key.(string)
			if !ok {
				return nil, false
			}
			m[k] = value
		}
		return m, true
	default:
		return nil, false
	}
}

func asBufferedReader(in io.Reader) *bufio.Reader {
	if reader, ok := in.(*bufio.Reader); ok {
		return reader
	}
	return bufio.NewReader(in)
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func defaultRelayInitIsInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

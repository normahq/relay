package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/normahq/relay/internal/git"
	"gopkg.in/yaml.v3"
)

func ensureRelayConfigGitignore(configDir string) error {
	gitignorePath := filepath.Join(configDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", gitignorePath, err)
	}
	if err := os.WriteFile(gitignorePath, []byte(relayConfigGitignoreContent), 0o600); err != nil {
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

func setRelayGlobalInstructionExample(doc map[string]any) error {
	relaySection, ok := toStringAnyMap(doc["relay"])
	if !ok {
		return fmt.Errorf("relay section is missing from generated config")
	}

	if existing, exists := relaySection["global_instruction"]; !exists || strings.TrimSpace(fmt.Sprintf("%v", existing)) == "" {
		relaySection["global_instruction"] = relayInitGlobalInstructionExample
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

var (
	relayInitInput           io.Reader = os.Stdin
	relayInitOutput          io.Writer = os.Stdout
	relayInitIsInteractive             = defaultRelayInitIsInteractive
	relayInitLookPath                  = exec.LookPath
	relayInitCurrentBranch             = detectRelayInitCurrentBranch
	relayInitLoadBotIdentity           = loadBotIdentityFromToken
)

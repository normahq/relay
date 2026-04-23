package welcome

import (
	"fmt"
	"strings"
)

const noneValue = "none"

// BuildAgentWelcomeMessage returns the markdown-formatted session welcome text.
func BuildAgentWelcomeMessage(name, sessionID, agentType, model string, mcpServers []string) string {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		cleanName = noneValue
	}

	cleanSessionID := strings.TrimSpace(sessionID)
	if cleanSessionID == "" {
		cleanSessionID = noneValue
	}

	cleanType := strings.TrimSpace(agentType)
	if cleanType == "" {
		cleanType = noneValue
	}

	cleanModel := strings.TrimSpace(model)
	if cleanModel == "" {
		cleanModel = noneValue
	}

	cleanMCP := normalizeMCPServers(mcpServers)
	if len(cleanMCP) == 0 {
		cleanMCP = []string{noneValue}
	}

	return fmt.Sprintf(
		"name=%s session=%s type=%s model=%s mcp=%s",
		cleanName,
		cleanSessionID,
		cleanType,
		cleanModel,
		strings.Join(cleanMCP, ","),
	)
}

func normalizeMCPServers(mcpServers []string) []string {
	if len(mcpServers) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(mcpServers))
	out := make([]string, 0, len(mcpServers))
	for _, serverID := range mcpServers {
		trimmed := strings.TrimSpace(serverID)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

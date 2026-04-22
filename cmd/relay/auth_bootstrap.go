package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/normahq/relay/internal/apps/relay/auth"
	relaystate "github.com/normahq/relay/internal/apps/relay/state"
)

const (
	relayStateDBFileName  = "relay.db"
	relayOwnerAuthTokenKV = "owner_auth_token"
)

var relayGenerateOwnerToken = auth.GenerateOwnerToken

func resolveRelayStateDir(workingDir, rawStateDir string) (string, error) {
	stateDir := strings.TrimSpace(rawStateDir)
	if stateDir == "" {
		return "", fmt.Errorf("relay.state_dir is required")
	}
	if !filepath.IsAbs(stateDir) {
		stateDir = filepath.Join(workingDir, stateDir)
	}

	resolved, err := filepath.Abs(stateDir)
	if err != nil {
		return "", fmt.Errorf("resolve relay state_dir %q: %w", rawStateDir, err)
	}
	return filepath.Clean(resolved), nil
}

func loadOrCreateRelayOwnerToken(ctx context.Context, dbPath string) (string, error) {
	provider, err := relaystate.NewSQLiteProvider(ctx, dbPath)
	if err != nil {
		return "", fmt.Errorf("open relay state provider: %w", err)
	}
	defer func() { _ = provider.Close() }()

	stored, ok, err := provider.AppKV().Get(ctx, relayOwnerAuthTokenKV)
	if err != nil {
		return "", fmt.Errorf("read owner auth token: %w", err)
	}
	if ok {
		trimmed := strings.TrimSpace(stored)
		if trimmed != "" {
			return trimmed, nil
		}
	}

	token, err := relayGenerateOwnerToken()
	if err != nil {
		return "", fmt.Errorf("generate owner auth token: %w", err)
	}
	if err := provider.AppKV().Set(ctx, relayOwnerAuthTokenKV, token); err != nil {
		return "", fmt.Errorf("store owner auth token: %w", err)
	}

	return token, nil
}

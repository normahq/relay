package main

import (
	"context"
	"path/filepath"
	"testing"
)

const testOwnerTokenPersisted = "owner-token-persisted"

func TestLoadOrCreateRelayOwnerToken_GeneratesAndReuses(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), relayStateDBFileName)

	previousGenerator := relayGenerateOwnerToken
	defer func() { relayGenerateOwnerToken = previousGenerator }()

	generateCalls := 0
	relayGenerateOwnerToken = func() (string, error) {
		generateCalls++
		return testOwnerTokenPersisted, nil
	}

	first, err := loadOrCreateRelayOwnerToken(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("loadOrCreateRelayOwnerToken(first): %v", err)
	}
	if first != testOwnerTokenPersisted {
		t.Fatalf("first token = %q, want %q", first, testOwnerTokenPersisted)
	}
	if generateCalls != 1 {
		t.Fatalf("generate calls after first = %d, want 1", generateCalls)
	}

	second, err := loadOrCreateRelayOwnerToken(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("loadOrCreateRelayOwnerToken(second): %v", err)
	}
	if second != testOwnerTokenPersisted {
		t.Fatalf("second token = %q, want %q", second, testOwnerTokenPersisted)
	}
	if generateCalls != 1 {
		t.Fatalf("generate calls after second = %d, want 1", generateCalls)
	}
}

func TestResolveRelayStateDir(t *testing.T) {
	workingDir := t.TempDir()

	resolved, err := resolveRelayStateDir(workingDir, ".config/relay")
	if err != nil {
		t.Fatalf("resolveRelayStateDir: %v", err)
	}

	want := filepath.Join(workingDir, ".config", "relay")
	if resolved != want {
		t.Fatalf("resolved = %q, want %q", resolved, want)
	}
}

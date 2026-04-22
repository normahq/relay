package runtimecfg

import (
	"os"
	"path/filepath"
	"testing"

	runtimeconfig "github.com/normahq/norma/pkg/runtime/appconfig"
)

func TestLoaderLoad_AppliesSelectedProfile(t *testing.T) {
	workingDir := t.TempDir()
	if err := writeRuntimeConfig(t, workingDir, `runtime:
  providers:
    opencode:
      type: opencode_acp
      opencode_acp:
        model: opencode/big-pickle
  mcp_servers:
    ext:
      type: http
      url: http://127.0.0.1:9090/mcp
profiles:
  default:
    relay:
      provider: opencode
      mcp_servers:
        - ext
      system_instructions: from profile
`); err != nil {
		t.Fatalf("writeRuntimeConfig() error = %v", err)
	}

	loader := NewLoader(runtimeconfig.RuntimeLoadOptions{WorkingDir: workingDir, Profile: "default"}, nil)
	snapshot, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if snapshot.Relay.Provider != "opencode" {
		t.Fatalf("provider = %q, want opencode", snapshot.Relay.Provider)
	}
	if len(snapshot.Relay.MCPServers) != 1 || snapshot.Relay.MCPServers[0] != "ext" {
		t.Fatalf("relay.mcp_servers = %#v, want [ext]", snapshot.Relay.MCPServers)
	}
	if got := snapshot.Relay.SystemInstructions; got != "from profile" {
		t.Fatalf("relay.system_instructions = %q, want from profile", got)
	}
}

func TestLoaderLoad_ReadsCurrentConfigOnEveryCall(t *testing.T) {
	workingDir := t.TempDir()
	loader := NewLoader(runtimeconfig.RuntimeLoadOptions{WorkingDir: workingDir, Profile: "default"}, nil)

	if err := writeRuntimeConfig(t, workingDir, `runtime:
  providers:
    alpha:
      type: opencode_acp
      opencode_acp:
        model: opencode/big-pickle
profiles:
  default:
    relay:
      provider: alpha
`); err != nil {
		t.Fatalf("writeRuntimeConfig(initial) error = %v", err)
	}

	first, err := loader.Load()
	if err != nil {
		t.Fatalf("Load(initial) error = %v", err)
	}
	if first.Relay.Provider != "alpha" {
		t.Fatalf("first provider = %q, want alpha", first.Relay.Provider)
	}

	if err := writeRuntimeConfig(t, workingDir, `runtime:
  providers:
    beta:
      type: opencode_acp
      opencode_acp:
        model: opencode/big-pickle
profiles:
  default:
    relay:
      provider: beta
`); err != nil {
		t.Fatalf("writeRuntimeConfig(updated) error = %v", err)
	}

	second, err := loader.Load()
	if err != nil {
		t.Fatalf("Load(updated) error = %v", err)
	}
	if second.Relay.Provider != "beta" {
		t.Fatalf("second provider = %q, want beta", second.Relay.Provider)
	}
}

func writeRuntimeConfig(t *testing.T, workingDir, content string) error {
	t.Helper()
	path := filepath.Join(workingDir, ".config", "relay", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

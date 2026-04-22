package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/normahq/norma/pkg/runtime/appconfig"
	relayapp "github.com/normahq/relay/internal/apps/relay"
)

type relayTestConfigDocument struct {
	Runtime appconfig.RuntimeConfig `mapstructure:"runtime"`
	Relay   relayapp.RelayConfig    `mapstructure:"relay"`
}

const testRelayDefaultProfile = "default"

func TestLoadConfigDocument_AppliesProfileRelayOverrides(t *testing.T) {
	workingDir := t.TempDir()
	t.Setenv("RELAY_TELEGRAM_WEBHOOK_ENABLED", "true")

	if err := writeFile(filepath.Join(workingDir, ".config", "relay", "config.yaml"), `runtime:
  providers:
    relay_agent:
      type: opencode_acp
      opencode_acp:
        model: opencode/big-pickle
profiles:
  default:
    relay:
      provider: relay_agent
`); err != nil {
		t.Fatalf("write relay config: %v", err)
	}

	var doc relayTestConfigDocument
	selectedProfile, err := appconfig.LoadConfigDocument(
		appconfig.RuntimeLoadOptions{WorkingDir: workingDir, Profile: testRelayDefaultProfile},
		appconfig.AppLoadOptions{
			AppName:      "relay",
			DefaultsYAML: defaultRelayConfig,
		},
		&doc,
	)
	if err != nil {
		t.Fatalf("LoadConfigDocument: %v", err)
	}
	if selectedProfile != testRelayDefaultProfile {
		t.Fatalf("profile = %q, want %s", selectedProfile, testRelayDefaultProfile)
	}

	relayCfg := relayapp.Config{Relay: doc.Relay}

	if relayCfg.Relay.Provider != "relay_agent" {
		t.Fatalf("provider = %q, want relay_agent", relayCfg.Relay.Provider)
	}
	if !relayCfg.Relay.Telegram.Webhook.Enabled {
		t.Fatal("webhook.enabled = false, want true from env override")
	}
}

func TestLoadConfigDocument_ImplicitDefaultProfileDoesNotRequireProfilesDefault(t *testing.T) {
	workingDir := t.TempDir()

	if err := writeFile(filepath.Join(workingDir, ".config", "relay", "config.yaml"), `runtime:
  providers:
    relay_agent:
      type: opencode_acp
      opencode_acp:
        model: opencode/big-pickle
profiles:
  codex:
    relay:
      provider: codex
relay:
  provider: relay_agent
`); err != nil {
		t.Fatalf("write relay config: %v", err)
	}

	var doc relayTestConfigDocument
	selectedProfile, err := appconfig.LoadConfigDocument(
		appconfig.RuntimeLoadOptions{WorkingDir: workingDir},
		appconfig.AppLoadOptions{
			AppName:      "relay",
			DefaultsYAML: defaultRelayConfig,
		},
		&doc,
	)
	if err != nil {
		t.Fatalf("LoadConfigDocument: %v", err)
	}
	if selectedProfile != testRelayDefaultProfile {
		t.Fatalf("profile = %q, want %s", selectedProfile, testRelayDefaultProfile)
	}
	if doc.Relay.Provider != "relay_agent" {
		t.Fatalf("provider = %q, want relay_agent", doc.Relay.Provider)
	}
}

func TestLoadConfigDocument_ExplicitMissingProfileFails(t *testing.T) {
	workingDir := t.TempDir()

	if err := writeFile(filepath.Join(workingDir, ".config", "relay", "config.yaml"), `runtime:
  providers:
    relay_agent:
      type: opencode_acp
      opencode_acp:
        model: opencode/big-pickle
profiles:
  codex:
    relay:
      provider: codex
relay:
  provider: relay_agent
`); err != nil {
		t.Fatalf("write relay config: %v", err)
	}

	var doc relayTestConfigDocument
	_, err := appconfig.LoadConfigDocument(
		appconfig.RuntimeLoadOptions{WorkingDir: workingDir, Profile: testRelayDefaultProfile},
		appconfig.AppLoadOptions{
			AppName:      "relay",
			DefaultsYAML: defaultRelayConfig,
		},
		&doc,
	)
	if err == nil {
		t.Fatal("expected error for missing explicit profile")
	}
	if got, want := err.Error(), `top-level profile "default" not found`; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestNewRootCommand_RegistersCommandsAndFlags(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "test-google-api-key")

	cmd, err := newRootCommand()
	if err != nil {
		t.Fatalf("newRootCommand: %v", err)
	}

	if _, _, err := cmd.Find([]string{"start"}); err != nil {
		t.Fatalf("start command missing: %v", err)
	}
	if _, _, err := cmd.Find([]string{"serve"}); err == nil {
		t.Fatal("serve command must not be registered")
	}
	if _, _, err := cmd.Find([]string{"init"}); err != nil {
		t.Fatalf("init command missing: %v", err)
	}

	for _, name := range []string{"config-dir", "profile", "debug", "trace"} {
		if cmd.PersistentFlags().Lookup(name) == nil {
			t.Fatalf("missing persistent flag %q", name)
		}
	}
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

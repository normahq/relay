package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/normahq/norma/pkg/runtime/agentconfig"
	"github.com/normahq/norma/pkg/runtime/agentfactory"
	runtimeconfig "github.com/normahq/norma/pkg/runtime/appconfig"
	"github.com/normahq/norma/pkg/runtime/mcpregistry"
	"github.com/normahq/norma/pkg/runtime/sessionstate"
	adksession "google.golang.org/adk/session"
)

func TestBundledMCPServerIDs(t *testing.T) {
	tests := []struct {
		name             string
		workspaceEnabled bool
		want             []string
	}{
		{
			name:             "workspace_disabled",
			workspaceEnabled: false,
			want:             []string{"relay"},
		},
		{
			name:             "workspace_enabled",
			workspaceEnabled: true,
			want:             []string{"relay"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bundledMCPServerIDs(tt.workspaceEnabled); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("bundledMCPServerIDs(%v) = %#v, want %#v", tt.workspaceEnabled, got, tt.want)
			}
		})
	}
}

func TestMergeMCPServerIDs(t *testing.T) {
	explicit := []string{" custom.one ", "relay", "", "custom.one", "custom.two"}
	extra := []string{"relay.extra", "custom.two", " "}
	got := mergeMCPServerIDs(explicit, extra, true)
	want := []string{"relay", "custom.one", "custom.two", "relay.extra"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeMCPServerIDs(%#v, %#v, true) = %#v, want %#v", explicit, extra, got, want)
	}
}

func TestBuildRelayInstruction_IncludesGlobalAndAgentInstruction(t *testing.T) {
	t.Parallel()

	builder := &Builder{
		normaCfg: runtimeconfig.RuntimeConfig{
			Providers: map[string]agentconfig.Config{
				"alpha": {
					SystemInstructions: "norma instruction",
				},
			},
		},
		relayGlobalInstruction: "relay instruction",
	}

	got := builder.buildRelayInstruction(
		"tg-1-2",
		"telegram",
		"alpha",
		"norma/relay/tg-1-2",
		"/tmp/work",
		"main",
	)

	wantSnippet := "Global instruction:\nrelay instruction\n\nInstruction:\nnorma instruction"
	if !strings.Contains(got, wantSnippet) {
		t.Fatalf("buildRelayInstruction() missing snippet %q in output:\n%s", wantSnippet, got)
	}
}

func TestProviderIDs(t *testing.T) {
	t.Parallel()

	builder := &Builder{
		normaCfg: runtimeconfig.RuntimeConfig{
			Providers: map[string]agentconfig.Config{
				"alpha": {},
				"":      {},
			},
		},
	}
	rawProviderID := " beta "
	builder.normaCfg.Providers[rawProviderID] = agentconfig.Config{}

	got := builder.ProviderIDs()
	want := []string{"alpha", "beta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProviderIDs() = %#v, want %#v", got, want)
	}
}

func TestBuildRelayInstruction_OmitsInstructionSectionsWhenEmpty(t *testing.T) {
	t.Parallel()

	builder := &Builder{}
	got := builder.buildRelayInstruction(
		"tg-1-2",
		"telegram",
		"alpha",
		"norma/relay/tg-1-2",
		"/tmp/work",
		"main",
	)

	if strings.Contains(got, "Global instruction:") || strings.Contains(got, "Instruction:") {
		t.Fatalf("buildRelayInstruction() unexpectedly contained instruction block:\n%s", got)
	}
}

func TestBuildRelayInstruction_IncludesGitWorkspaceContext(t *testing.T) {
	t.Parallel()

	builder := &Builder{
		workspaceEnabled:    true,
		workspaceBaseBranch: "main",
		workingDir:          "/repo",
	}

	got := builder.buildRelayInstruction(
		"tg-1-2",
		"telegram",
		"alpha",
		"norma/relay/tg-1-2",
		"/tmp/work",
		"develop",
	)

	wantSnippets := []string{
		"Workspace settings:",
		"This session belongs to channel type: telegram.",
		"Mode: git-worktree",
		"Path: /tmp/work",
		"Config path: /repo/.config/relay/config.yaml",
		"Base branch: main",
		"Session branch: norma/relay/tg-1-2",
		"Main repo branch at start: develop",
		"Git workspace guidance:",
	}
	for _, snippet := range wantSnippets {
		if !strings.Contains(got, snippet) {
			t.Fatalf("buildRelayInstruction() missing snippet %q in output:\n%s", snippet, got)
		}
	}
	if strings.Contains(got, "ask one short clarifying question") {
		t.Fatalf("buildRelayInstruction() unexpectedly included clarification mandate:\n%s", got)
	}
}

func TestBuildRelayInstruction_IncludesDirectModeSettingsWhenWorkspaceDisabled(t *testing.T) {
	t.Parallel()

	builder := &Builder{workspaceEnabled: false, workingDir: "/repo"}
	got := builder.buildRelayInstruction(
		"tg-1-2",
		"telegram",
		"alpha",
		"norma/relay/tg-1-2",
		"/tmp/work",
		"main",
	)

	wantSnippets := []string{
		"Workspace settings:",
		"This session belongs to channel type: telegram.",
		"Mode: direct",
		"Path: /tmp/work",
		"Config path: /repo/.config/relay/config.yaml",
		"Base branch: n/a",
		"Git workspace tooling: disabled",
	}
	for _, snippet := range wantSnippets {
		if !strings.Contains(got, snippet) {
			t.Fatalf("buildRelayInstruction() missing snippet %q in output:\n%s", snippet, got)
		}
	}

	if strings.Contains(got, "Git workspace guidance:") {
		t.Fatalf("buildRelayInstruction() unexpectedly included git guidance in direct mode:\n%s", got)
	}
	if strings.Contains(got, "Available namespaces:") {
		t.Fatalf("buildRelayInstruction() unexpectedly included generic MCP namespace docs:\n%s", got)
	}
}

func TestCreateRuntimeSession_IncludesCanonicalCWDState(t *testing.T) {
	t.Parallel()

	providers := map[string]agentconfig.Config{
		"alpha": {Type: "llm"},
	}
	builder := &Builder{
		factory:  agentfactory.New(providers, mcpregistry.New(nil)),
		normaCfg: runtimeconfig.RuntimeConfig{Providers: providers},
	}
	runtime := &BuiltRuntime{
		SessionSvc: adksession.InMemoryService(),
		AppName:    "norma-relay",
	}

	workspaceDir := t.TempDir()
	sess, err := builder.CreateRuntimeSession(context.Background(), runtime, "alpha", "user-1", "s-1", workspaceDir)
	if err != nil {
		t.Fatalf("CreateRuntimeSession() error = %v", err)
	}
	gotCWD, err := sess.State().Get(sessionstate.CWDKey)
	if err != nil {
		t.Fatalf("session state get %q error = %v", sessionstate.CWDKey, err)
	}
	if gotCWD != workspaceDir {
		t.Fatalf("session state %q = %v, want %q", sessionstate.CWDKey, gotCWD, workspaceDir)
	}
}

func TestCreateRuntimeSession_InvalidCWD_FailsBeforeCreate(t *testing.T) {
	t.Parallel()

	providers := map[string]agentconfig.Config{
		"alpha": {Type: agentconfig.AgentTypeGenericACP},
	}
	builder := &Builder{
		factory:  agentfactory.New(providers, mcpregistry.New(nil)),
		normaCfg: runtimeconfig.RuntimeConfig{Providers: providers},
	}
	runtime := &BuiltRuntime{
		SessionSvc: adksession.InMemoryService(),
		AppName:    "norma-relay",
	}

	_, err := builder.CreateRuntimeSession(context.Background(), runtime, "alpha", "user-1", "s-1", t.TempDir()+"/missing")
	if err == nil {
		t.Fatal("CreateRuntimeSession() error = nil, want invalid cwd error")
	}
	if !strings.Contains(err.Error(), "stat session cwd") {
		t.Fatalf("CreateRuntimeSession() error = %q, want stat session cwd", err)
	}
}

func TestCurrentRepoBranch_Fallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		builder Builder
		want    string
	}{
		{
			name:    "workspace_disabled",
			builder: Builder{workspaceEnabled: false},
			want:    "n/a",
		},
		{
			name:    "missing_working_dir",
			builder: Builder{workspaceEnabled: true},
			want:    "unknown",
		},
		{
			name:    "non_git_working_dir",
			builder: Builder{workspaceEnabled: true, workingDir: t.TempDir()},
			want:    "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.builder.currentRepoBranch(context.Background()); got != tt.want {
				t.Fatalf("currentRepoBranch() = %q, want %q", got, tt.want)
			}
		})
	}
}

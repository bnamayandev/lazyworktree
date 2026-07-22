package app

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/chmouel/lazyworktree/internal/config"
	"github.com/chmouel/lazyworktree/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forceInlineAgentPath makes openAgentSession take the inline branch by
// clearing the zellij environment markers for the duration of the test.
func forceInlineAgentPath(t *testing.T) {
	t.Helper()
	t.Setenv("ZELLIJ", "")
	t.Setenv("ZELLIJ_SESSION_NAME", "")
}

func TestOpenAgentSessionRunsConfiguredCommandInline(t *testing.T) {
	forceInlineAgentPath(t)
	t.Setenv("SHELL", "/bin/zsh")

	cfg := &config.AppConfig{
		WorktreeDir:  t.TempDir(),
		AgentCommand: "my-agent --flag",
	}
	m := NewModel(cfg, "")
	m.state.data.filteredWts = []*models.WorktreeInfo{{Path: testWorktreePath, Branch: "feat"}}
	m.state.data.selectedIndex = 0

	capture := &commandCapture{}
	m.commandRunner = capture.runner
	m.execProcess = capture.exec

	cmd := m.openAgentForSelectedWorktree()
	require.NotNil(t, cmd)

	assert.Equal(t, "/bin/zsh", capture.name)
	assert.Equal(t, []string{"-lc", "my-agent --flag"}, capture.args)
	assert.Equal(t, testWorktreePath, capture.dir)
	// Nothing may reach tmux any more; the agent is the pane's own process.
	assert.NotContains(t, strings.Join(capture.args, " "), "tmux")
}

func TestOpenAgentSessionDefaultsToClaude(t *testing.T) {
	forceInlineAgentPath(t)

	cfg := &config.AppConfig{WorktreeDir: t.TempDir()} // AgentCommand unset
	m := NewModel(cfg, "")

	capture := &commandCapture{}
	m.commandRunner = capture.runner
	m.execProcess = capture.exec

	cmd := m.openAgentSession(&models.WorktreeInfo{Path: testWorktreePath, Branch: "feat"})
	require.NotNil(t, cmd)
	assert.Equal(t, "claude", capture.args[1])
}

func TestHandleEnterKeyOpensAgentOnWorktreePane(t *testing.T) {
	forceInlineAgentPath(t)

	cfg := &config.AppConfig{WorktreeDir: t.TempDir(), AgentCommand: "my-agent"}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = paneWorktrees
	m.state.data.filteredWts = []*models.WorktreeInfo{{Path: testWorktreePath, Branch: "feat"}}
	m.state.data.selectedIndex = 0

	capture := &commandCapture{}
	m.commandRunner = capture.runner
	m.execProcess = capture.exec

	_, cmd := m.handleEnterKey()
	require.NotNil(t, cmd, "expected Enter to return an agent command")
	assert.Empty(t, m.selectedPath, "Enter must not set the shell-integration path")
	assert.Equal(t, "my-agent", capture.args[1])
}

func TestOpenAgentSessionUsesFloatingZellijPaneWhenInsideZellij(t *testing.T) {
	if _, err := exec.LookPath("zellij"); err != nil {
		t.Skip("zellij not installed")
	}
	t.Setenv("ZELLIJ", "0") // inside zellij

	cfg := &config.AppConfig{WorktreeDir: t.TempDir(), AgentCommand: "my-agent --flag"}
	m := NewModel(cfg, "")

	capture := &commandCapture{}
	// Keep the pane lookup off the real zellij server and report no existing
	// panes, so this exercises the creation path rather than the reuse path.
	m.commandRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if len(args) > 1 && args[1] == "list-panes" {
			return exec.Command("printf", "%s", "PANE_ID  TYPE  TITLE\n")
		}
		return capture.runner(ctx, name, args...)
	}
	m.execProcess = capture.exec

	cmd := m.openAgentSession(&models.WorktreeInfo{Path: testWorktreePath, Branch: "feat"})
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}
	if capture.name != "zellij" {
		t.Fatalf("expected zellij command, got %q", capture.name)
	}
	joined := strings.Join(capture.args, " ")
	for _, want := range []string{"new-pane", "--floating", "--width 90%", "--height 90%", "--cwd " + testWorktreePath, "my-agent --flag"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected zellij args to contain %q, got %q", want, joined)
		}
	}
}

func TestZellijPaneListContains(t *testing.T) {
	// Real "zellij action list-panes" output: two-space separated columns, and
	// unnamed panes carry whatever terminal title the running command reports.
	const paneList = `PANE_ID  TYPE  TITLE
plugin_0  plugin  (.) - zellij:link
terminal_1  terminal  ✳ Reviewing the agent branch
terminal_11  terminal  Pane #2
terminal_13  terminal  agent:feature-one
`

	tests := []struct {
		name     string
		output   string
		pane     string
		expected bool
	}{
		{name: "finds a named agent pane", output: paneList, pane: "agent:feature-one", expected: true},
		{name: "ignores a worktree without a pane", output: paneList, pane: "agent:feature-two", expected: false},
		{name: "does not match the header", output: paneList, pane: "TITLE", expected: false},
		{name: "tolerates empty output", output: "", pane: "agent:feature-one", expected: false},
		{name: "matches titles containing spaces", output: paneList, pane: "Pane #2", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, zellijPaneListContains(tt.output, tt.pane))
		})
	}
}

func TestOpenAgentSessionReusesExistingZellijPane(t *testing.T) {
	if _, err := exec.LookPath("zellij"); err != nil {
		t.Skip("zellij not installed")
	}
	t.Setenv("ZELLIJ", "0") // inside zellij

	cfg := &config.AppConfig{WorktreeDir: t.TempDir(), AgentCommand: "my-agent"}
	m := NewModel(cfg, "")

	var calls [][]string
	m.commandRunner = func(_ context.Context, name string, args ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, args...))
		if len(args) > 1 && args[1] == "list-panes" {
			// #nosec G204 -- test mock data, not user input
			return exec.Command("printf", "%s", "PANE_ID  TYPE  TITLE\nterminal_13  terminal  agent:wt\n")
		}
		return exec.Command("true")
	}

	cmd := m.openAgentSession(&models.WorktreeInfo{Path: testWorktreePath, Branch: "feat"})
	require.NotNil(t, cmd)
	cmd()

	require.Len(t, calls, 2, "expected a list-panes lookup followed by one action")
	assert.Equal(t, []string{"zellij", "action", "list-panes"}, calls[0])
	// Reuse must reveal the existing pane, never open a second one onto the
	// same agent.
	assert.Equal(t, []string{"zellij", "action", "show-floating-panes"}, calls[1])
	assert.NotContains(t, strings.Join(calls[1], " "), "new-pane")
}

func TestOpenAgentForSelectedWorktreeNoSelection(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir()}
	m := NewModel(cfg, "")
	m.state.data.selectedIndex = -1

	if cmd := m.openAgentForSelectedWorktree(); cmd != nil {
		t.Fatal("expected nil command when no worktree is selected")
	}
}

func TestResolveAgentCommandContinuesExistingChat(t *testing.T) {
	tests := []struct {
		name       string
		base       string
		hasSession bool
		expected   string
	}{
		{
			name:       "continues when a transcript exists",
			base:       "claude",
			hasSession: true,
			expected:   "claude --continue",
		},
		{
			name:       "starts fresh when nothing to continue",
			base:       "claude",
			hasSession: false,
			expected:   "claude",
		},
		{
			name:       "keeps existing flags and appends continue",
			base:       "claude --dangerously-skip-permissions",
			hasSession: true,
			expected:   "claude --dangerously-skip-permissions --continue",
		},
		{
			name:       "resolves an absolute claude path",
			base:       "/usr/local/bin/claude",
			hasSession: true,
			expected:   "/usr/local/bin/claude --continue",
		},
		{
			name:       "does not double up an explicit --continue",
			base:       "claude --continue",
			hasSession: true,
			expected:   "claude --continue",
		},
		{
			name:       "does not override an explicit --resume",
			base:       "claude --resume",
			hasSession: true,
			expected:   "claude --resume",
		},
		{
			name:       "does not override the short resume flag",
			base:       "claude -r",
			hasSession: true,
			expected:   "claude -r",
		},
		{
			name:       "leaves non-claude agents untouched",
			base:       "codex",
			hasSession: true,
			expected:   "codex",
		},
		{
			name:       "handles an empty command",
			base:       "",
			hasSession: true,
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, resolveAgentCommand(tt.base, tt.hasSession))
		})
	}
}

func TestHasResumableClaudeSession(t *testing.T) {
	const root = "/tmp/wt/feature"

	tests := []struct {
		name     string
		sessions []*models.AgentSession
		expected bool
	}{
		{
			name:     "claude session in the worktree root is resumable",
			sessions: []*models.AgentSession{{Agent: models.AgentKindClaude, CWD: root}},
			expected: true,
		},
		{
			name:     "trailing separators still match",
			sessions: []*models.AgentSession{{Agent: models.AgentKindClaude, CWD: root + "/"}},
			expected: true,
		},
		{
			// "--continue" only finds conversations recorded against the exact
			// launch directory, so a subdirectory session is not resumable.
			name:     "claude session in a subdirectory is not resumable",
			sessions: []*models.AgentSession{{Agent: models.AgentKindClaude, CWD: root + "/cmd/api"}},
			expected: false,
		},
		{
			name:     "pi session does not imply a claude conversation",
			sessions: []*models.AgentSession{{Agent: models.AgentKindPi, CWD: root}},
			expected: false,
		},
		{
			name:     "picks the claude session out of a mixed set",
			sessions: []*models.AgentSession{{Agent: models.AgentKindPi, CWD: root}, {Agent: models.AgentKindClaude, CWD: root}},
			expected: true,
		},
		{
			name:     "nil entries are skipped",
			sessions: []*models.AgentSession{nil},
			expected: false,
		},
		{
			name:     "no sessions at all",
			sessions: nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, hasResumableClaudeSession(tt.sessions, root))
		})
	}
}

func TestBuildAgentPaneCommandRunsAgentDirectly(t *testing.T) {
	tests := []struct {
		name      string
		shell     string
		wantShell string
	}{
		{name: "uses the login shell", shell: "/bin/zsh", wantShell: "/bin/zsh"},
		{name: "falls back to bash when SHELL is unset", shell: "", wantShell: testBashCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SHELL", tt.shell)
			cfg := &config.AppConfig{WorktreeDir: t.TempDir(), AgentCommand: "my-agent"}
			m := NewModel(cfg, "")

			argv := m.buildAgentPaneCommand(&models.WorktreeInfo{Path: testWorktreePath, Branch: "feat"})

			// The agent is the pane's own process, with no multiplexer between
			// them: the pane is what keeps it alive.
			assert.Equal(t, []string{tt.wantShell, "-lc", "my-agent"}, argv)
		})
	}
}

func TestOpenAgentSessionRunsInlineOutsideZellij(t *testing.T) {
	tests := []struct {
		name      string
		shell     string
		wantShell string
	}{
		{name: "uses the login shell", shell: "/bin/zsh", wantShell: "/bin/zsh"},
		{name: "falls back to bash when SHELL is unset", shell: "", wantShell: testBashCmd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forceInlineAgentPath(t)
			t.Setenv("SHELL", tt.shell)

			cfg := &config.AppConfig{WorktreeDir: t.TempDir(), AgentCommand: "my-agent --flag"}
			m := NewModel(cfg, "")

			capture := &commandCapture{}
			m.commandRunner = capture.runner
			m.execProcess = capture.exec

			cmd := m.openAgentSession(&models.WorktreeInfo{Path: testWorktreePath, Branch: "feat"})
			require.NotNil(t, cmd, "a bare terminal must still open an agent")
			assert.Equal(t, tt.wantShell, capture.name)
			assert.Equal(t, []string{"-lc", "my-agent --flag"}, capture.args)
			assert.Equal(t, testWorktreePath, capture.dir)
			assert.False(t, m.state.ui.screenManager.IsActive(), "must not raise an info screen")
		})
	}
}

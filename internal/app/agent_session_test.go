package app

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chmouel/lazyworktree/internal/config"
	"github.com/chmouel/lazyworktree/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forceTmuxAgentPath makes openAgentSession take the tmux fallback branch by
// clearing the zellij environment markers for the duration of the test.
func forceTmuxAgentPath(t *testing.T) {
	t.Helper()
	t.Setenv("ZELLIJ", "")
	t.Setenv("ZELLIJ_SESSION_NAME", "")
}

func TestOpenAgentSessionRunsConfiguredCommandInTmux(t *testing.T) {
	forceTmuxAgentPath(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

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
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}

	if capture.name != testBashCmd {
		t.Fatalf("expected bash command, got %q", capture.name)
	}
	if len(capture.args) != 2 || capture.args[0] != "-lc" {
		t.Fatalf("expected bash -lc args, got %v", capture.args)
	}
	if !strings.Contains(capture.args[1], "my-agent --flag") {
		t.Fatalf("expected tmux script to run the configured agent command, got %q", capture.args[1])
	}
	if capture.dir != testWorktreePath {
		t.Fatalf("expected worktree dir, got %q", capture.dir)
	}
}

func TestOpenAgentSessionDefaultsToClaude(t *testing.T) {
	forceTmuxAgentPath(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	cfg := &config.AppConfig{WorktreeDir: t.TempDir()} // AgentCommand unset
	m := NewModel(cfg, "")

	capture := &commandCapture{}
	m.commandRunner = capture.runner
	m.execProcess = capture.exec

	cmd := m.openAgentSession(&models.WorktreeInfo{Path: testWorktreePath, Branch: "feat"})
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}
	if !strings.Contains(capture.args[1], "claude") {
		t.Fatalf("expected tmux script to default to claude, got %q", capture.args[1])
	}
}

func TestHandleEnterKeyOpensAgentOnWorktreePane(t *testing.T) {
	forceTmuxAgentPath(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	cfg := &config.AppConfig{WorktreeDir: t.TempDir(), AgentCommand: "my-agent"}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = paneWorktrees
	m.state.data.filteredWts = []*models.WorktreeInfo{{Path: testWorktreePath, Branch: "feat"}}
	m.state.data.selectedIndex = 0

	capture := &commandCapture{}
	m.commandRunner = capture.runner
	m.execProcess = capture.exec

	_, cmd := m.handleEnterKey()
	if cmd == nil {
		t.Fatal("expected Enter to return an agent command")
	}
	if m.selectedPath != "" {
		t.Fatalf("Enter must not set the shell-integration path, got %q", m.selectedPath)
	}
	if !strings.Contains(capture.args[1], "my-agent") {
		t.Fatalf("expected tmux script to run the configured agent command, got %q", capture.args[1])
	}
}

func TestOpenAgentSessionUsesFloatingZellijPaneWhenInsideZellij(t *testing.T) {
	if _, err := exec.LookPath("zellij"); err != nil {
		t.Skip("zellij not installed")
	}
	t.Setenv("ZELLIJ", "0") // inside zellij

	cfg := &config.AppConfig{WorktreeDir: t.TempDir(), AgentCommand: "my-agent --flag"}
	m := NewModel(cfg, "")

	capture := &commandCapture{}
	m.commandRunner = capture.runner
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

func TestBuildAgentPaneCommandWrapsInPersistentTmux(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir:   t.TempDir(),
		AgentCommand:  "my-agent",
		SessionPrefix: "wt-",
	}
	m := NewModel(cfg, "")
	wt := &models.WorktreeInfo{Path: testWorktreePath, Branch: "feat"}

	t.Run("tmux backs the pane so the agent outlives it", func(t *testing.T) {
		argv := m.buildAgentPaneCommand(wt, true)
		require.GreaterOrEqual(t, len(argv), 7)
		assert.Equal(t, "tmux", argv[0])
		assert.Equal(t, "new-session", argv[1])
		// -A attaches to the running session instead of starting a second one.
		assert.Equal(t, "-A", argv[2])
		assert.Equal(t, "-s", argv[3])
		assert.Equal(t, "wt-"+filepath.Base(testWorktreePath), argv[4])
		assert.Equal(t, "-c", argv[5])
		assert.Equal(t, testWorktreePath, argv[6])
		assert.Contains(t, strings.Join(argv, " "), "my-agent")
	})

	t.Run("falls back to running the agent directly without tmux", func(t *testing.T) {
		argv := m.buildAgentPaneCommand(wt, false)
		assert.NotContains(t, argv, "tmux")
		assert.Equal(t, "my-agent", argv[len(argv)-1])
		assert.Equal(t, "-lc", argv[len(argv)-2])
	})
}

func TestAgentTmuxSessionNameIsStablePerWorktree(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir(), SessionPrefix: "wt-"}
	m := NewModel(cfg, "")

	// A deterministic, per-worktree name is what makes Enter re-attach to the
	// running agent rather than spawn a second one.
	assert.Equal(t, "wt-"+filepath.Base(testWorktreePath),
		m.agentTmuxSessionName(&models.WorktreeInfo{Path: testWorktreePath, Branch: "feat"}))
	assert.NotEqual(t,
		m.agentTmuxSessionName(&models.WorktreeInfo{Path: testWorktreePath, Branch: "feat"}),
		m.agentTmuxSessionName(&models.WorktreeInfo{Path: "/tmp/other-wt", Branch: "feat"}),
		"distinct worktrees must not share an agent session")
}

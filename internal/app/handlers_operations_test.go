package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	appscreen "github.com/chmouel/lazyworktree/internal/app/screen"
	"github.com/chmouel/lazyworktree/internal/config"
	"github.com/chmouel/lazyworktree/internal/models"
)

func TestJumpKeySelectsWorktree(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 0
	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: filepath.Join(cfg.WorktreeDir, "wt"), Branch: testFeat},
	}
	m.state.data.selectedIndex = 0

	_, cmd, handled := m.handleOperationKey(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !handled {
		t.Fatal("expected 'a' to be handled")
	}
	if m.selectedPath == "" {
		t.Fatal("expected selected path to be set")
	}
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}
}

func TestJumpAfterNavigationUsesHighlightedWorktree(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 0
	m.state.ui.worktreeTable.Focus()

	firstPath := filepath.Join(cfg.WorktreeDir, "wt1")
	secondPath := filepath.Join(cfg.WorktreeDir, "wt2")
	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: firstPath, Branch: testFeat},
		{Path: secondPath, Branch: testOtherBranch},
	}
	m.state.ui.worktreeTable.SetRows([]table.Row{
		{"wt1"},
		{"wt2"},
	})
	m.state.ui.worktreeTable.SetCursor(0)
	m.state.data.selectedIndex = 0

	_, _ = m.handleNavigationDown(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.state.ui.worktreeTable.Cursor() != 1 {
		t.Fatalf("expected cursor to move to 1, got %d", m.state.ui.worktreeTable.Cursor())
	}

	_, cmd, handled := m.handleOperationKey(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !handled {
		t.Fatal("expected 'a' to be handled")
	}
	if m.selectedPath != secondPath {
		t.Fatalf("expected selected path %q, got %q", secondPath, m.selectedPath)
	}
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}
}

// TestStatusFileEnterShowsDiff tests that Enter on pane 2 triggers showFileDiff.
func TestStatusFileEnterShowsDiff(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 2
	m.state.ui.statusViewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))

	// Set up worktree and status files
	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: filepath.Join(cfg.WorktreeDir, "wt1"), Branch: "feature"},
	}
	m.state.data.selectedIndex = 0
	m.setStatusFiles([]StatusFile{
		{Filename: "file1.go", Status: ".M", IsUntracked: false},
		{Filename: "file2.go", Status: "M.", IsUntracked: false},
	})
	m.state.services.statusTree.Index = 1

	// Mock execProcess to capture the command
	var capturedCmd bool
	m.execProcess = func(_ *exec.Cmd, cb tea.ExecCallback) tea.Cmd {
		capturedCmd = true
		return func() tea.Msg { return cb(nil) }
	}

	_, cmd := m.handleEnterKey()
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}

	// Execute the command
	_ = cmd()

	if !capturedCmd {
		t.Fatal("expected execProcess to be called")
	}
}

func TestLogPaneDiffCommandModeUsesCommitRange(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir:         t.TempDir(),
		GitPager:            "lumen",
		GitPagerCommandMode: true,
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 3
	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: testWorktreePath, Branch: testFeat},
	}
	m.state.data.selectedIndex = 0
	m.setLogEntries([]commitLogEntry{
		{sha: "abc123", authorInitials: "ab", message: "Fix parser"},
	}, true)

	capture := &commandCapture{}
	m.commandRunner = capture.runner
	m.execProcess = capture.exec

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 'd', Text: string('d')})
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}

	if capture.name != testBashCmd {
		t.Fatalf("expected bash command, got %q", capture.name)
	}
	if len(capture.args) != 2 || capture.args[0] != "-c" {
		t.Fatalf("expected bash -c args, got %v", capture.args)
	}
	if !strings.Contains(capture.args[1], "lumen diff abc123^..abc123") {
		t.Fatalf("expected commit range command, got %q", capture.args[1])
	}
}

func TestStatusFileEditOpensEditor(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
		Editor:      "nvim",
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 2
	m.state.ui.statusViewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))

	wtPath := filepath.Join(cfg.WorktreeDir, "wt1")
	if err := os.MkdirAll(wtPath, 0o700); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	filename := "file1.go"
	if err := os.WriteFile(filepath.Join(wtPath, filename), []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: wtPath, Branch: "feature"},
	}
	m.state.data.selectedIndex = 0
	m.setStatusFiles([]StatusFile{
		{Filename: filename, Status: ".M", IsUntracked: false},
	})
	m.state.services.statusTree.Index = 0

	var gotCmd *exec.Cmd
	m.execProcess = func(cmd *exec.Cmd, cb tea.ExecCallback) tea.Cmd {
		gotCmd = cmd
		return func() tea.Msg { return cb(nil) }
	}

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 'e', Text: string('e')})
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}
	_ = cmd()

	if gotCmd == nil {
		t.Fatal("expected execProcess to be called")
	}
	if gotCmd.Dir != wtPath {
		t.Fatalf("expected worktree dir %q, got %q", wtPath, gotCmd.Dir)
	}
	if len(gotCmd.Args) < 3 || gotCmd.Args[0] != testBashCmd || gotCmd.Args[1] != "-c" {
		t.Fatalf("expected bash -c command, got %v", gotCmd.Args)
	}
	if !strings.Contains(gotCmd.Args[2], "nvim") || !strings.Contains(gotCmd.Args[2], filename) {
		t.Fatalf("expected editor command to include nvim and file, got %q", gotCmd.Args[2])
	}
}

func TestCommitAllChangesFromStatusPane(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 2
	m.state.ui.statusViewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))

	wtPath := filepath.Join(cfg.WorktreeDir, "wt1")
	if err := os.MkdirAll(wtPath, 0o700); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: wtPath, Branch: "feature"},
	}
	m.state.data.selectedIndex = 0

	// Add a staged file to skip the confirmation prompt
	m.state.data.statusFilesAll = []StatusFile{
		{Filename: "staged.txt", Status: "M "},
	}

	var gotCmd *exec.Cmd
	m.execProcess = func(cmd *exec.Cmd, cb tea.ExecCallback) tea.Cmd {
		gotCmd = cmd
		return func() tea.Msg { return cb(nil) }
	}

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 'C', Text: string('C')})
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}
	_ = cmd()

	if gotCmd == nil {
		t.Fatal("expected execProcess to be called")
	}
	if gotCmd.Dir != wtPath {
		t.Fatalf("expected worktree dir %q, got %q", wtPath, gotCmd.Dir)
	}
	if len(gotCmd.Args) < 3 || gotCmd.Args[0] != testBashCmd || gotCmd.Args[1] != "-c" {
		t.Fatalf("expected bash -c command, got %v", gotCmd.Args)
	}
	if !strings.Contains(gotCmd.Args[2], "git commit") {
		t.Fatalf("expected git commit command, got %q", gotCmd.Args[2])
	}
}

func TestCommitAllChangesNotInStatusPane(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 0 // Not status pane

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 'C', Text: string('C')})
	if cmd != nil {
		t.Fatal("expected no command when not in status pane")
	}
}

func TestCommitStagedChangesFromStatusPane(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 2
	m.state.ui.statusViewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))

	wtPath := filepath.Join(cfg.WorktreeDir, "wt1")
	if err := os.MkdirAll(wtPath, 0o700); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: wtPath, Branch: "feature"},
	}
	m.state.data.selectedIndex = 0

	// Set up staged changes
	m.setStatusFiles([]StatusFile{
		{Filename: "file1.go", Status: "M ", IsUntracked: false}, // Staged modification
	})

	m.execProcess = func(cmd *exec.Cmd, cb tea.ExecCallback) tea.Cmd {
		return func() tea.Msg { return cb(nil) }
	}

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 'c', Text: string('c')})
	if cmd != nil {
		t.Fatal("expected nil command because modal screen should be shown")
	}

	// Verify CommitMessageScreen was pushed
	if m.state.ui.screenManager.Type() != appscreen.TypeCommitMessage {
		t.Fatalf("expected CommitMessageScreen to be pushed, got %s", m.state.ui.screenManager.Type())
	}
}

func TestCtrlGOpensCommitScreenFromStatusPane(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 2
	m.state.ui.statusViewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))

	wtPath := filepath.Join(cfg.WorktreeDir, "wt1")
	if err := os.MkdirAll(wtPath, 0o700); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: wtPath, Branch: "feature"},
	}
	m.state.data.selectedIndex = 0
	m.setStatusFiles([]StatusFile{
		{Filename: "file1.go", Status: "M ", IsUntracked: false},
	})

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatal("expected nil command because modal screen should be shown")
	}

	if m.state.ui.screenManager.Type() != appscreen.TypeCommitMessage {
		t.Fatalf("expected CommitMessageScreen to be pushed, got %s", m.state.ui.screenManager.Type())
	}
}

func TestCtrlGOpensCommitScreenOutsideStatusPane(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 0
	m.state.ui.statusViewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))

	wtPath := filepath.Join(cfg.WorktreeDir, "wt1")
	if err := os.MkdirAll(wtPath, 0o700); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: wtPath, Branch: "feature"},
	}
	m.state.data.selectedIndex = 0
	m.setStatusFiles([]StatusFile{
		{Filename: "file1.go", Status: "M ", IsUntracked: false},
	})

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatal("expected nil command because modal screen should be shown")
	}

	if m.state.ui.screenManager.Type() != appscreen.TypeCommitMessage {
		t.Fatalf("expected CommitMessageScreen to be pushed, got %s", m.state.ui.screenManager.Type())
	}
}

func TestCommitStagedChangesNoStagedFiles(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 2
	m.state.ui.statusViewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))

	wtPath := filepath.Join(cfg.WorktreeDir, "wt1")
	if err := os.MkdirAll(wtPath, 0o700); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: wtPath, Branch: "feature"},
	}
	m.state.data.selectedIndex = 0

	// Set up only unstaged changes (no staged)
	m.setStatusFiles([]StatusFile{
		{Filename: "file1.go", Status: " M", IsUntracked: false}, // Unstaged modification
	})

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 'c', Text: string('c')})
	if cmd != nil {
		t.Fatal("expected no command when no staged changes")
	}

	// Should show confirm screen with message
	if !m.state.ui.screenManager.IsActive() || m.state.ui.screenManager.Type() != appscreen.TypeConfirm {
		t.Fatalf("expected confirm screen, got active=%t type=%s", m.state.ui.screenManager.IsActive(), m.state.ui.screenManager.Type())
	}
}

func TestCommitStagedChangesNotInStatusPane(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 0 // Not status pane

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 'c', Text: string('c')})
	// When not in status pane, 'c' should trigger create worktree which returns a command
	if cmd == nil {
		t.Fatal("expected command for create worktree when not in status pane")
	}
}

// Note: The smart PR sync tests that check isBehindBase() require a real git repository
// with actual commits and branches. Those are tested via integration tests.
// Here we test the simpler cases that don't require mocking git internals.

func TestStageUnstagedFile(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 2
	m.state.ui.statusViewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))

	wtPath := filepath.Join(cfg.WorktreeDir, "wt1")
	if err := os.MkdirAll(wtPath, 0o700); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: wtPath, Branch: "feature"},
	}
	m.state.data.selectedIndex = 0
	m.setStatusFiles([]StatusFile{
		{Filename: "file1.go", Status: " M", IsUntracked: false}, // Unstaged modification
	})
	m.state.services.statusTree.Index = 0

	var gotCmd *exec.Cmd
	m.commandRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotCmd = exec.CommandContext(ctx, name, args...) //#nosec G204,G702 -- test mock with controlled args
		return gotCmd
	}

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 's', Text: string('s')})
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}

	if gotCmd == nil {
		t.Fatal("expected commandRunner to be called")
	}
	if gotCmd.Dir != wtPath {
		t.Fatalf("expected worktree dir %q, got %q", wtPath, gotCmd.Dir)
	}
	if len(gotCmd.Args) < 3 || gotCmd.Args[0] != testBashCmd || gotCmd.Args[1] != "-c" {
		t.Fatalf("expected bash -c command, got %v", gotCmd.Args)
	}
	if !strings.Contains(gotCmd.Args[2], "git add") {
		t.Fatalf("expected git add command, got %q", gotCmd.Args[2])
	}
}

func TestUnstageStagedFile(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 2
	m.state.ui.statusViewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))

	wtPath := filepath.Join(cfg.WorktreeDir, "wt1")
	if err := os.MkdirAll(wtPath, 0o700); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: wtPath, Branch: "feature"},
	}
	m.state.data.selectedIndex = 0
	m.setStatusFiles([]StatusFile{
		{Filename: "file1.go", Status: "M ", IsUntracked: false}, // Staged modification
	})
	m.state.services.statusTree.Index = 0

	var gotCmd *exec.Cmd
	m.commandRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotCmd = exec.CommandContext(ctx, name, args...) //#nosec G204,G702 -- test mock with controlled args
		return gotCmd
	}

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 's', Text: string('s')})
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}

	if gotCmd == nil {
		t.Fatal("expected commandRunner to be called")
	}
	if !strings.Contains(gotCmd.Args[2], "git restore --staged") {
		t.Fatalf("expected git restore --staged command, got %q", gotCmd.Args[2])
	}
}

func TestStageMixedStatusFile(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 2
	m.state.ui.statusViewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))

	wtPath := filepath.Join(cfg.WorktreeDir, "wt1")
	if err := os.MkdirAll(wtPath, 0o700); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: wtPath, Branch: "feature"},
	}
	m.state.data.selectedIndex = 0
	m.setStatusFiles([]StatusFile{
		{Filename: "file1.go", Status: "MM", IsUntracked: false}, // Both staged and unstaged
	})
	m.state.services.statusTree.Index = 0

	var gotCmd *exec.Cmd
	m.commandRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotCmd = exec.CommandContext(ctx, name, args...) //#nosec G204,G702 -- test mock with controlled args
		return gotCmd
	}

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 's', Text: string('s')})
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}

	if gotCmd == nil {
		t.Fatal("expected commandRunner to be called")
	}
	if !strings.Contains(gotCmd.Args[2], "git add") {
		t.Fatalf("expected git add command for mixed status, got %q", gotCmd.Args[2])
	}
}

func TestStageFileNotInStatusPane(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 0 // Not status pane
	m.setStatusFiles([]StatusFile{
		{Filename: "file1.go", Status: " M", IsUntracked: false},
	})
	m.state.services.statusTree.Index = 0

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 's', Text: string('s')})
	if cmd != nil {
		t.Fatal("expected no command when not in status pane")
	}
}

func TestStageDirectoryAllUnstaged(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 2
	m.state.ui.statusViewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))
	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: cfg.WorktreeDir, Branch: "feature"},
	}
	m.state.data.selectedIndex = 0

	// Build a tree with a directory containing unstaged files
	m.setStatusFiles([]StatusFile{
		{Filename: "src/file1.go", Status: " M", IsUntracked: false},
		{Filename: "src/file2.go", Status: " M", IsUntracked: false},
	})
	m.state.services.statusTree.Index = 0 // Select the directory

	if len(m.state.services.statusTree.TreeFlat) < 2 || !m.state.services.statusTree.TreeFlat[0].IsDir() {
		t.Fatal("expected directory node at index 0")
	}

	var gotCmd *exec.Cmd
	m.commandRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotCmd = exec.CommandContext(ctx, name, args...) //#nosec G204,G702 -- test mock with controlled args
		return gotCmd
	}

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 's', Text: string('s')})
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}

	if gotCmd == nil {
		t.Fatal("expected commandRunner to be called")
	}
	if !strings.Contains(gotCmd.Args[2], "git add") {
		t.Fatalf("expected git add command for unstaged directory, got %q", gotCmd.Args[2])
	}
	// Verify both files are included
	if !strings.Contains(gotCmd.Args[2], "file1.go") || !strings.Contains(gotCmd.Args[2], "file2.go") {
		t.Fatalf("expected both files in git add command, got %q", gotCmd.Args[2])
	}
}

func TestStageDirectoryAllStaged(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 2
	m.state.ui.statusViewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))
	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: cfg.WorktreeDir, Branch: "feature"},
	}
	m.state.data.selectedIndex = 0

	// Build a tree with a directory containing fully staged files
	m.setStatusFiles([]StatusFile{
		{Filename: "src/file1.go", Status: "M ", IsUntracked: false},
		{Filename: "src/file2.go", Status: "A ", IsUntracked: false},
	})
	m.state.services.statusTree.Index = 0 // Select the directory

	if len(m.state.services.statusTree.TreeFlat) < 2 || !m.state.services.statusTree.TreeFlat[0].IsDir() {
		t.Fatal("expected directory node at index 0")
	}

	var gotCmd *exec.Cmd
	m.commandRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotCmd = exec.CommandContext(ctx, name, args...) //#nosec G204,G702 -- test mock with controlled args
		return gotCmd
	}

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 's', Text: string('s')})
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}

	if gotCmd == nil {
		t.Fatal("expected commandRunner to be called")
	}
	if !strings.Contains(gotCmd.Args[2], "git restore --staged") {
		t.Fatalf("expected git restore --staged command for fully staged directory, got %q", gotCmd.Args[2])
	}
}

func TestStageDirectoryMixed(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 2
	m.state.ui.statusViewport = viewport.New(viewport.WithWidth(40), viewport.WithHeight(10))
	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: cfg.WorktreeDir, Branch: "feature"},
	}
	m.state.data.selectedIndex = 0

	// Build a tree with a directory containing mixed status files
	m.setStatusFiles([]StatusFile{
		{Filename: "src/file1.go", Status: "M ", IsUntracked: false}, // Staged
		{Filename: "src/file2.go", Status: " M", IsUntracked: false}, // Unstaged
	})
	m.state.services.statusTree.Index = 0 // Select the directory

	if len(m.state.services.statusTree.TreeFlat) < 2 || !m.state.services.statusTree.TreeFlat[0].IsDir() {
		t.Fatal("expected directory node at index 0")
	}

	var gotCmd *exec.Cmd
	m.commandRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotCmd = exec.CommandContext(ctx, name, args...) //#nosec G204,G702 -- test mock with controlled args
		return gotCmd
	}

	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 's', Text: string('s')})
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}

	if gotCmd == nil {
		t.Fatal("expected commandRunner to be called")
	}
	// Mixed status should stage all files
	if !strings.Contains(gotCmd.Args[2], "git add") {
		t.Fatalf("expected git add command for mixed status directory, got %q", gotCmd.Args[2])
	}
}

func TestCollectFiles(t *testing.T) {
	// Test CollectFiles on a directory node
	dirNode := &StatusTreeNode{
		Path: "src",
		File: nil, // Directory
		Children: []*StatusTreeNode{
			{
				Path: "src/file1.go",
				File: &StatusFile{Filename: "src/file1.go", Status: " M"},
			},
			{
				Path: "src/sub",
				File: nil, // Subdirectory
				Children: []*StatusTreeNode{
					{
						Path: "src/sub/file2.go",
						File: &StatusFile{Filename: "src/sub/file2.go", Status: "M "},
					},
				},
			},
		},
	}

	files := dirNode.CollectFiles()
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	// Verify file names
	names := make(map[string]bool)
	for _, f := range files {
		names[f.Filename] = true
	}
	if !names["src/file1.go"] {
		t.Fatal("expected src/file1.go in collected files")
	}
	if !names["src/sub/file2.go"] {
		t.Fatal("expected src/sub/file2.go in collected files")
	}
}

func TestShowDeleteFileNoSelection(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.data.filteredWts = []*models.WorktreeInfo{}
	m.state.data.selectedIndex = -1

	if cmd := m.showDeleteFile(); cmd != nil {
		t.Fatal("expected nil command when no worktree selected")
	}
	if m.state.ui.screenManager.IsActive() && m.state.ui.screenManager.Type() == appscreen.TypeConfirm {
		t.Fatal("expected no confirm screen when no selection")
	}
}

func TestShowDeleteFileNoFiles(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: "/tmp/feat", Branch: featureBranch},
	}
	m.state.data.selectedIndex = 0
	m.state.services.statusTree.TreeFlat = []*StatusTreeNode{}
	m.state.services.statusTree.Index = 0

	if cmd := m.showDeleteFile(); cmd != nil {
		t.Fatal("expected nil command when no files in tree")
	}
	if m.state.ui.screenManager.IsActive() && m.state.ui.screenManager.Type() == appscreen.TypeConfirm {
		t.Fatal("expected no confirm screen when no files")
	}
}

func TestShowDeleteFileSingleFile(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: "/tmp/feat", Branch: featureBranch},
	}
	m.state.data.selectedIndex = 0
	m.state.services.statusTree.TreeFlat = []*StatusTreeNode{
		{
			Path: "file.go",
			File: &StatusFile{Filename: "file.go", Status: " M"},
		},
	}
	m.state.services.statusTree.Index = 0

	if cmd := m.showDeleteFile(); cmd != nil {
		t.Fatal("expected nil command for confirm screen setup")
	}
	if !m.state.ui.screenManager.IsActive() || m.state.ui.screenManager.Type() != appscreen.TypeConfirm {
		t.Fatal("expected confirm screen to be set for file deletion")
	}
}

func TestShowDeleteFileDirectory(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: "/tmp/feat", Branch: featureBranch},
	}
	m.state.data.selectedIndex = 0
	m.state.services.statusTree.TreeFlat = []*StatusTreeNode{
		{
			Path: "src",
			File: nil, // Directory
			Children: []*StatusTreeNode{
				{
					Path: "src/file1.go",
					File: &StatusFile{Filename: "src/file1.go", Status: " M"},
				},
				{
					Path: "src/file2.go",
					File: &StatusFile{Filename: "src/file2.go", Status: "M "},
				},
			},
		},
	}
	m.state.services.statusTree.Index = 0

	if cmd := m.showDeleteFile(); cmd != nil {
		t.Fatal("expected nil command for confirm screen setup")
	}
	if !m.state.ui.screenManager.IsActive() || m.state.ui.screenManager.Type() != appscreen.TypeConfirm {
		t.Fatal("expected confirm screen to be set for directory deletion")
	}
}

func TestShowDeleteFileEmptyDirectory(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: "/tmp/feat", Branch: featureBranch},
	}
	m.state.data.selectedIndex = 0
	m.state.services.statusTree.TreeFlat = []*StatusTreeNode{
		{
			Path:     "src",
			File:     nil, // Directory
			Children: []*StatusTreeNode{},
		},
	}
	m.state.services.statusTree.Index = 0

	if cmd := m.showDeleteFile(); cmd != nil {
		t.Fatal("expected nil command for empty directory")
	}
	if m.state.ui.screenManager.IsActive() && m.state.ui.screenManager.Type() == appscreen.TypeConfirm {
		t.Fatal("expected no confirm screen for empty directory")
	}
}

// TestStatusFileEnterNoFilesDoesNothing tests Enter with no status files.

// TestStatusFileEnterNoFilesDoesNothing tests Enter with no status files.
func TestStatusFileEnterNoFilesDoesNothing(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 2
	m.state.data.statusFiles = nil

	_, cmd := m.handleEnterKey()
	if cmd != nil {
		t.Fatal("expected no command when no status files")
	}
}

// TestBuildStatusContentParsesFiles tests that buildStatusContent parses git status correctly.

func TestCICheckEnterOpensURL(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 1
	m.state.data.selectedIndex = 0
	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: testWorktreePath, Branch: "feat"},
	}

	// Add CI checks to cache
	checkURL := "https://github.com/owner/repo/actions/runs/123"
	checks := []*models.CICheck{
		{Name: "build", Conclusion: "success", Link: checkURL},
	}
	m.cache.ciCache.Set("feat", checks)
	m.ciCheckIndex = 0

	// Capture command
	var capturedCmd *exec.Cmd
	m.commandRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedCmd = exec.CommandContext(ctx, name, args...) //#nosec G204,G702 -- test mock with controlled args
		return capturedCmd
	}
	m.startCommand = func(cmd *exec.Cmd) error {
		return nil
	}

	// Press Enter
	_, cmd := m.handleEnterKey()
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}

	// Execute command to capture URL
	_ = cmd()

	// Verify the command was to open URL (platform-specific)
	if capturedCmd == nil || len(capturedCmd.Args) == 0 {
		t.Fatal("expected command to be executed")
	}
	// Check for common browser commands
	validCommands := []string{"open", "xdg-open", "rundll32"}
	isValid := false
	for _, validCmd := range validCommands {
		if capturedCmd.Args[0] == validCmd {
			isValid = true
			break
		}
	}
	if !isValid {
		t.Fatalf("expected browser command, got %v", capturedCmd.Args)
	}
}

func TestCICheckCtrlVShowsLogs(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")
	m.state.view.FocusedPane = 1
	m.state.data.selectedIndex = 0
	m.state.data.filteredWts = []*models.WorktreeInfo{
		{Path: testWorktreePath, Branch: "feat"},
	}

	// Add CI checks to cache
	checkURL := "https://github.com/owner/repo/actions/runs/123"
	checks := []*models.CICheck{
		{Name: "build", Conclusion: "success", Link: checkURL},
	}
	m.cache.ciCache.Set("feat", checks)
	m.ciCheckIndex = 0

	// Capture command
	var capturedCmd *exec.Cmd
	m.commandRunner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedCmd = exec.CommandContext(ctx, name, args...) //#nosec G204,G702 -- test mock with controlled args
		return capturedCmd
	}
	m.startCommand = func(cmd *exec.Cmd) error {
		return nil
	}

	// Press Ctrl+v
	_, cmd := m.handleBuiltInKey(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("expected command to be returned")
	}

	// Execute command
	_ = cmd()

	// Verify the command contains gh run view
	if capturedCmd == nil || len(capturedCmd.Args) < 2 {
		t.Fatal("expected command with args")
	}
	if capturedCmd.Args[0] != "bash" {
		t.Fatalf("expected bash command, got %s", capturedCmd.Args[0])
	}
	if !strings.Contains(capturedCmd.Args[2], "gh run view") {
		t.Fatalf("expected gh run view in command, got %s", capturedCmd.Args[2])
	}
}

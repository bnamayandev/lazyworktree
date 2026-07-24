package app

import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/bubbles/v2/table"
	"github.com/chmouel/lazyworktree/internal/config"
	"github.com/chmouel/lazyworktree/internal/models"
)

func TestDetermineCurrentWorktreePrefersSelection(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")

	main := &models.WorktreeInfo{Path: "/tmp/main", Branch: "main", IsMain: true}
	feature := &models.WorktreeInfo{Path: "/tmp/feature", Branch: "feature"}
	m.state.data.worktrees = []*models.WorktreeInfo{main, feature}
	m.state.data.filteredWts = m.state.data.worktrees

	rows := []table.Row{
		{"main"},
		{"feature"},
	}
	m.state.ui.worktreeTable.SetRows(rows)
	m.state.ui.worktreeTable.SetCursor(1)

	got := m.determineCurrentWorktree()
	if got != feature {
		t.Fatalf("expected selected worktree, got %v", got)
	}
}

func TestDetermineCurrentWorktreeAvoidsSiblingPrefixMatch(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")

	parent := normalizePathForTest(t, t.TempDir())
	feature := filepath.Join(parent, "feature")
	featureTwo := filepath.Join(parent, "feature-2")
	requireDir(t, feature)
	requireDir(t, featureTwo)

	main := &models.WorktreeInfo{Path: filepath.Join(parent, "main"), Branch: "main", IsMain: true}
	featureWt := &models.WorktreeInfo{Path: feature, Branch: "feature"}
	featureTwoWt := &models.WorktreeInfo{Path: featureTwo, Branch: "feature-2"}
	m.state.data.worktrees = []*models.WorktreeInfo{main, featureWt, featureTwoWt}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(featureTwo); err != nil {
		t.Fatalf("failed to change working directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	got := m.determineCurrentWorktree()
	if got != featureTwoWt {
		t.Fatalf("expected feature-2 worktree, got %v", got)
	}
}

func TestSelectInitialWorktreeFromCwd(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")

	parent := normalizePathForTest(t, t.TempDir())
	mainPath := filepath.Join(parent, "main")
	featurePath := filepath.Join(parent, "feature")
	requireDir(t, mainPath)
	requireDir(t, featurePath)

	main := &models.WorktreeInfo{Path: mainPath, Branch: "main", IsMain: true}
	feature := &models.WorktreeInfo{Path: featurePath, Branch: "feature"}
	// filteredWts is sorted with main first, mimicking a fresh launch where
	// the feature worktree has no access history to sort it to the top.
	m.state.data.worktrees = []*models.WorktreeInfo{main, feature}
	m.state.data.filteredWts = m.state.data.worktrees
	m.state.ui.worktreeTable.SetRows([]table.Row{{"main"}, {"feature"}})
	m.state.ui.worktreeTable.SetCursor(0)
	m.state.data.selectedIndex = 0

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(featurePath); err != nil {
		t.Fatalf("failed to change working directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	m.selectInitialWorktreeFromCwd()

	if m.state.data.selectedIndex != 1 {
		t.Fatalf("expected selectedIndex 1 (feature), got %d", m.state.data.selectedIndex)
	}
	if m.state.ui.worktreeTable.Cursor() != 1 {
		t.Fatalf("expected table cursor 1 (feature), got %d", m.state.ui.worktreeTable.Cursor())
	}
}

func TestSelectInitialWorktreeFromCwdNoMatchKeepsSelection(t *testing.T) {
	cfg := &config.AppConfig{
		WorktreeDir: t.TempDir(),
	}
	m := NewModel(cfg, "")

	main := &models.WorktreeInfo{Path: "/does/not/exist/main", Branch: "main", IsMain: true}
	feature := &models.WorktreeInfo{Path: "/does/not/exist/feature", Branch: "feature"}
	m.state.data.worktrees = []*models.WorktreeInfo{main, feature}
	m.state.data.filteredWts = m.state.data.worktrees
	m.state.ui.worktreeTable.SetRows([]table.Row{{"main"}, {"feature"}})
	m.state.ui.worktreeTable.SetCursor(0)
	m.state.data.selectedIndex = 0

	m.selectInitialWorktreeFromCwd()

	if m.state.data.selectedIndex != 0 {
		t.Fatalf("expected selectedIndex to remain 0 when cwd matches no worktree, got %d", m.state.data.selectedIndex)
	}
}

func requireDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil { //nolint:gosec
		t.Fatalf("failed to create directory %q: %v", path, err)
	}
}

func normalizePathForTest(t *testing.T, path string) string {
	t.Helper()

	normalized, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return normalized
}

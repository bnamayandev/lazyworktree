package app

import (
	"os"
	"strings"

	"github.com/chmouel/lazyworktree/internal/models"
	"github.com/chmouel/lazyworktree/internal/utils"
)

// determineCurrentWorktree finds the worktree that matches the current working directory.
func (m *Model) determineCurrentWorktree() *models.WorktreeInfo {
	if wt := m.selectedWorktree(); wt != nil {
		return wt
	}

	if cwd, err := os.Getwd(); err == nil {
		for _, wt := range m.state.data.worktrees {
			if utils.PathContains(wt.Path, cwd) {
				return wt
			}
		}
	}

	for _, wt := range m.state.data.worktrees {
		if wt.IsMain {
			return wt
		}
	}

	return nil
}

// selectInitialWorktreeFromCwd highlights the worktree matching the process's
// launch directory. Called once on startup so the window title and info pane
// reflect where the user actually launched from, instead of leaving the
// cursor on row 0 of whatever the default sort produced.
func (m *Model) selectInitialWorktreeFromCwd() {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	for i, wt := range m.state.data.filteredWts {
		if utils.PathContains(wt.Path, cwd) {
			m.state.ui.worktreeTable.SetCursor(i)
			m.state.data.selectedIndex = i
			m.updateWorktreeArrows()
			return
		}
	}
}

// selectedWorktree returns the currently selected worktree from the filtered list.
func (m *Model) selectedWorktree() *models.WorktreeInfo {
	indices := []int{m.state.ui.worktreeTable.Cursor(), m.state.data.selectedIndex}
	for _, idx := range indices {
		if wt := m.worktreeAtIndex(idx); wt != nil {
			return wt
		}
	}
	return nil
}

// worktreeAtIndex returns the worktree at the given index in the filtered list.
func (m *Model) worktreeAtIndex(idx int) *models.WorktreeInfo {
	if idx < 0 || idx >= len(m.state.data.filteredWts) {
		return nil
	}
	return m.state.data.filteredWts[idx]
}

// selectWorktreeByPath selects the given worktree path in the main table.
// Returns true when the selection was applied.
func (m *Model) selectWorktreeByPath(path string) bool {
	if path == "" {
		return false
	}

	// If the worktree filter hides the target, clear it first so selection can succeed.
	if strings.TrimSpace(m.state.services.filter.FilterQuery) != "" {
		m.setFilterQuery(filterTargetWorktrees, "")
		m.state.ui.filterInput.SetValue("")
	}

	m.updateTable()
	for i, wt := range m.state.data.filteredWts {
		if wt.Path == path {
			m.state.ui.worktreeTable.SetCursor(i)
			m.state.data.selectedIndex = i
			m.updateWorktreeArrows()
			return true
		}
	}

	return false
}

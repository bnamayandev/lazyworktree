package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	appscreen "github.com/chmouel/lazyworktree/internal/app/screen"
	"github.com/chmouel/lazyworktree/internal/app/services"
	"github.com/chmouel/lazyworktree/internal/config"
	"github.com/chmouel/lazyworktree/internal/models"
	"github.com/chmouel/lazyworktree/internal/multiplexer"
)

const (
	zellijSessionLabel = "zellij session"
	tmuxSessionLabel   = "tmux session"
)

type (
	zellijSessionReadyMsg struct {
		sessionName  string
		attach       bool
		insideZellij bool
	}
	zellijPaneCreatedMsg struct {
		sessionName string
		direction   string
	}
	tmuxSessionReadyMsg struct {
		sessionName string
		attach      bool
		insideTmux  bool
	}
)

func buildZellijInfoMessage(sessionName string) string {
	quoted := multiplexer.ShellQuote(sessionName)
	return fmt.Sprintf("zellij session ready.\n\nAttach with:\n\n  zellij attach %s", quoted)
}

func (m *Model) attachZellijSessionCmd(sessionName string) tea.Cmd {
	// #nosec G204 -- zellij session name comes from user configuration.
	c := m.commandRunner(m.ctx, "zellij", multiplexer.OnExistsAttach, sessionName)
	return m.execProcess(c, func(err error) tea.Msg {
		if err != nil {
			return errMsg{err: err}
		}
		return refreshCompleteMsg{}
	})
}

// getZellijActiveSessions queries zellij for all sessions starting with the configured session prefix
// Returns session names with the prefix stripped, or empty slice if zellij is unavailable.
func (m *Model) getZellijActiveSessions() []string {
	// Check if zellij is available
	if _, err := exec.LookPath("zellij"); err != nil {
		return nil
	}

	// Query zellij for session list (not --short, because --short strips the EXITED marker)
	// #nosec G204 -- static command with format string
	cmd := m.commandRunner(m.ctx, "zellij", "list-sessions", "--no-formatting")
	output, err := cmd.Output()
	if err != nil {
		// zellij not running or no sessions
		return nil
	}

	// Parse output and filter for worktree session prefix, excluding exited sessions
	// Full output format: "session_name [Created ...]" or "session_name [Created ...] (EXITED ...)"
	var sessions []string
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "EXITED") {
			continue
		}
		// Extract session name before the first " ["
		name := line
		if idx := strings.Index(line, " ["); idx >= 0 {
			name = line[:idx]
		}
		name = strings.TrimSpace(name)
		if name != "" && strings.HasPrefix(name, m.config.SessionPrefix) {
			sessionName := strings.TrimPrefix(name, m.config.SessionPrefix)
			if sessionName != "" {
				sessions = append(sessions, sessionName)
			}
		}
	}

	// Sort alphabetically for consistent display
	sort.Strings(sessions)
	return sessions
}

// getAllZellijSessions queries zellij for all active sessions (not filtered by prefix).
// Returns sorted session names with EXITED sessions excluded.
// Uses full output (not --short) because --short strips the EXITED marker.
func (m *Model) getAllZellijSessions() []string {
	// #nosec G204 -- static command arguments
	cmd := m.commandRunner(m.ctx, "zellij", "list-sessions", "--no-formatting")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var sessions []string
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "EXITED") {
			continue
		}
		// Full output format: "session_name [Created ...]"
		// Extract session name before the first " ["
		name := line
		if idx := strings.Index(line, " ["); idx >= 0 {
			name = line[:idx]
		}
		name = strings.TrimSpace(name)
		if name != "" {
			sessions = append(sessions, name)
		}
	}

	sort.Strings(sessions)
	return sessions
}

func sanitizeZellijSessionName(name string) string {
	return multiplexer.SanitizeZellijSessionName(name)
}

func buildTmuxInfoMessage(sessionName string, insideTmux bool) string {
	quoted := multiplexer.ShellQuote(sessionName)
	if insideTmux {
		return fmt.Sprintf("tmux session ready.\n\nSwitch with:\n\n  tmux switch-client -t %s", quoted)
	}
	return fmt.Sprintf("tmux session ready.\n\nAttach with:\n\n  tmux attach-session -t %s", quoted)
}

func (m *Model) attachTmuxSessionCmd(sessionName string, insideTmux bool) tea.Cmd {
	args := []string{"attach-session", "-t", sessionName}
	if insideTmux {
		args = []string{"switch-client", "-t", sessionName}
	}
	// #nosec G204 -- tmux session name comes from user configuration.
	c := m.commandRunner(m.ctx, "tmux", args...)
	return m.execProcess(c, func(err error) tea.Msg {
		if err != nil {
			return errMsg{err: err}
		}
		return refreshCompleteMsg{}
	})
}

func readTmuxSessionFile(path, fallback string) string {
	return multiplexer.ReadSessionFile(path, fallback)
}

func buildTmuxScript(sessionName string, tmuxCfg *config.TmuxCommand, windows []multiplexer.ResolvedWindow, env map[string]string) string {
	return multiplexer.BuildTmuxScript(sessionName, tmuxCfg, windows, env)
}

func buildZellijScript(sessionName string, zellijCfg *config.TmuxCommand, layoutPaths []string) string {
	return multiplexer.BuildZellijScript(sessionName, zellijCfg, layoutPaths)
}

func writeZellijLayouts(windows []multiplexer.ResolvedWindow) ([]string, error) {
	return multiplexer.WriteZellijLayouts(windows)
}

func sanitizeTmuxSessionName(name string) string {
	return multiplexer.SanitizeTmuxSessionName(name)
}

// getTmuxActiveSessions queries tmux for all sessions starting with the configured session prefix
// Returns session names with the prefix stripped, or empty slice if tmux is unavailable.
func (m *Model) getTmuxActiveSessions() []string {
	// Check if tmux is available
	if _, err := exec.LookPath("tmux"); err != nil {
		return nil
	}

	// Query tmux for session list
	// #nosec G204 -- static command with format string
	cmd := m.commandRunner(m.ctx, "tmux", "list-sessions", "-F", "#{session_name}")
	output, err := cmd.Output()
	if err != nil {
		// tmux not running or no sessions
		return nil
	}

	// Parse output and filter for worktree session prefix
	var sessions []string
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, m.config.SessionPrefix) {
			// Strip worktree prefix
			sessionName := strings.TrimPrefix(line, m.config.SessionPrefix)
			if sessionName != "" {
				sessions = append(sessions, sessionName)
			}
		}
	}

	// Sort alphabetically for consistent display
	sort.Strings(sessions)
	return sessions
}

func resolveTmuxWindows(windows []config.TmuxWindow, env map[string]string, defaultCwd string) ([]multiplexer.ResolvedWindow, bool) {
	return multiplexer.ResolveTmuxWindows(windows, env, defaultCwd)
}

func buildTmuxWindowCommand(command string, env map[string]string) string {
	return multiplexer.BuildTmuxWindowCommand(command, env)
}

// openAgentForSelectedWorktree opens an agent session for the currently
// selected worktree, if any.
func (m *Model) openAgentForSelectedWorktree() tea.Cmd {
	if m.state.data.selectedIndex < 0 || m.state.data.selectedIndex >= len(m.state.data.filteredWts) {
		return nil
	}
	return m.openAgentSession(m.state.data.filteredWts[m.state.data.selectedIndex])
}

// agentCommand returns the configured agent command, defaulting to "claude".
func (m *Model) agentCommand() string {
	agentCmd := strings.TrimSpace(m.config.AgentCommand)
	if agentCmd == "" {
		agentCmd = "claude"
	}
	return agentCmd
}

// worktreeHasAgentSession reports whether Claude has a conversation rooted at
// the worktree itself, meaning there is something for "--continue" to resume.
//
// The match is deliberately stricter than SessionsForWorktree: that helper also
// reports sessions from subdirectories and from other agents, whereas
// "--continue" only finds conversations recorded against the exact directory we
// launch in and errors out otherwise.
func (m *Model) worktreeHasAgentSession(wt *models.WorktreeInfo) bool {
	if wt == nil || !m.agentSessionsEnabled() {
		return false
	}
	service := m.state.services.agentSessions
	if service == nil {
		return false
	}
	return hasResumableClaudeSession(service.SessionsForWorktree(wt.Path), wt.Path)
}

// hasResumableClaudeSession reports whether any session is a Claude
// conversation recorded against the worktree directory itself.
func hasResumableClaudeSession(sessions []*models.AgentSession, worktreePath string) bool {
	root := filepath.Clean(strings.TrimSpace(worktreePath))
	if root == "" || root == "." {
		return false
	}
	for _, session := range sessions {
		if session == nil || session.Agent != models.AgentKindClaude {
			continue
		}
		if filepath.Clean(strings.TrimSpace(session.CWD)) == root {
			return true
		}
	}
	return false
}

// resolveAgentCommand continues an existing conversation rather than starting a
// fresh one when the worktree already has a transcript. Only Claude is
// special-cased, since "--continue" is its flag, and the command is left alone
// when it already asks to resume or when there is nothing to continue, because
// "--continue" errors out with no prior conversation.
func resolveAgentCommand(base string, hasSession bool) string {
	fields := strings.Fields(base)
	if len(fields) == 0 || filepath.Base(fields[0]) != "claude" {
		return base
	}
	for _, field := range fields[1:] {
		switch field {
		case "-c", "--continue", "-r", "--resume":
			return base
		}
	}
	if !hasSession {
		return base
	}
	return base + " --continue"
}

// agentCommandForWorktree resolves the command to launch for a worktree.
func (m *Model) agentCommandForWorktree(wt *models.WorktreeInfo) string {
	return resolveAgentCommand(m.agentCommand(), m.worktreeHasAgentSession(wt))
}

// openAgentSession launches the configured agent command (default "claude") for
// the worktree. Inside zellij it goes into a floating pane, which leaves the
// interface responsive whilst the agent works and lets the pane be hidden away
// without stopping it. Elsewhere the agent runs inline, taking over the
// terminal until it exits.
func (m *Model) openAgentSession(wt *models.WorktreeInfo) tea.Cmd {
	if wt == nil {
		return nil
	}
	if os.Getenv("ZELLIJ") != "" || os.Getenv("ZELLIJ_SESSION_NAME") != "" {
		return m.openAgentFloatingZellij(wt)
	}
	return m.openAgentInline(wt)
}

// agentShell resolves the shell the agent is launched under, so that the login
// profile supplies the PATH the agent expects.
func agentShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return "bash"
}

// openAgentInline runs the agent in LazyWorktree's own terminal, suspending the
// interface until the agent exits. This is the path outside zellij, where there
// is no pane to put the agent in: nothing continues in the background and only
// one agent can be open at a time. The conversation itself still survives,
// because the transcript lives on disk and agentCommandForWorktree resumes it
// on the next launch.
func (m *Model) openAgentInline(wt *models.WorktreeInfo) tea.Cmd {
	m.debugf("agent session: outside zellij, running %q inline in %s", m.agentCommand(), wt.Path)
	env := m.buildCommandEnvForWorktree(wt)
	// #nosec G204 -- the agent command comes from user configuration.
	c := m.commandRunner(m.ctx, agentShell(), "-lc", m.agentCommandForWorktree(wt))
	c.Dir = wt.Path
	c.Env = services.AppendCommandEnv(os.Environ(), env)
	return m.execProcess(c, func(err error) tea.Msg {
		if err != nil {
			return errMsg{err: err}
		}
		return refreshCompleteMsg{}
	})
}

// buildAgentPaneCommand returns the argv the floating zellij pane runs: the
// agent under a login shell, nothing more. The pane is the agent's home for as
// long as it lives, so hiding the floating layer leaves it working whilst
// closing the pane, or quitting zellij, stops it.
func (m *Model) buildAgentPaneCommand(wt *models.WorktreeInfo) []string {
	return []string{agentShell(), "-lc", m.agentCommandForWorktree(wt)}
}

// agentZellijPaneName titles a worktree's floating agent pane. Zellij keeps an
// explicitly set name in place of the terminal title the running command
// reports, so the name survives as a stable handle for finding the pane again
// on a later Enter.
func agentZellijPaneName(wt *models.WorktreeInfo) string {
	return "agent:" + filepath.Base(wt.Path)
}

// zellijPaneListContains reports whether "zellij action list-panes" output holds
// a pane with the given title. The columns are "PANE_ID  TYPE  TITLE" separated
// by two spaces, and the title is taken as the whole remainder of the line so
// that titles containing spaces still match. The leading header row is skipped,
// lest a pane titled "TITLE" match it.
func zellijPaneListContains(output, name string) bool {
	for line := range strings.SplitSeq(output, "\n") {
		parts := strings.SplitN(strings.TrimRight(line, "\r"), "  ", 3)
		if len(parts) < 3 || parts[0] == "PANE_ID" {
			continue
		}
		if strings.TrimSpace(parts[2]) == name {
			return true
		}
	}
	return false
}

// zellijAgentPaneExists reports whether the worktree already has an agent pane
// in the current zellij session, so that a second Enter reuses it instead of
// stacking up duplicate panes onto the same agent.
func (m *Model) zellijAgentPaneExists(name string) bool {
	// #nosec G204 -- static command arguments.
	cmd := m.commandRunner(m.ctx, "zellij", "action", "list-panes")
	output, err := cmd.Output()
	if err != nil {
		m.debugf("agent pane: list-panes failed, assuming no existing pane: %v", err)
		return false
	}
	return zellijPaneListContains(string(output), name)
}

// revealZellijFloatingPanes brings the floating layer back into view, which is
// as close to raising the agent pane as zellij allows: it exposes no "focus pane
// by id" action, so a pane that already exists can only be revealed, not
// selected. Exit code 2 reports the panes were already visible, which is not a
// failure.
func (m *Model) revealZellijFloatingPanes() tea.Cmd {
	return func() tea.Msg {
		// #nosec G204 -- static command arguments.
		c := m.commandRunner(m.ctx, "zellij", "action", "show-floating-panes")
		if err := c.Run(); err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
				return errMsg{err: fmt.Errorf("failed to reveal agent pane: %w", err)}
			}
		}
		return nil
	}
}

// openAgentFloatingZellij opens the agent in a large (90%) centred floating
// zellij pane rooted at the worktree. Creating the pane returns at once, so the
// interface stays live whilst the agent works; hide the floating layer to send
// the agent to the background and reveal it to come back. A worktree that
// already has a pane reuses it, which matters rather more without a session
// behind the pane: a second pane would be a second agent, not another view of
// the first.
func (m *Model) openAgentFloatingZellij(wt *models.WorktreeInfo) tea.Cmd {
	if _, err := exec.LookPath("zellij"); err != nil {
		m.showInfo("zellij is not installed. Install it from https://zellij.dev to open agent sessions.", nil)
		return nil
	}
	name := agentZellijPaneName(wt)
	if m.zellijAgentPaneExists(name) {
		m.debugf("agent pane: reusing existing zellij pane %q", name)
		return m.revealZellijFloatingPanes()
	}
	env := m.buildCommandEnvForWorktree(wt)
	args := []string{
		"action", "new-pane", "--floating",
		"--width", "90%", "--height", "90%", "--x", "5%", "--y", "5%",
		"--close-on-exit", "--name", name, "--cwd", wt.Path,
		"--",
	}
	args = append(args, m.buildAgentPaneCommand(wt)...)
	// #nosec G204 -- cwd is a managed worktree path; the agent command comes from user configuration.
	c := m.commandRunner(m.ctx, "zellij", args...)
	c.Dir = wt.Path
	c.Env = services.AppendCommandEnv(os.Environ(), env)
	return func() tea.Msg {
		if err := c.Run(); err != nil {
			return errMsg{err: fmt.Errorf("failed to open agent pane: %w", err)}
		}
		return nil
	}
}

func (m *Model) openTmuxSession(customCmd *config.CustomCommand, wt *models.WorktreeInfo) tea.Cmd {
	if customCmd == nil || customCmd.Tmux == nil {
		return nil
	}
	tmuxCfg := customCmd.Tmux
	env := m.buildCommandEnvForWorktree(wt)
	insideTmux := os.Getenv("TMUX") != ""
	sessionName := expandWithEnv(tmuxCfg.SessionName, env)
	if strings.TrimSpace(sessionName) == "" {
		sessionName = fmt.Sprintf("%s%s", m.config.SessionPrefix, filepath.Base(wt.Path))
	}
	sessionName = sanitizeTmuxSessionName(sessionName)

	resolved, ok := resolveTmuxWindows(tmuxCfg.Windows, env, wt.Path)
	if !ok {
		return func() tea.Msg {
			return errMsg{err: fmt.Errorf("failed to resolve tmux windows")}
		}
	}

	// Wrap window commands in container if configured
	if customCmd.Container != nil {
		var err error
		resolved, err = multiplexer.WrapWindowCommandsForContainer(resolved, customCmd.Container, env)
		if err != nil {
			return func() tea.Msg { return errMsg{err: err} }
		}
	}

	// When new_tab is set, run the entire tmux script (create + attach) in a
	// new terminal tab so the TUI is never suspended.
	if customCmd.NewTab {
		m.debugf("Opening tmux session %q in new terminal tab", sessionName)
		script := buildTmuxScript(sessionName, tmuxCfg, resolved, env)
		// New tabs can inherit TMUX vars from the originating pane. Clear them
		// so attach mode targets the new terminal tab client instead of running
		// switch-client logic that may affect the current tab.
		script = "unset TMUX TMUX_PANE\n" + script
		c := &config.CustomCommand{
			Command:     script,
			Description: filepath.Base(wt.Path),
		}
		return m.openTerminalTab(c, wt)
	}

	sessionFile, err := os.CreateTemp("", "lazyworktree-tmux-")
	if err != nil {
		return func() tea.Msg {
			return errMsg{err: err}
		}
	}
	sessionPath := sessionFile.Name()
	if closeErr := sessionFile.Close(); closeErr != nil {
		return func() tea.Msg {
			return errMsg{err: closeErr}
		}
	}

	scriptCfg := *tmuxCfg
	scriptCfg.Attach = false
	env["LW_TMUX_SESSION_FILE"] = sessionPath
	script := buildTmuxScript(sessionName, &scriptCfg, resolved, env)
	// #nosec G204 -- command is built from user-configured tmux session settings.
	c := m.commandRunner(m.ctx, "bash", "-lc", script)
	c.Dir = wt.Path
	c.Env = services.AppendCommandEnv(os.Environ(), env)

	return m.execProcess(c, func(err error) tea.Msg {
		defer func() {
			_ = os.Remove(sessionPath)
		}()
		if err != nil {
			return errMsg{err: err}
		}
		finalSession := readTmuxSessionFile(sessionPath, sessionName)
		return tmuxSessionReadyMsg{
			sessionName: finalSession,
			attach:      tmuxCfg.Attach,
			insideTmux:  insideTmux,
		}
	})
}

// showZellijPaneSelector is the entry point for the new-pane flow when inside zellij.
// It fetches all active sessions and either skips to the direction picker (single session)
// or shows a session picker (multiple sessions).
func (m *Model) showZellijPaneSelector(wt *models.WorktreeInfo) tea.Cmd {
	sessions := m.getAllZellijSessions()
	currentSession := os.Getenv("ZELLIJ_SESSION_NAME")

	switch len(sessions) {
	case 0:
		// Edge case: inside zellij but no sessions found; use current session env
		if currentSession != "" {
			m.showZellijDirectionPicker(currentSession, wt)
			return nil
		}
		m.showInfo("No active zellij sessions found.", nil)
		return nil
	case 1:
		m.showZellijDirectionPicker(sessions[0], wt)
		return nil
	default:
		items := make([]appscreen.SelectionItem, len(sessions))
		for i, s := range sessions {
			desc := ""
			if s == currentSession {
				desc = "(current)"
			}
			items[i] = appscreen.SelectionItem{
				ID:          s,
				Label:       s,
				Description: desc,
			}
		}
		scr := appscreen.NewListSelectionScreen(
			items,
			"Select zellij session",
			"Filter sessions...",
			"No sessions found.",
			m.state.view.WindowWidth, m.state.view.WindowHeight,
			currentSession,
			m.theme,
		)
		scr.OnSelect = func(item appscreen.SelectionItem) tea.Cmd {
			m.showZellijDirectionPicker(item.ID, wt)
			return nil
		}
		m.state.ui.screenManager.Push(scr)
		return nil
	}
}

// showZellijDirectionPicker shows a picker for pane split direction (right or down).
func (m *Model) showZellijDirectionPicker(sessionName string, wt *models.WorktreeInfo) {
	items := []appscreen.SelectionItem{
		{ID: "right", Label: "Right", Description: "Split pane to the right"},
		{ID: "down", Label: "Down", Description: "Split pane downward"},
	}
	scr := appscreen.NewListSelectionScreen(
		items,
		"Select pane direction",
		"",
		"",
		m.state.view.WindowWidth, m.state.view.WindowHeight,
		"right",
		m.theme,
	)
	scr.OnSelect = func(item appscreen.SelectionItem) tea.Cmd {
		return m.zellijNewPaneCmd(sessionName, item.ID, wt.Path)
	}
	m.state.ui.screenManager.Push(scr)
}

// zellijNewPaneCmd runs `zellij action new-pane` to add a pane in the given direction.
func (m *Model) zellijNewPaneCmd(sessionName, direction, cwd string) tea.Cmd {
	return func() tea.Msg {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "bash"
		}
		// #nosec G204 -- session name, direction come from user selection; shell defaults to bash if $SHELL is unset
		c := m.commandRunner(m.ctx, "zellij", "action", "new-pane", "--direction", direction, "--cwd", cwd, "--", shell)
		c.Env = append(os.Environ(), "ZELLIJ_SESSION_NAME="+sessionName)
		if err := c.Run(); err != nil {
			return errMsg{err: fmt.Errorf("failed to create zellij pane: %w", err)}
		}
		return zellijPaneCreatedMsg{sessionName: sessionName, direction: direction}
	}
}

// zellijCreateExternalPaneCmd creates a pane in an existing zellij session from outside zellij.
// Sets ZELLIJ_SESSION_NAME so zellij action can target the session.
// The TUI remains active; only the pane is created in the target session.
func (m *Model) zellijCreateExternalPaneCmd(sessionName, direction, cwd string) tea.Cmd {
	return func() tea.Msg {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "bash"
		}
		// #nosec G204 -- session name, direction, cwd, shell come from user env/selection
		c := m.commandRunner(m.ctx, "zellij", "action", "new-pane", "--direction", direction, "--cwd", cwd, "--", shell)
		c.Env = append(os.Environ(), "ZELLIJ_SESSION_NAME="+sessionName)
		if err := c.Run(); err != nil {
			return errMsg{err: fmt.Errorf("failed to create zellij pane: %w", err)}
		}
		return zellijPaneCreatedMsg{sessionName: sessionName, direction: direction}
	}
}

// zellijAttachNewSessionCmd attaches to a new zellij session with the worktree cwd.
// Uses --create so the initial pane opens directly in the worktree directory (single pane).
func (m *Model) zellijAttachNewSessionCmd(sessionName, cwd string) tea.Cmd {
	// #nosec G204 -- session name comes from user configuration
	c := m.commandRunner(m.ctx, "zellij", "attach", "--create", sessionName)
	c.Dir = cwd
	return m.execProcess(c, func(err error) tea.Msg {
		if err != nil {
			return errMsg{err: err}
		}
		return refreshCompleteMsg{}
	})
}

// showZellijDirectionPickerExternal shows a direction picker for outside-zellij pane creation.
// After selection, creates the pane in the target session without attaching (TUI stays active).
func (m *Model) showZellijDirectionPickerExternal(sessionName string, wt *models.WorktreeInfo) {
	items := []appscreen.SelectionItem{
		{ID: "right", Label: "Right", Description: "Split pane to the right"},
		{ID: "down", Label: "Down", Description: "Split pane downward"},
	}
	scr := appscreen.NewListSelectionScreen(
		items,
		"Select pane direction",
		"",
		"",
		m.state.view.WindowWidth, m.state.view.WindowHeight,
		"right",
		m.theme,
	)
	scr.OnSelect = func(item appscreen.SelectionItem) tea.Cmd {
		return m.zellijCreateExternalPaneCmd(sessionName, item.ID, wt.Path)
	}
	m.state.ui.screenManager.Push(scr)
}

// showZellijSessionPickerWithAttach shows a session picker for outside-zellij use.
// On selection, shows a direction picker to create a pane with the worktree cwd (TUI stays active).
func (m *Model) showZellijSessionPickerWithAttach(sessions []string, wt *models.WorktreeInfo) {
	items := make([]appscreen.SelectionItem, len(sessions))
	for i, s := range sessions {
		items[i] = appscreen.SelectionItem{
			ID:    s,
			Label: s,
		}
	}
	scr := appscreen.NewListSelectionScreen(
		items,
		"Select zellij session",
		"Filter sessions...",
		"No sessions found.",
		m.state.view.WindowWidth, m.state.view.WindowHeight,
		"",
		m.theme,
	)
	scr.OnSelect = func(item appscreen.SelectionItem) tea.Cmd {
		m.showZellijDirectionPickerExternal(item.ID, wt)
		return nil
	}
	m.state.ui.screenManager.Push(scr)
}

func (m *Model) openZellijSession(customCmd *config.CustomCommand, wt *models.WorktreeInfo) tea.Cmd {
	if customCmd == nil || customCmd.Zellij == nil {
		return nil
	}

	// Check that zellij is installed
	if _, err := exec.LookPath("zellij"); err != nil {
		m.showInfo("zellij is not installed. Install it from https://zellij.dev to use this feature.", nil)
		return nil
	}

	// When inside zellij, use the new-pane flow
	insideZellij := os.Getenv("ZELLIJ") != "" || os.Getenv("ZELLIJ_SESSION_NAME") != ""
	if insideZellij {
		return m.showZellijPaneSelector(wt)
	}

	// When outside zellij, create/reuse a session and add a pane, then attach
	zellijCfg := customCmd.Zellij
	env := m.buildCommandEnvForWorktree(wt)
	sessionName := strings.TrimSpace(expandWithEnv(zellijCfg.SessionName, env))
	if sessionName == "" {
		sessionName = fmt.Sprintf("%s%s", m.config.SessionPrefix, filepath.Base(wt.Path))
	}
	sessionName = sanitizeZellijSessionName(sessionName)

	// When new_tab is set, run in a new terminal tab
	if customCmd.NewTab {
		resolved, ok := resolveTmuxWindows(zellijCfg.Windows, env, wt.Path)
		if !ok {
			return func() tea.Msg {
				return errMsg{err: fmt.Errorf("failed to resolve zellij windows")}
			}
		}
		// Wrap window commands in container if configured
		if customCmd.Container != nil {
			var wrapErr error
			resolved, wrapErr = multiplexer.WrapWindowCommandsForContainer(resolved, customCmd.Container, env)
			if wrapErr != nil {
				return func() tea.Msg { return errMsg{err: wrapErr} }
			}
		}
		layoutPaths, err := writeZellijLayouts(resolved)
		if err != nil {
			return func() tea.Msg {
				return errMsg{err: err}
			}
		}
		script := buildZellijScript(sessionName, zellijCfg, layoutPaths)
		script += fmt.Sprintf("zellij attach %s\n", multiplexer.ShellQuote(sessionName))
		for _, lp := range layoutPaths {
			script += fmt.Sprintf("rm -f %s\n", multiplexer.ShellQuote(lp))
		}
		c := &config.CustomCommand{
			Command:     script,
			Description: filepath.Base(wt.Path),
		}
		return m.openTerminalTab(c, wt)
	}

	// Check for existing sessions
	sessions := m.getAllZellijSessions()
	if len(sessions) == 0 {
		// No sessions: create and attach directly (single pane with worktree cwd)
		return m.zellijAttachNewSessionCmd(sessionName, wt.Path)
	}

	// Sessions exist: let user pick a session, then direction, then create pane and attach
	m.showZellijSessionPickerWithAttach(sessions, wt)
	return nil
}

package app

import (
	"image/color"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/chmouel/lazyworktree/internal/app/services"
	"github.com/chmouel/lazyworktree/internal/models"
)

// agentSpinnerInterval paces the busy state indicator. The tick loop only
// runs while a visible session is working, so this costs nothing at idle.
const agentSpinnerInterval = 120 * time.Millisecond

type agentRenderStyles struct {
	prefix    lipgloss.Style
	title     lipgloss.Style
	muted     lipgloss.Style
	separator lipgloss.Style
}

func (m *Model) agentRenderStyles() agentRenderStyles {
	return agentRenderStyles{
		prefix:    lipgloss.NewStyle().Foreground(m.theme.BorderDim),
		title:     lipgloss.NewStyle().Foreground(m.theme.TextFg).Bold(true),
		muted:     lipgloss.NewStyle().Foreground(m.theme.MutedFg),
		separator: lipgloss.NewStyle().Foreground(m.theme.BorderDim),
	}
}

func (m *Model) agentSessionsEnabled() bool {
	return m.config == nil || !m.config.AgentSessionsDisabled
}

// agentSessionKey returns a stable identity for a session across refreshes.
func agentSessionKey(session *models.AgentSession) string {
	if session == nil {
		return ""
	}
	if strings.TrimSpace(session.SessionKey) != "" {
		return session.SessionKey
	}
	if strings.TrimSpace(session.ID) != "" {
		return session.ID
	}
	return session.JSONLPath
}

// observeAgentSessions seeds the seen-at map for sessions met for the first
// time, so only activity occurring while LazyWorktree is running is flagged as
// unviewed. Without this every pre-existing transcript would light up green on
// start-up.
func (m *Model) observeAgentSessions(sessions []*models.AgentSession) {
	if m.state.data.agentSessionSeenAt == nil {
		m.state.data.agentSessionSeenAt = make(map[string]time.Time, len(sessions))
	}
	for _, session := range sessions {
		key := agentSessionKey(session)
		if key == "" {
			continue
		}
		if _, ok := m.state.data.agentSessionSeenAt[key]; !ok {
			m.state.data.agentSessionSeenAt[key] = session.LastActivity
		}
	}
}

// agentSessionUnviewed reports whether a session has advanced since the user
// last looked at it.
func (m *Model) agentSessionUnviewed(session *models.AgentSession) bool {
	key := agentSessionKey(session)
	if key == "" {
		return false
	}
	seenAt, ok := m.state.data.agentSessionSeenAt[key]
	if !ok {
		return false
	}
	return session.LastActivity.After(seenAt)
}

// markAgentSessionViewed records that the user has seen the session's current
// state, moving its indicator from green back to grey.
func (m *Model) markAgentSessionViewed(session *models.AgentSession) {
	key := agentSessionKey(session)
	if key == "" {
		return
	}
	if m.state.data.agentSessionSeenAt == nil {
		m.state.data.agentSessionSeenAt = make(map[string]time.Time)
	}
	seenAt := session.LastActivity
	if now := time.Now(); now.After(seenAt) {
		seenAt = now
	}
	m.state.data.agentSessionSeenAt[key] = seenAt
}

// anyVisibleAgentBusy reports whether any known session is still working. This
// gates the spinner tick loop so idle CPU stays at zero. It deliberately scans
// every session rather than just the selected worktree's, because the worktree
// list shows a state glyph per row and those must animate too.
func (m *Model) anyVisibleAgentBusy() bool {
	for _, session := range m.state.data.agentSessionsSnapshot {
		if session != nil && agentBusy(session.Activity) {
			return true
		}
	}
	return false
}

// agentSpinnerTick schedules the next frame of the busy indicator.
func (m *Model) agentSpinnerTick() tea.Cmd {
	return tea.Tick(agentSpinnerInterval, func(time.Time) tea.Msg {
		return agentSpinnerTickMsg{}
	})
}

// advanceAgentSpinner steps the animation and repaints from the snapshot
// already in hand, avoiding a session-service query per frame. Both the
// worktree list and the sessions pane carry the indicator, so both are redrawn.
func (m *Model) advanceAgentSpinner() {
	m.state.ui.agentSpinnerFrame++
	m.updateTable()
	if len(m.state.data.agentSessions) == 0 {
		return
	}
	m.agentSessionsContent = m.buildAgentSessionsContent(m.state.data.agentSessions)
	m.state.ui.agentSessionsViewport.SetContent(m.agentSessionsContent)
}

// agentSessionsEqual reports whether two session snapshots carry identical
// data, letting periodic refreshes skip state churn when nothing changed.
func agentSessionsEqual(a, b []*models.AgentSession) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] == nil || b[i] == nil {
			if a[i] != b[i] {
				return false
			}
			continue
		}
		if *a[i] != *b[i] {
			return false
		}
	}
	return true
}

func (m *Model) refreshAgentSessions() tea.Cmd {
	if !m.agentSessionsEnabled() {
		return nil
	}
	service := m.state.services.agentSessions
	if service == nil {
		return nil
	}
	processService := m.state.services.agentProcesses
	return func() tea.Msg {
		var processes []*services.AgentProcess
		if processService != nil {
			snapshot, err := processService.Refresh()
			if err != nil {
				m.debugf("agent sessions: process refresh failed: %v", err)
			} else {
				processes = snapshot
			}
		}
		sessions, err := service.RefreshWithProcesses(processes)
		return agentSessionsUpdatedMsg{sessions: sessions, err: err}
	}
}

func (m *Model) startAgentWatcher() tea.Cmd {
	if !m.agentSessionsEnabled() {
		return nil
	}
	watcher := m.state.services.agentWatch
	if watcher == nil || watcher.Started {
		return nil
	}
	started, err := watcher.Start()
	if err != nil {
		return func() tea.Msg { return errMsg{err: err} }
	}
	if !started {
		return nil
	}
	return m.waitForAgentWatchEvent()
}

func (m *Model) stopAgentWatcher() {
	if watcher := m.state.services.agentWatch; watcher != nil && watcher.Started {
		watcher.Stop()
	}
}

func (m *Model) waitForAgentWatchEvent() tea.Cmd {
	watcher := m.state.services.agentWatch
	if watcher == nil {
		return nil
	}
	events := watcher.NextEvent()
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		_, ok := <-events
		if !ok {
			return nil
		}
		return agentWatchChangedMsg{}
	}
}

func (m *Model) hasAgentSessionsForSelectedWorktree() bool {
	return len(m.agentSessionsForSelectedWorktree()) > 0
}

func (m *Model) hasAnyAgentSessionsForSelectedWorktree() bool {
	return len(m.allAgentSessionsForSelectedWorktree()) > 0
}

func (m *Model) allAgentSessionsForSelectedWorktree() []*models.AgentSession {
	if !m.agentSessionsEnabled() {
		return nil
	}
	wt := m.selectedWorktree()
	service := m.state.services.agentSessions
	if wt == nil || service == nil {
		return nil
	}
	return service.SessionsForWorktree(wt.Path)
}

func (m *Model) agentSessionsForSelectedWorktree() []*models.AgentSession {
	sessions := m.allAgentSessionsForSelectedWorktree()
	if m.state.view.ShowAllAgentSessions {
		return sessions
	}
	visible := make([]*models.AgentSession, 0, len(sessions))
	for _, session := range sessions {
		if session == nil {
			continue
		}
		switch session.LivenessState {
		case models.AgentSessionLivenessActive, models.AgentSessionLivenessSuspect:
			visible = append(visible, session)
		}
	}
	return visible
}

func (m *Model) refreshSelectedWorktreeAgentSessionsPane() {
	selected := m.agentSessionsForSelectedWorktree()
	m.state.data.agentSessions = selected
	if len(selected) == 0 {
		m.state.data.agentSessionIndex = 0
		m.agentSessionsContent = ""
		m.state.ui.agentSessionsViewport.SetYOffset(0)
		if m.state.view.FocusedPane == paneAgentSessions {
			m.state.view.FocusedPane = paneWorktrees
			m.state.ui.worktreeTable.Focus()
			if m.state.view.ZoomedPane == paneAgentSessions {
				m.state.view.ZoomedPane = -1
			}
		}
		return
	}

	if m.state.data.agentSessionIndex >= len(selected) {
		m.state.data.agentSessionIndex = len(selected) - 1
	}
	if m.state.data.agentSessionIndex < 0 {
		m.state.data.agentSessionIndex = 0
	}
	// Looking at a session in the focused pane counts as viewing it, which is
	// what clears the green "finished" indicator back to grey.
	if m.state.view.FocusedPane == paneAgentSessions {
		m.markAgentSessionViewed(selected[m.state.data.agentSessionIndex])
	}
	m.agentSessionsContent = m.buildAgentSessionsContent(selected)
	m.state.ui.agentSessionsViewport.SetContent(m.agentSessionsContent)
	m.syncAgentSessionsViewport()
}

func (m *Model) buildAgentSessionsContent(sessions []*models.AgentSession) string {
	if len(sessions) == 0 {
		return ""
	}

	width := m.agentSessionViewportWidth()
	lines := make([]string, 0, len(sessions)*3)

	for i, session := range sessions {
		if session == nil {
			continue
		}
		cardLines := m.renderAgentSessionCard(
			session,
			width,
			i == m.state.data.agentSessionIndex && m.state.view.FocusedPane == paneAgentSessions,
		)
		lines = append(lines, cardLines...)
		if i < len(sessions)-1 {
			lines = append(lines, m.renderAgentSessionSeparator(width))
		}
	}

	return strings.Join(lines, "\n")
}

func (m *Model) agentSessionViewportWidth() int {
	width := m.state.ui.agentSessionsViewport.Width()
	if width > 0 {
		return width
	}
	return 60
}

func (m *Model) syncAgentSessionsViewport() {
	if len(m.state.data.agentSessions) == 0 {
		m.state.ui.agentSessionsViewport.SetYOffset(0)
		return
	}

	viewportHeight := m.state.ui.agentSessionsViewport.Height()
	if viewportHeight <= 0 {
		return
	}

	selectedLine := m.state.data.agentSessionIndex * 2
	currentOffset := m.state.ui.agentSessionsViewport.YOffset()
	if selectedLine < currentOffset {
		m.state.ui.agentSessionsViewport.SetYOffset(selectedLine)
		return
	}
	if selectedLine >= currentOffset+viewportHeight {
		m.state.ui.agentSessionsViewport.SetYOffset(selectedLine - viewportHeight + 1)
	}
}

func (m *Model) renderAgentSessionCard(session *models.AgentSession, width int, selected bool) []string {
	styles := m.agentRenderStyles()
	prefix := " "
	prefixWidth := lipgloss.Width(prefix)
	prefixStyle := styles.prefix
	titleStyle := styles.title
	if selected {
		prefix = "▏"
		prefixStyle = lipgloss.NewStyle().Foreground(m.theme.Accent)
	}

	contentWidth := max(12, width-prefixWidth)
	marker := m.renderAgentSessionMarker(session)
	right := m.renderAgentSessionRight(session)
	plainRight := ansi.Strip(right)
	title := m.agentSessionTitle(session)
	titlePrefix := marker + " " + title
	if right != "" {
		titleWidth := max(8, contentWidth-lipgloss.Width(plainRight)-1)
		titlePrefix = ansi.Truncate(titlePrefix, titleWidth, "…")
	}
	line1 := titleStyle.Render(titlePrefix)
	if right != "" {
		gapWidth := max(1, contentWidth-lipgloss.Width(ansi.Strip(titlePrefix))-lipgloss.Width(plainRight))
		line1 += strings.Repeat(" ", gapWidth) + right
	}
	line1 = prefixStyle.Render(prefix) + line1

	if session.LivenessState != models.AgentSessionLivenessActive &&
		session.LivenessState != models.AgentSessionLivenessSuspect {
		return []string{m.mutedPaneStyle().Width(width).Render(line1)}
	}
	return []string{line1}
}

func (m *Model) renderAgentSessionSeparator(width int) string {
	return m.agentRenderStyles().separator.Render(strings.Repeat("─", max(6, width-1)))
}

func (m *Model) renderAgentSessionMarker(session *models.AgentSession) string {
	letter := "C"
	fg := m.theme.Accent
	if session != nil && session.Agent == models.AgentKindPi {
		letter = "P"
		fg = m.theme.Cyan
	} else if m.config != nil && strings.EqualFold(strings.TrimSpace(m.config.IconSet), "nerd-font-v3") {
		letter = "✻"
	}
	return lipgloss.NewStyle().Foreground(fg).Bold(true).Render(letter)
}

func (m *Model) renderAgentSessionRight(session *models.AgentSession) string {
	if session == nil {
		return ""
	}
	styles := m.agentRenderStyles()
	parts := []string{m.renderAgentSessionStateIndicator(session)}
	if badge := m.renderAgentSessionLivenessBadge(session); badge != "" {
		parts = append(parts, badge)
	}
	parts = append(parts, styles.muted.Render(formatRelativeTime(session.LastActivity)))
	return strings.Join(parts, " ")
}

// agentBusy reports whether the agent is actively working on the previous
// request, covering reasoning, context compaction and every tool activity.
//
// Approval counts as busy rather than as a prompt for you. The transcript only
// records that a delegated tool call has no result yet, which is equally true
// while that tool is simply still running, so it cannot be read as "the agent
// needs you".
func agentBusy(activity models.AgentActivity) bool {
	switch activity {
	case models.AgentActivityThinking,
		models.AgentActivityCompacting,
		models.AgentActivityReading,
		models.AgentActivityWriting,
		models.AgentActivityRunning,
		models.AgentActivitySearching,
		models.AgentActivityBrowsing,
		models.AgentActivitySpawning,
		models.AgentActivityApproval:
		return true
	default:
		return false
	}
}

// agentSpinnerFrames returns the animation frames for the busy indicator,
// falling back to plain ASCII when icons are disabled.
func (m *Model) agentSpinnerFrames() []string {
	if m.config != nil && !m.config.IconsEnabled() {
		return []string{"|", "/", "-", "\\"}
	}
	return []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
}

// agentStateGlyph resolves a state glyph, honouring the icon set so that
// text-only configurations still render something meaningful.
func (m *Model) agentStateGlyph(icon, ascii string) string {
	if m.config != nil && !m.config.IconsEnabled() {
		return ascii
	}
	return icon
}

// renderAgentSessionStateIndicator renders the compact state glyph for a
// session:
//
//	?        blocked on you: the agent asked a question and cannot proceed
//	spinner  the agent is still working on the last request
//	● green  finished a request you have not looked at yet
//	● grey   the change has been viewed
//	·        idle with nothing outstanding
//
// The "?" covers a question the agent asked outright, which the transcript
// records as an unresolved AskUserQuestion or ExitPlanMode call. A bare end of
// turn still reads as done: an assistant message with no tool call looks the
// same whether the agent asked something in prose or simply finished, so only
// the tools that block on a person are treated as a prompt for you.
func (m *Model) renderAgentSessionStateIndicator(session *models.AgentSession) string {
	if session == nil {
		return ""
	}
	glyph, fg := m.agentSessionState(session)
	return lipgloss.NewStyle().Foreground(fg).Bold(true).Render(glyph)
}

// agentSessionState maps a session onto its state glyph and theme colour.
func (m *Model) agentSessionState(session *models.AgentSession) (string, color.Color) {
	switch {
	case session.Activity == models.AgentActivityWaiting:
		return m.agentStateGlyph("?", "?"), m.theme.WarnFg
	case agentBusy(session.Activity):
		frames := m.agentSpinnerFrames()
		return frames[m.state.ui.agentSpinnerFrame%len(frames)], m.theme.Accent
	case m.agentSessionUnviewed(session):
		return m.agentStateGlyph("●", "*"), m.theme.SuccessFg
	case session.LastActivity.IsZero():
		return m.agentStateGlyph("·", "."), m.theme.MutedFg
	default:
		return m.agentStateGlyph("●", "*"), m.theme.MutedFg
	}
}

// renderWorktreeAgentState renders the state glyph for a worktree row, or an
// empty cell when the worktree has no agent session behind it.
func (m *Model) renderWorktreeAgentState(wt *models.WorktreeInfo) string {
	glyph, fg, ok := m.worktreeAgentState(wt)
	if !ok {
		return ""
	}
	return lipgloss.NewStyle().Foreground(fg).Bold(true).Render(glyph)
}

// worktreeAgentState collapses every session attached to a worktree into the
// single glyph shown in the worktree list. Busy outranks unviewed, which
// outranks settled, so a row never understates what its agent is up to. The
// bool reports whether the worktree has any session at all, letting rows
// without one stay blank instead of carrying a placeholder.
func (m *Model) worktreeAgentState(wt *models.WorktreeInfo) (string, color.Color, bool) {
	if wt == nil || !m.agentSessionsEnabled() {
		return "", nil, false
	}
	base := filepath.Clean(strings.TrimSpace(wt.Path))
	if base == "" || base == "." {
		return "", nil, false
	}

	found := false
	unviewed := false
	for _, session := range m.state.data.agentSessionsSnapshot {
		if session == nil {
			continue
		}
		cwd := filepath.Clean(strings.TrimSpace(session.CWD))
		if cwd == "" || (cwd != base && !strings.HasPrefix(cwd, base+string(filepath.Separator))) {
			continue
		}
		found = true
		// A worktree blocked on you outranks one merely working: it is the row
		// you need to act on.
		if session.Activity == models.AgentActivityWaiting {
			return m.agentStateGlyph("?", "?"), m.theme.WarnFg, true
		}
		if agentBusy(session.Activity) {
			frames := m.agentSpinnerFrames()
			return frames[m.state.ui.agentSpinnerFrame%len(frames)], m.theme.Accent, true
		}
		if m.agentSessionUnviewed(session) {
			unviewed = true
		}
	}

	switch {
	case !found:
		return "", nil, false
	case unviewed:
		return m.agentStateGlyph("●", "*"), m.theme.SuccessFg, true
	default:
		return m.agentStateGlyph("●", "*"), m.theme.MutedFg, true
	}
}

func (m *Model) renderAgentSessionBadge(label string, bg, fg color.Color) string {
	badgeStyle := lipgloss.NewStyle().
		Background(bg).
		Foreground(fg).
		Bold(true).
		Padding(0, 1)
	return badgeStyle.Render(strings.ToUpper(label))
}

func (m *Model) renderAgentSessionLivenessBadge(session *models.AgentSession) string {
	if session == nil {
		return ""
	}
	label := string(session.LivenessState)
	if label == "" {
		return ""
	}
	var bg, fg color.Color
	switch session.LivenessState {
	case models.AgentSessionLivenessActive:
		bg = m.theme.SuccessFg
		fg = m.theme.AccentFg
		switch session.LivenessSource {
		case models.AgentSessionLivenessSourceExactFile:
			label = "exact"
		case models.AgentSessionLivenessSourceNative:
			label = "native"
		}
	case models.AgentSessionLivenessRecent:
		bg = m.theme.Cyan
		fg = m.theme.AccentFg
	case models.AgentSessionLivenessSuspect:
		bg = m.theme.WarnFg
		fg = m.theme.AccentFg
		if session.LivenessSource == models.AgentSessionLivenessSourceCWDHeuristic {
			label = "cwd"
		}
	default:
		bg = m.theme.BorderDim
		fg = m.theme.TextFg
	}
	return m.renderAgentSessionBadge(label, bg, fg)
}

func (m *Model) agentSessionTitle(session *models.AgentSession) string {
	if session == nil {
		return ""
	}
	if strings.TrimSpace(session.Title) != "" {
		return session.Title
	}
	if strings.TrimSpace(session.TaskLabel) != "" {
		return session.TaskLabel
	}
	if strings.TrimSpace(session.DisplayName) != "" {
		return session.DisplayName
	}
	if session.Agent == models.AgentKindPi {
		return "pi session"
	}
	return "Claude session"
}

func (m *Model) mutedPaneStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.theme.MutedFg)
}

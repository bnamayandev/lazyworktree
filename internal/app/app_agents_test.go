package app

import (
	"image/color"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/chmouel/lazyworktree/internal/config"
	"github.com/chmouel/lazyworktree/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentSessionStateIndicatorGlyphs(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir(), IconSet: "nerd-font-v3"}
	m := NewModel(cfg, "")

	tests := []struct {
		name     string
		activity models.AgentActivity
		glyph    string
		colour   color.Color
	}{
		{"thinking spins", models.AgentActivityThinking, "⠋", m.theme.Accent},
		{"compacting spins", models.AgentActivityCompacting, "⠋", m.theme.Accent},
		{"writing spins", models.AgentActivityWriting, "⠋", m.theme.Accent},
		{"running spins", models.AgentActivityRunning, "⠋", m.theme.Accent},
		{"searching spins", models.AgentActivitySearching, "⠋", m.theme.Accent},
		{"spawning spins", models.AgentActivitySpawning, "⠋", m.theme.Accent},
		// A pending delegated tool call is indistinguishable from one that is
		// merely still running, so it counts as busy rather than as a prompt.
		{"pending approval spins", models.AgentActivityApproval, "⠋", m.theme.Accent},
		// Claude ends every turn with a plain assistant message, whether it
		// asked a question or finished the job, so "waiting" reads as settled.
		{"waiting is treated as settled", models.AgentActivityWaiting, "●", m.theme.MutedFg},
		{"settled session is grey", models.AgentActivityIdle, "●", m.theme.MutedFg},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &models.AgentSession{
				ID:           "session",
				Activity:     tt.activity,
				LastActivity: time.Now(),
			}
			glyph, colour := m.agentSessionState(session)
			assert.Equal(t, tt.glyph, glyph)
			assert.Equal(t, tt.colour, colour)
		})
	}
}

func TestAgentSessionStateIndicatorFallsBackToASCII(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir()} // icons disabled
	m := NewModel(cfg, "")

	tests := []struct {
		name     string
		activity models.AgentActivity
		glyph    string
	}{
		{"busy", models.AgentActivityThinking, "|"},
		{"pending approval is busy", models.AgentActivityApproval, "|"},
		{"waiting is settled", models.AgentActivityWaiting, "*"},
		{"settled", models.AgentActivityIdle, "*"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := &models.AgentSession{ID: "s", Activity: tt.activity, LastActivity: time.Now()}
			glyph, _ := m.agentSessionState(session)
			assert.Equal(t, tt.glyph, glyph)
		})
	}
}

func TestAgentSessionUnviewedLifecycle(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir(), IconSet: "nerd-font-v3"}
	m := NewModel(cfg, "")

	started := time.Now().Add(-time.Minute)
	session := &models.AgentSession{
		ID:           "session",
		Activity:     models.AgentActivityThinking,
		LastActivity: started,
	}

	// First sight seeds a baseline so pre-existing transcripts stay quiet.
	m.observeAgentSessions([]*models.AgentSession{session})
	assert.False(t, m.agentSessionUnviewed(session), "a freshly observed session must not be flagged")

	// The agent finishes the request, advancing past the baseline.
	finished := &models.AgentSession{
		ID:           "session",
		Activity:     models.AgentActivityIdle,
		LastActivity: time.Now(),
	}
	assert.True(t, m.agentSessionUnviewed(finished), "a completed request should be flagged for attention")

	glyph, colour := m.agentSessionState(finished)
	assert.Equal(t, "●", glyph)
	assert.Equal(t, m.theme.SuccessFg, colour, "an unviewed completion should be green")

	// Viewing the chat settles it back to grey.
	m.markAgentSessionViewed(finished)
	assert.False(t, m.agentSessionUnviewed(finished))
	_, colour = m.agentSessionState(finished)
	assert.Equal(t, m.theme.MutedFg, colour, "a viewed change should be grey")
}

func TestObserveAgentSessionsKeepsExistingBaseline(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir()}
	m := NewModel(cfg, "")

	session := &models.AgentSession{ID: "session", LastActivity: time.Now().Add(-time.Hour)}
	m.observeAgentSessions([]*models.AgentSession{session})
	m.markAgentSessionViewed(session)

	// A later refresh must not reset the baseline and re-flag a viewed session.
	advanced := &models.AgentSession{ID: "session", LastActivity: session.LastActivity}
	m.observeAgentSessions([]*models.AgentSession{advanced})
	assert.False(t, m.agentSessionUnviewed(advanced))
}

func TestWorktreeAgentStateAggregatesSessions(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir(), IconSet: "nerd-font-v3"}
	m := NewModel(cfg, "")

	root := filepath.Join(cfg.WorktreeDir, "feature")
	wt := &models.WorktreeInfo{Path: root, Branch: "feature"}
	busyFrame := m.agentSpinnerFrames()[0]
	seeded := time.Now().Add(-time.Hour)

	tests := []struct {
		name     string
		sessions []*models.AgentSession
		glyph    string
		colour   color.Color
		ok       bool
	}{
		{
			name: "no sessions leaves the cell empty",
			ok:   false,
		},
		{
			name:     "a session elsewhere does not claim the row",
			sessions: []*models.AgentSession{{ID: "other", CWD: filepath.Join(cfg.WorktreeDir, "unrelated")}},
			ok:       false,
		},
		{
			name:     "a settled session is grey",
			sessions: []*models.AgentSession{{ID: "s", CWD: root, Activity: models.AgentActivityIdle, LastActivity: seeded}},
			glyph:    "●",
			colour:   m.theme.MutedFg,
			ok:       true,
		},
		{
			name:     "a session in a subdirectory still counts",
			sessions: []*models.AgentSession{{ID: "s", CWD: filepath.Join(root, "internal"), Activity: models.AgentActivityIdle, LastActivity: seeded}},
			glyph:    "●",
			colour:   m.theme.MutedFg,
			ok:       true,
		},
		{
			name: "an unviewed completion turns green",
			sessions: []*models.AgentSession{
				{ID: "s", CWD: root, Activity: models.AgentActivityIdle, LastActivity: time.Now()},
			},
			glyph:  "●",
			colour: m.theme.SuccessFg,
			ok:     true,
		},
		{
			name: "busy outranks an unviewed sibling",
			sessions: []*models.AgentSession{
				{ID: "s", CWD: root, Activity: models.AgentActivityIdle, LastActivity: time.Now()},
				{ID: "t", CWD: root, Activity: models.AgentActivityRunning, LastActivity: time.Now()},
			},
			glyph:  busyFrame,
			colour: m.theme.Accent,
			ok:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.state.data.agentSessionSeenAt = map[string]time.Time{"s": seeded, "t": seeded}
			m.state.ui.agentSpinnerFrame = 0
			m.state.data.agentSessionsSnapshot = tt.sessions

			glyph, colour, ok := m.worktreeAgentState(wt)
			require.Equal(t, tt.ok, ok)
			if !tt.ok {
				assert.Empty(t, glyph)
				return
			}
			assert.Equal(t, tt.glyph, glyph)
			assert.Equal(t, tt.colour, colour)
		})
	}
}

func TestWorktreeAgentStateRespectsDisabledSessions(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir(), AgentSessionsDisabled: true}
	m := NewModel(cfg, "")

	root := filepath.Join(cfg.WorktreeDir, "feature")
	m.state.data.agentSessionsSnapshot = []*models.AgentSession{
		{ID: "s", CWD: root, Activity: models.AgentActivityRunning, LastActivity: time.Now()},
	}

	_, _, ok := m.worktreeAgentState(&models.WorktreeInfo{Path: root})
	assert.False(t, ok, "the indicator must stay hidden when agent sessions are disabled")
}

func TestAnyVisibleAgentBusyGatesSpinnerLoop(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir()}
	m := NewModel(cfg, "")

	assert.False(t, m.anyVisibleAgentBusy(), "no sessions means no tick loop")

	// The loop is gated on the full snapshot, not the selected worktree's
	// sessions, because every worktree row carries an animated indicator.
	m.state.data.agentSessionsSnapshot = []*models.AgentSession{
		{ID: "a", Activity: models.AgentActivityIdle},
		{ID: "b", Activity: models.AgentActivityWaiting},
	}
	assert.False(t, m.anyVisibleAgentBusy(), "settled sessions must not spin the loop")

	m.state.data.agentSessionsSnapshot = append(m.state.data.agentSessionsSnapshot,
		&models.AgentSession{ID: "c", Activity: models.AgentActivityRunning})
	assert.True(t, m.anyVisibleAgentBusy(), "a working session should start the loop")

	// An unselected worktree's agent still has to drive the animation.
	m.state.data.agentSessions = nil
	assert.True(t, m.anyVisibleAgentBusy(), "a busy agent off-screen should still tick")
}

func TestAdvanceAgentSpinnerCyclesFrames(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir(), IconSet: "nerd-font-v3"}
	m := NewModel(cfg, "")
	m.state.ui.agentSessionsViewport.SetWidth(80)
	session := &models.AgentSession{ID: "s", Activity: models.AgentActivityThinking, LastActivity: time.Now()}
	m.state.data.agentSessions = []*models.AgentSession{session}

	first, _ := m.agentSessionState(session)
	m.advanceAgentSpinner()
	second, _ := m.agentSessionState(session)

	assert.NotEqual(t, first, second, "the busy indicator should animate between frames")

	// The frame index must wrap rather than run off the end of the frame set.
	m.state.ui.agentSpinnerFrame = len(m.agentSpinnerFrames()) - 1
	m.advanceAgentSpinner()
	wrapped, _ := m.agentSessionState(session)
	assert.Equal(t, first, wrapped)
}

func TestBuildAgentSessionsContentRendersSessionCards(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir()}
	m := NewModel(cfg, "")

	worktreePath := filepath.Join(t.TempDir(), "repo", "feature")
	m.state.data.filteredWts = []*models.WorktreeInfo{{Path: worktreePath, Branch: "feature"}}
	m.state.data.selectedIndex = 0
	m.state.ui.agentSessionsViewport.SetWidth(92)

	sessions := []*models.AgentSession{
		{
			ID:             "claude-open",
			Agent:          models.AgentKindClaude,
			DisplayName:    "Authoring",
			CWD:            filepath.Join(worktreePath, "cmd", "api"),
			TaskLabel:      "editing internal/app/app_agents.go",
			Model:          "claude-sonnet",
			GitBranch:      "feature",
			LastActivity:   time.Now(),
			Activity:       models.AgentActivityWriting,
			IsOpen:         true,
			OpenConfidence: models.AgentOpenConfidenceExact,
		},
		{
			ID:           "pi-offline",
			Agent:        models.AgentKindPi,
			DisplayName:  "Notes tidy",
			CWD:          worktreePath,
			LastActivity: time.Now().Add(-2 * time.Hour),
			Activity:     models.AgentActivityIdle,
			IsOpen:       false,
		},
	}

	content := m.buildAgentSessionsContent(sessions)
	if !strings.Contains(content, "\x1b[") {
		t.Fatal("expected styled output with ANSI sequences")
	}

	plain := ansi.Strip(content)
	for _, want := range []string{
		"Notes tidy",
		"|", // busy spinner frame for the writing session (ASCII icon set)
		"*", // settled indicator for the idle session
		"editing internal/app/app_agents.go",
		"─",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected rendered content to contain %q, got %q", want, plain)
		}
	}

	// The verbose activity badges were replaced by compact state glyphs.
	for _, unwanted := range []string{"WRITING", "IDLE"} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("expected activity badge %q to be replaced by a glyph, got %q", unwanted, plain)
		}
	}

	if got := strings.Count(plain, "editing internal/app/app_agents.go"); got != 1 {
		t.Fatalf("expected task label to appear once as the title, got %d occurrences in %q", got, plain)
	}

	for _, unwanted := range []string{"OPEN", "CWD", "OFFLINE", "transcript match", "cwd match", "feature", "cmd/api", "claude-sonnet", "Authoring"} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("expected rendered content to omit %q, got %q", unwanted, plain)
		}
	}
}

func TestRenderAgentSessionCardSuppressesMetaWhenNarrow(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir()}
	m := NewModel(cfg, "")

	worktreePath := filepath.Join(t.TempDir(), "repo", "feature")
	session := &models.AgentSession{
		ID:             "claude-open",
		Agent:          models.AgentKindClaude,
		DisplayName:    "Authoring",
		CWD:            filepath.Join(worktreePath, "cmd", "api"),
		Model:          "claude-sonnet",
		GitBranch:      "feature",
		LastActivity:   time.Now(),
		Activity:       models.AgentActivityWriting,
		IsOpen:         true,
		OpenConfidence: models.AgentOpenConfidenceExact,
	}

	lines := m.renderAgentSessionCard(session, 22, false)
	if len(lines) != 1 {
		t.Fatalf("expected a single compact line for narrow width, got %d", len(lines))
	}

	plain := ansi.Strip(strings.Join(lines, "\n"))
	for _, unwanted := range []string{"transcript match", "cwd match", "OPEN", "CWD"} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("expected narrow rendering to suppress %q, got %q", unwanted, plain)
		}
	}
}

func TestRenderAgentSessionCardSelectedUsesThinRail(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir()}
	m := NewModel(cfg, "")

	worktreePath := filepath.Join(t.TempDir(), "repo", "feature")
	session := &models.AgentSession{
		ID:           "claude-open",
		Agent:        models.AgentKindClaude,
		DisplayName:  "Authoring",
		CWD:          filepath.Join(worktreePath, "cmd", "api"),
		LastActivity: time.Now(),
		Activity:     models.AgentActivityWaiting,
		IsOpen:       true,
	}

	lines := m.renderAgentSessionCard(session, 72, true)
	if len(lines) == 0 {
		t.Fatal("expected selected card output")
	}

	plain := ansi.Strip(lines[0])
	if !strings.HasPrefix(plain, "▏") {
		t.Fatalf("expected selected line to use a thin left rail, got %q", plain)
	}
	if strings.Contains(plain, "OPEN") || strings.Contains(plain, "CWD") {
		t.Fatalf("expected selected line to prioritise activity only, got %q", plain)
	}
}

func TestRenderAgentSessionCardFallsBackToDisplayNameWithoutTaskLabel(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir()}
	m := NewModel(cfg, "")

	worktreePath := filepath.Join(t.TempDir(), "repo", "feature")
	session := &models.AgentSession{
		ID:           "claude-open",
		Agent:        models.AgentKindClaude,
		DisplayName:  "Authoring",
		CWD:          filepath.Join(worktreePath, "cmd", "api"),
		Model:        "claude-sonnet",
		LastActivity: time.Now(),
		Activity:     models.AgentActivityWaiting,
		IsOpen:       true,
	}

	lines := m.renderAgentSessionCard(session, 72, false)
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "Authoring") {
		t.Fatalf("expected display name fallback when no task label exists, got %q", plain)
	}
	if strings.Contains(plain, "claude-sonnet") {
		t.Fatalf("expected model to stay hidden, got %q", plain)
	}
}

func TestRenderAgentSessionCardUsesGenericFallbackWithoutTaskOrDisplayName(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir()}
	m := NewModel(cfg, "")

	worktreePath := filepath.Join(t.TempDir(), "repo", "feature")
	session := &models.AgentSession{
		ID:           "claude-open",
		Agent:        models.AgentKindClaude,
		CWD:          worktreePath,
		LastActivity: time.Now(),
		Activity:     models.AgentActivityWaiting,
		IsOpen:       true,
	}

	lines := m.renderAgentSessionCard(session, 72, false)
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "Claude session") {
		t.Fatalf("expected generic session fallback title, got %q", plain)
	}
}

// A pending approval is reported as busy: the transcript cannot distinguish a
// tool call awaiting your say-so from one that is simply still running.
func TestRenderAgentSessionCardShowsPendingApprovalAsBusy(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir()}
	m := NewModel(cfg, "")

	session := &models.AgentSession{
		ID:           "claude-open",
		Agent:        models.AgentKindClaude,
		DisplayName:  "Authoring",
		LastActivity: time.Now(),
		Activity:     models.AgentActivityApproval,
		IsOpen:       true,
	}

	lines := m.renderAgentSessionCard(session, 72, false)
	plain := ansi.Strip(strings.Join(lines, "\n"))
	busy := m.agentSpinnerFrames()[0]
	if !strings.Contains(plain, busy) {
		t.Fatalf("expected busy indicator %q, got %q", busy, plain)
	}
}

func TestAgentSessionsForSelectedWorktreeIncludesSuspectByDefault(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir()}
	m := NewModel(cfg, "")

	worktreePath := filepath.Join(t.TempDir(), "repo", "feature")
	m.state.data.filteredWts = []*models.WorktreeInfo{{Path: worktreePath, Branch: "feature"}}
	m.state.data.selectedIndex = 0
	m.state.ui.agentSessionsViewport.SetWidth(92)

	suspect := &models.AgentSession{
		ID:             "claude-suspect",
		Agent:          models.AgentKindClaude,
		DisplayName:    "Authoring",
		CWD:            worktreePath,
		Title:          "editing internal/app/app_agents.go",
		LastActivity:   time.Now(),
		Activity:       models.AgentActivityWriting,
		LivenessState:  models.AgentSessionLivenessSuspect,
		LivenessSource: models.AgentSessionLivenessSourceCWDHeuristic,
		OpenConfidence: models.AgentOpenConfidenceCWD,
	}

	m.state.view.ShowAllAgentSessions = false
	visible := []*models.AgentSession{suspect}
	content := m.buildAgentSessionsContent(visible)
	plain := ansi.Strip(content)
	if !strings.Contains(plain, "CWD") {
		t.Fatalf("expected suspect session badge to render by default, got %q", plain)
	}
}

func TestRenderAgentSessionMarkerUsesNerdFontGlyphForClaude(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir(), IconSet: "nerd-font-v3"}
	m := NewModel(cfg, "")

	marker := ansi.Strip(m.renderAgentSessionMarker(&models.AgentSession{Agent: models.AgentKindClaude}))
	if marker != "✻" {
		t.Fatalf("expected nerd font Claude marker, got %q", marker)
	}
}

func TestRenderAgentSessionMarkerUsesTextGlyphForClaude(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir(), IconSet: "text"}
	m := NewModel(cfg, "")

	marker := ansi.Strip(m.renderAgentSessionMarker(&models.AgentSession{Agent: models.AgentKindClaude}))
	if marker != "C" {
		t.Fatalf("expected text Claude marker, got %q", marker)
	}
}

func TestAgentSessionsEqual(t *testing.T) {
	now := time.Now()
	a := &models.AgentSession{ID: "one", CWD: "/tmp/wt", Status: models.AgentSessionStatusWaitingForUser, LastActivity: now}
	b := &models.AgentSession{ID: "one", CWD: "/tmp/wt", Status: models.AgentSessionStatusWaitingForUser, LastActivity: now}
	c := &models.AgentSession{ID: "two", CWD: "/tmp/wt"}

	if !agentSessionsEqual(nil, nil) {
		t.Fatal("expected nil slices to be equal")
	}
	if !agentSessionsEqual([]*models.AgentSession{a}, []*models.AgentSession{b}) {
		t.Fatal("expected identical session values to be equal")
	}
	if agentSessionsEqual([]*models.AgentSession{a}, []*models.AgentSession{c}) {
		t.Fatal("expected differing sessions to be unequal")
	}
	if agentSessionsEqual([]*models.AgentSession{a}, nil) {
		t.Fatal("expected differing lengths to be unequal")
	}
	if !agentSessionsEqual([]*models.AgentSession{nil}, []*models.AgentSession{nil}) {
		t.Fatal("expected nil entries to be equal")
	}
	if agentSessionsEqual([]*models.AgentSession{nil}, []*models.AgentSession{a}) {
		t.Fatal("expected nil versus value entry to be unequal")
	}
}

func TestAgentSessionsUpdatedMsgSkipsUnchangedSnapshot(t *testing.T) {
	cfg := &config.AppConfig{WorktreeDir: t.TempDir()}
	m := NewModel(cfg, "")

	current := []*models.AgentSession{{ID: "one", CWD: "/tmp/wt"}}
	m.state.data.agentSessionsSnapshot = current

	clone := []*models.AgentSession{{ID: "one", CWD: "/tmp/wt"}}
	_, _ = m.Update(agentSessionsUpdatedMsg{sessions: clone})
	if &m.state.data.agentSessionsSnapshot[0] != &current[0] {
		t.Fatal("expected unchanged snapshot to keep existing state")
	}

	changed := []*models.AgentSession{{ID: "two", CWD: "/tmp/wt"}}
	_, _ = m.Update(agentSessionsUpdatedMsg{sessions: changed})
	if m.state.data.agentSessionsSnapshot[0].ID != "two" {
		t.Fatal("expected changed snapshot to replace state")
	}
}

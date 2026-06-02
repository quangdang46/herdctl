package codex

import (
	"reflect"
	"testing"
)

// Representative captured-pane fixtures, one per state. These approximate what a
// tmux capture of a Codex pane would contain in each state. Where the exact
// Codex string is unverified (see Marker.Assumed in palette.go), the fixture
// uses the same best-effort string the table matches, so the test proves the
// classifier wiring even if the literal copy later needs correcting.
const (
	fixtureIdle = `
Welcome to Codex.

›
? for shortcuts
`

	fixtureSlashPaletteOpen = `
› /
  /model      switch the active model
  /approvals  configure approval policy
  /init       initialize project context
  /compact    compact the conversation
slash commands
`

	fixtureGoalPalettePrimed = `
Plan mode

goal: implement the palette-state classifier
Press Enter to send
`

	fixtureDialogOpen = `
╭───────────────────────────────────────────╮
│ Allow command?                            │
│   $ rm -rf build/                          │
│ Do you want to allow this command?         │
│   [ Yes ]   [ No ]                         │
╰───────────────────────────────────────────╯
`

	fixtureUnknown = `
some unrelated shell output
make: *** [build] Error 2
$ ls -la
`
)

func TestClassify_PerState(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantState   PaletteState
		wantMarkers []string
	}{
		{
			name:        "idle",
			content:     fixtureIdle,
			wantState:   StateIdle,
			wantMarkers: []string{"? for shortcuts", "›"},
		},
		{
			name:        "slash_palette_open",
			content:     fixtureSlashPaletteOpen,
			wantState:   StateSlashPaletteOpen,
			wantMarkers: []string{"/approvals", "/model", "/init", "/compact", "slash commands"},
		},
		{
			name:        "goal_palette_primed",
			content:     fixtureGoalPalettePrimed,
			wantState:   StateGoalPalettePrimed,
			wantMarkers: []string{"Press Enter to send", "goal:", "Plan mode"},
		},
		{
			name:        "dialog_open",
			content:     fixtureDialogOpen,
			wantState:   StateDialogOpen,
			wantMarkers: []string{"Allow command?", "Do you want to allow", "[ Yes ]", "╭─"},
		},
		{
			name:        "unknown",
			content:     fixtureUnknown,
			wantState:   StateUnknown,
			wantMarkers: nil,
		},
		{
			name:        "empty",
			content:     "",
			wantState:   StateUnknown,
			wantMarkers: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.content)
			if got.State != tt.wantState {
				t.Fatalf("Classify() state = %q, want %q", got.State, tt.wantState)
			}
			if !reflect.DeepEqual(got.MarkersMatched, tt.wantMarkers) {
				t.Fatalf("Classify() markers = %#v, want %#v", got.MarkersMatched, tt.wantMarkers)
			}
		})
	}
}

// TestClassify_Precedence proves the priority ordering: when a higher-priority
// overlay (a modal dialog) is drawn over a lower-priority one (an open slash
// palette), the most input-capturing state wins.
func TestClassify_Precedence(t *testing.T) {
	combined := fixtureSlashPaletteOpen + fixtureDialogOpen
	got := Classify(combined)
	if got.State != StateDialogOpen {
		t.Fatalf("expected dialog_open to win over slash_palette_open, got %q", got.State)
	}
}

// TestClassify_CaseInsensitive proves matching ignores case.
func TestClassify_CaseInsensitive(t *testing.T) {
	got := Classify("? FOR SHORTCUTS")
	if got.State != StateIdle {
		t.Fatalf("expected idle for upper-cased idle marker, got %q", got.State)
	}
}

// TestOrderedRules_SortedByPriority guards the precedence contract independent
// of the literal authoring order of StateMarkers.
func TestOrderedRules_SortedByPriority(t *testing.T) {
	rules := orderedRules()
	for i := 1; i < len(rules); i++ {
		if rules[i].Priority < rules[i-1].Priority {
			t.Fatalf("orderedRules not ascending: %d before %d", rules[i-1].Priority, rules[i].Priority)
		}
	}
}

// TestAllStates_Closed guards the closed state set.
func TestAllStates_Closed(t *testing.T) {
	want := []PaletteState{StateIdle, StateSlashPaletteOpen, StateGoalPalettePrimed, StateDialogOpen, StateUnknown}
	if !reflect.DeepEqual(AllStates(), want) {
		t.Fatalf("AllStates() = %#v, want %#v", AllStates(), want)
	}
}

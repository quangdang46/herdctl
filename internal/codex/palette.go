// Package codex classifies the on-screen state of a Codex agent pane.
//
// Codex (the OpenAI Codex CLI, agent type "cod") presents a terminal UI with
// transient overlays: a slash-command palette, a goal/plan prime indicator, and
// modal dialogs (approvals, confirmations). NTM commands that drive Codex panes
// (the #165-169 pane-control set) need to know which overlay, if any, is
// currently visible before they send keystrokes. This package answers that
// question by classifying a captured pane snapshot into a small, well-defined
// state machine.
//
// # Fragility note
//
// The classification depends on matching literal strings that Codex prints to
// the terminal. Those strings are NOT a stable public contract — Codex can
// change its TUI copy at any release. To keep that fragility contained, ALL
// match patterns live in exactly one place: the StateMarkers table below. When
// Codex changes its UI, update that table (and the table only); the classifier
// logic never needs to change.
//
// Markers that are best-effort inferences (rather than strings we have verified
// against a live Codex pane) are flagged with an "ASSUMPTION:" comment so they
// can be validated and corrected against a real pane.
package codex

import "strings"

// PaletteState is the classified state of a Codex pane's command/goal palette.
//
// The state set is intentionally small and closed. Any pane content that does
// not match a more specific state falls through to StateUnknown (when a Codex
// overlay marker is absent but the content is non-trivial) or StateIdle (when
// the pane shows a quiescent Codex prompt waiting for input).
type PaletteState string

const (
	// StateIdle means the pane shows a quiescent Codex input prompt with no
	// overlay open — Codex is waiting for the user to type. This is the safe
	// state to send a fresh prompt or open the palette.
	StateIdle PaletteState = "idle"

	// StateSlashPaletteOpen means the slash-command palette is open: the user
	// has typed "/" and Codex is showing the filterable list of slash commands
	// (/model, /approvals, /init, ...). Sending a plain prompt here would be
	// interpreted as palette filtering, not as a message.
	StateSlashPaletteOpen PaletteState = "slash_palette_open"

	// StateGoalPalettePrimed means a goal/plan prompt has been primed but not
	// yet submitted — Codex is showing the goal-entry / plan-prime affordance
	// (e.g. the "/plan" or goal-prompt surface) awaiting confirmation.
	StateGoalPalettePrimed PaletteState = "goal_palette_primed"

	// StateDialogOpen means a modal dialog/overlay is open — an approval
	// request, a confirmation, or another framed modal that captures input.
	// Keystrokes are routed to the dialog, not the main prompt.
	StateDialogOpen PaletteState = "dialog_open"

	// StateUnknown means the content did not match any known state. Callers
	// should treat this conservatively (do not assume the palette is closed).
	StateUnknown PaletteState = "unknown"
)

// String returns the state's stable string value (matches the JSON encoding).
func (s PaletteState) String() string { return string(s) }

// Marker is a single substring (or set of substrings) whose presence in the
// captured pane content is evidence of a particular state.
type Marker struct {
	// Substr is the literal substring to look for in the captured content.
	// Matching is case-insensitive (the classifier lowercases both sides).
	Substr string

	// Why documents what this marker corresponds to in the Codex UI. Keep this
	// current; it is the primary aid for re-validating markers against a live
	// pane.
	Why string

	// Assumed is true when Substr is a best-effort inference rather than a
	// string verified against a live Codex pane. Assumed markers are the first
	// thing to re-check when classification misbehaves.
	Assumed bool
}

// StateRule binds a state to the markers that select it. A rule fires when ANY
// of its markers is present in the captured content (logical OR). Rules are
// evaluated in Priority order (lowest number first); the first state whose
// rule fires wins. This ordering is how mutually-overlapping UIs (e.g. a dialog
// drawn over an open palette) resolve deterministically to the most specific,
// most input-capturing state.
type StateRule struct {
	State    PaletteState
	Priority int
	Markers  []Marker
}

// StateMarkers is the SINGLE SOURCE OF TRUTH for Codex pane-state classification.
//
// To adapt to a Codex UI change, edit ONLY this table:
//   - Add/adjust Marker.Substr values to match the new on-screen strings.
//   - Flip Marker.Assumed to false once a string is verified against a live pane.
//   - Adjust Priority only if the precedence between overlapping overlays changes.
//
// Do NOT scatter string matching elsewhere. The classifier (Classify) is pure
// table-driven logic and should not need edits when Codex's copy changes.
//
// Priority rationale (lower fires first):
//
//	10 dialog_open         — a modal captures input; it wins over everything.
//	20 slash_palette_open  — the slash list is open over the prompt.
//	30 goal_palette_primed — a goal/plan prompt is primed but not submitted.
//	40 idle                — quiescent prompt, no overlay.
var StateMarkers = []StateRule{
	{
		State:    StateDialogOpen,
		Priority: 10,
		Markers: []Marker{
			// Codex approval prompts ask the user to allow/deny a command or
			// edit. These phrasings appear in the approval modal body.
			{Substr: "Allow command?", Why: "Codex command-approval modal title", Assumed: true},
			{Substr: "Apply this change?", Why: "Codex edit-approval modal title", Assumed: true},
			{Substr: "Do you want to allow", Why: "Codex approval prompt body", Assumed: true},
			// ASSUMPTION: approval modals offer y/n style choices. The exact
			// affordance text is unverified; "(y/n)" and the bracketed Yes/No
			// pair are the most common Codex/terminal conventions.
			{Substr: "[ Yes ]", Why: "modal Yes affordance", Assumed: true},
			{Substr: "Approve  Reject", Why: "approval modal button row", Assumed: true},
			// Box-drawing top frame is a strong signal of a framed modal. This
			// is distinctive but can also appear in non-modal panels, so it is
			// only used at dialog priority alongside the textual markers above.
			// ASSUMPTION: Codex draws modals with rounded box corners (╭ … ╮).
			{Substr: "╭─", Why: "rounded modal frame top-left (box-drawing)", Assumed: true},
		},
	},
	{
		State:    StateSlashPaletteOpen,
		Priority: 20,
		Markers: []Marker{
			// When the slash palette is open Codex lists its built-in slash
			// commands. The presence of several well-known command names in the
			// captured frame is the most distinctive, copy-stable signal.
			{Substr: "/approvals", Why: "slash-command list entry (approvals)", Assumed: true},
			{Substr: "/model", Why: "slash-command list entry (model picker)", Assumed: false}, // /model is referenced elsewhere in NTM's Codex handling
			{Substr: "/init", Why: "slash-command list entry (init)", Assumed: true},
			{Substr: "/compact", Why: "slash-command list entry (compact context)", Assumed: true},
			// ASSUMPTION: the palette header/hint text. Codex commonly shows a
			// hint line while filtering slash commands.
			{Substr: "slash commands", Why: "slash palette header/hint", Assumed: true},
		},
	},
	{
		State:    StateGoalPalettePrimed,
		Priority: 30,
		Markers: []Marker{
			// A primed goal/plan prompt awaiting submission. ASSUMPTION: these
			// strings approximate Codex's plan/goal affordance; verify against a
			// live pane and update Substr as needed.
			{Substr: "/plan", Why: "slash entry / primed plan goal", Assumed: true},
			{Substr: "Press Enter to send", Why: "goal primed, awaiting submit", Assumed: true},
			{Substr: "goal:", Why: "explicit goal-entry label", Assumed: true},
			{Substr: "Plan mode", Why: "Codex plan-mode indicator", Assumed: true},
		},
	},
	{
		State:    StateIdle,
		Priority: 40,
		Markers: []Marker{
			// Quiescent Codex prompt waiting for input. These mirror the idle
			// markers already used by NTM's agent state parser
			// (internal/agent/patterns.go codIdlePatterns), so they are the most
			// trustworthy markers in this table.
			{Substr: "? for shortcuts", Why: "Codex idle prompt hint line", Assumed: false},
			{Substr: "codex>", Why: "Codex shell-style prompt", Assumed: false},
			// ASSUMPTION: the chevron prompt glyph. patterns.go matches a "›"
			// chevron at line start for the idle Codex prompt.
			{Substr: "›", Why: "Codex chevron input prompt", Assumed: false},
		},
	},
}

// Classification is the full result of classifying captured pane content.
type Classification struct {
	// State is the resolved pane state.
	State PaletteState

	// MarkersMatched lists the literal marker substrings that fired for the
	// winning state, in table order. Empty when State is idle-by-default or
	// unknown.
	MarkersMatched []string
}

// Classify inspects captured Codex pane content and returns its palette state
// plus the markers that selected it.
//
// Algorithm (pure, table-driven over StateMarkers):
//  1. Evaluate rules in ascending Priority order.
//  2. The first rule with at least one matching marker wins; its matched
//     markers are recorded.
//  3. If no rule matches, return StateUnknown with no markers.
//
// Matching is case-insensitive. content is the raw captured pane text.
func Classify(content string) Classification {
	lower := strings.ToLower(content)

	// Evaluate in priority order. StateMarkers is authored in priority order,
	// but sort defensively so reordering the table literal cannot change
	// precedence semantics.
	rules := orderedRules()

	for _, rule := range rules {
		var matched []string
		for _, m := range rule.Markers {
			if m.Substr == "" {
				continue
			}
			if strings.Contains(lower, strings.ToLower(m.Substr)) {
				matched = append(matched, m.Substr)
			}
		}
		if len(matched) > 0 {
			return Classification{State: rule.State, MarkersMatched: matched}
		}
	}

	return Classification{State: StateUnknown, MarkersMatched: nil}
}

// orderedRules returns StateMarkers sorted by ascending Priority. It does not
// mutate the package-level table.
func orderedRules() []StateRule {
	rules := make([]StateRule, len(StateMarkers))
	copy(rules, StateMarkers)
	// Simple insertion sort; the table is tiny and this avoids importing sort
	// for a handful of entries.
	for i := 1; i < len(rules); i++ {
		for j := i; j > 0 && rules[j].Priority < rules[j-1].Priority; j-- {
			rules[j], rules[j-1] = rules[j-1], rules[j]
		}
	}
	return rules
}

// AllStates returns the closed set of palette states, in canonical order. Useful
// for documentation, validation, and exhaustive tests.
func AllStates() []PaletteState {
	return []PaletteState{
		StateIdle,
		StateSlashPaletteOpen,
		StateGoalPalettePrimed,
		StateDialogOpen,
		StateUnknown,
	}
}

package panels

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Dicklesworthstone/ntm/internal/ensemble"
	"github.com/Dicklesworthstone/ntm/internal/tui/components"
	"github.com/Dicklesworthstone/ntm/internal/tui/theme"
)

// CompareSection identifies which section is currently displayed.
type CompareSection string

const (
	CompareSectionSummary       CompareSection = "summary"
	CompareSectionModes         CompareSection = "modes"
	CompareSectionFindings      CompareSection = "findings"
	CompareSectionConclusions   CompareSection = "conclusions"
	CompareSectionContributions CompareSection = "contributions"
)

// CompareUpdateMsg updates the comparison data.
type CompareUpdateMsg struct {
	Result *ensemble.ComparisonResult
	Err    error
	Gen    int
}

// CompareData holds comparison data for display.
type CompareData struct {
	RunA   string
	RunB   string
	Result *ensemble.ComparisonResult
}

// ComparePanel displays a comparison between two ensemble runs.
type ComparePanel struct {
	PanelBase
	data         CompareData
	err          error
	section      CompareSection
	theme        theme.Theme
	scrollOffset int
}

// compareConfig returns the configuration for the compare panel.
func compareConfig() PanelConfig {
	return PanelConfig{
		ID:              "compare",
		Title:           "Ensemble Comparison",
		Priority:        PriorityNormal,
		RefreshInterval: 30 * time.Second,
		MinWidth:        40,
		MinHeight:       12,
		Collapsible:     true,
	}
}

// NewComparePanel creates a new comparison panel.
func NewComparePanel() *ComparePanel {
	return &ComparePanel{
		PanelBase: NewPanelBase(compareConfig()),
		theme:     theme.Current(),
		section:   CompareSectionSummary,
	}
}

// Init implements tea.Model.
func (p *ComparePanel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (p *ComparePanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !p.IsFocused() {
			return p, nil
		}
		switch msg.String() {
		case "1":
			p.section = CompareSectionSummary
			p.scrollOffset = 0
		case "2":
			p.section = CompareSectionModes
			p.scrollOffset = 0
		case "3":
			p.section = CompareSectionFindings
			p.scrollOffset = 0
		case "4":
			p.section = CompareSectionConclusions
			p.scrollOffset = 0
		case "5":
			p.section = CompareSectionContributions
			p.scrollOffset = 0
		case "j", "down":
			p.scrollOffset++
		case "k", "up":
			if p.scrollOffset > 0 {
				p.scrollOffset--
			}
		}
	}
	return p, nil
}

// SetData updates the comparison data.
func (p *ComparePanel) SetData(data CompareData, err error) {
	p.data = data
	p.err = err
	if err == nil && data.Result != nil {
		p.SetLastUpdate(time.Now())
	}
}

// HasError returns true if there's an active error.
func (p *ComparePanel) HasError() bool {
	return p.err != nil
}

// Keybindings returns panel-specific shortcuts.
func (p *ComparePanel) Keybindings() []Keybinding {
	return []Keybinding{
		{
			Key:         key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "summary")),
			Description: "Show summary",
			Action:      "summary",
		},
		{
			Key:         key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "modes")),
			Description: "Show mode changes",
			Action:      "modes",
		},
		{
			Key:         key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "findings")),
			Description: "Show finding changes",
			Action:      "findings",
		},
		{
			Key:         key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "conclusions")),
			Description: "Show conclusion changes",
			Action:      "conclusions",
		},
		{
			Key:         key.NewBinding(key.WithKeys("5"), key.WithHelp("5", "contributions")),
			Description: "Show contribution changes",
			Action:      "contributions",
		},
	}
}

// View renders the panel.
func (p *ComparePanel) View() string {
	t := p.theme
	w, h := p.Width(), p.Height()

	borderColor := t.Surface1
	bgColor := t.Base
	if p.IsFocused() {
		borderColor = t.Primary
		bgColor = t.Surface0
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(bgColor).
		Width(w-2).
		Height(h-2).
		Padding(0, 1)

	var content strings.Builder

	title := p.Config().Title
	if p.err != nil {
		errorBadge := lipgloss.NewStyle().
			Background(t.Red).
			Foreground(t.Base).
			Bold(true).
			Padding(0, 1).
			Render("Error")
		title = title + " " + errorBadge
	} else if staleBadge := components.RenderStaleBadge(p.LastUpdate(), p.Config().RefreshInterval); staleBadge != "" {
		title = title + " " + staleBadge
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Lavender).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(t.Surface1).
		Width(w - 4).
		Align(lipgloss.Center)

	content.WriteString(headerStyle.Render(title) + "\n")

	if p.err != nil {
		content.WriteString(components.ErrorState(p.err.Error(), "Press r to retry", w-4) + "\n")
		return boxStyle.Render(FitToHeight(content.String(), h-4))
	}

	if p.data.Result == nil {
		content.WriteString(components.EmptyState(
			"No comparison loaded. Use 'herdctl ensemble compare <run1> <run2>'",
			w-4,
		) + "\n")
		return boxStyle.Render(FitToHeight(content.String(), h-4))
	}

	// Navigation tabs
	tabs := p.renderTabs()
	content.WriteString(tabs + "\n\n")

	// Section content with scrolling
	var sectionContent string
	switch p.section {
	case CompareSectionSummary:
		sectionContent = p.renderSummary()
	case CompareSectionModes:
		sectionContent = p.renderModes()
	case CompareSectionFindings:
		sectionContent = p.renderFindings()
	case CompareSectionConclusions:
		sectionContent = p.renderConclusions()
	case CompareSectionContributions:
		sectionContent = p.renderContributions()
	}

	// Apply scroll offset to section content
	sectionLines := strings.Split(sectionContent, "\n")
	scrollOffset := p.scrollOffset
	if scrollOffset >= len(sectionLines) {
		scrollOffset = max(0, len(sectionLines)-1)
	}
	if scrollOffset > 0 && len(sectionLines) > scrollOffset {
		sectionLines = sectionLines[scrollOffset:]
	}
	content.WriteString(strings.Join(sectionLines, "\n"))

	return boxStyle.Render(FitToHeight(content.String(), h-4))
}

func (p *ComparePanel) renderTabs() string {
	t := p.theme
	sections := []struct {
		key     string
		label   string
		section CompareSection
	}{
		{"1", "Summary", CompareSectionSummary},
		{"2", "Modes", CompareSectionModes},
		{"3", "Findings", CompareSectionFindings},
		{"4", "Conclusions", CompareSectionConclusions},
		{"5", "Contrib", CompareSectionContributions},
	}

	var tabs []string
	for _, s := range sections {
		style := lipgloss.NewStyle().Padding(0, 1)
		if s.section == p.section {
			style = style.Background(t.Primary).Foreground(t.Base).Bold(true)
		} else {
			style = style.Foreground(t.Subtext)
		}
		tabs = append(tabs, style.Render(fmt.Sprintf("[%s]%s", s.key, s.label)))
	}
	return strings.Join(tabs, " ")
}

func (p *ComparePanel) renderSummary() string {
	r := p.data.Result
	var lines []string

	lines = append(lines, fmt.Sprintf("Comparing: %s vs %s", r.RunA, r.RunB))
	lines = append(lines, fmt.Sprintf("Generated: %s", r.GeneratedAt.Format("2006-01-02 15:04:05")))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Summary: %s", r.Summary))
	lines = append(lines, "")

	// Quick stats
	lines = append(lines, "Quick Stats:")
	lines = append(lines, fmt.Sprintf("  Modes: +%d / -%d / =%d",
		r.ModeDiff.AddedCount, r.ModeDiff.RemovedCount, r.ModeDiff.UnchangedCount))
	lines = append(lines, fmt.Sprintf("  Findings: +%d / -%d / ~%d / =%d",
		r.FindingsDiff.NewCount, r.FindingsDiff.MissingCount,
		r.FindingsDiff.ChangedCount, r.FindingsDiff.UnchangedCount))

	return strings.Join(lines, "\n")
}

func (p *ComparePanel) renderModes() string {
	r := p.data.Result
	var lines []string

	if r.ModeDiff.AddedCount > 0 {
		lines = append(lines, fmt.Sprintf("Added (%d):", r.ModeDiff.AddedCount))
		for _, m := range r.ModeDiff.Added {
			lines = append(lines, fmt.Sprintf("  + %s", m))
		}
		lines = append(lines, "")
	}

	if r.ModeDiff.RemovedCount > 0 {
		lines = append(lines, fmt.Sprintf("Removed (%d):", r.ModeDiff.RemovedCount))
		for _, m := range r.ModeDiff.Removed {
			lines = append(lines, fmt.Sprintf("  - %s", m))
		}
		lines = append(lines, "")
	}

	if r.ModeDiff.UnchangedCount > 0 {
		lines = append(lines, fmt.Sprintf("Unchanged (%d):", r.ModeDiff.UnchangedCount))
		for _, m := range r.ModeDiff.Unchanged {
			lines = append(lines, fmt.Sprintf("    %s", m))
		}
	}

	if len(lines) == 0 {
		return "No mode changes"
	}
	return strings.Join(lines, "\n")
}

func (p *ComparePanel) renderFindings() string {
	r := p.data.Result
	var lines []string

	lines = append(lines, fmt.Sprintf("New: %d | Missing: %d | Changed: %d | Unchanged: %d",
		r.FindingsDiff.NewCount, r.FindingsDiff.MissingCount,
		r.FindingsDiff.ChangedCount, r.FindingsDiff.UnchangedCount))
	lines = append(lines, "")

	// Show top 5 new findings
	if len(r.FindingsDiff.New) > 0 {
		lines = append(lines, "New Findings:")
		for i, f := range r.FindingsDiff.New {
			if i >= 5 {
				lines = append(lines, fmt.Sprintf("  ... and %d more", len(r.FindingsDiff.New)-5))
				break
			}
			text := truncateString(f.Text, 50)
			lines = append(lines, fmt.Sprintf("  + [%s] %s", f.ModeID, text))
		}
		lines = append(lines, "")
	}

	// Show top 5 missing findings
	if len(r.FindingsDiff.Missing) > 0 {
		lines = append(lines, "Missing Findings:")
		for i, f := range r.FindingsDiff.Missing {
			if i >= 5 {
				lines = append(lines, fmt.Sprintf("  ... and %d more", len(r.FindingsDiff.Missing)-5))
				break
			}
			text := truncateString(f.Text, 50)
			lines = append(lines, fmt.Sprintf("  - [%s] %s", f.ModeID, text))
		}
	}

	if len(lines) == 2 {
		return "No finding changes"
	}
	return strings.Join(lines, "\n")
}

func (p *ComparePanel) renderConclusions() string {
	r := p.data.Result
	var lines []string

	if r.ConclusionDiff.SynthesisChanged {
		lines = append(lines, "Synthesis: CHANGED")
	} else {
		lines = append(lines, "Synthesis: unchanged")
	}
	lines = append(lines, "")

	if len(r.ConclusionDiff.ThesisChanges) > 0 {
		lines = append(lines, fmt.Sprintf("Thesis Changes (%d):", len(r.ConclusionDiff.ThesisChanges)))
		for _, tc := range r.ConclusionDiff.ThesisChanges {
			lines = append(lines, fmt.Sprintf("  [%s]:", tc.ModeID))
			if tc.ThesisA != "" {
				lines = append(lines, fmt.Sprintf("    - %s", truncateString(tc.ThesisA, 40)))
			}
			if tc.ThesisB != "" {
				lines = append(lines, fmt.Sprintf("    + %s", truncateString(tc.ThesisB, 40)))
			}
		}
	}

	if len(lines) == 2 {
		return "No conclusion changes"
	}
	return strings.Join(lines, "\n")
}

func (p *ComparePanel) renderContributions() string {
	r := p.data.Result
	var lines []string

	lines = append(lines, fmt.Sprintf("Overlap Rate: %.2f -> %.2f",
		r.ContributionDiff.OverlapRateA, r.ContributionDiff.OverlapRateB))
	lines = append(lines, fmt.Sprintf("Diversity Score: %.2f -> %.2f",
		r.ContributionDiff.DiversityScoreA, r.ContributionDiff.DiversityScoreB))
	lines = append(lines, "")

	if len(r.ContributionDiff.ScoreDeltas) > 0 {
		lines = append(lines, "Score Changes:")
		for _, sd := range r.ContributionDiff.ScoreDeltas {
			sign := "+"
			if sd.Delta < 0 {
				sign = ""
			}
			lines = append(lines, fmt.Sprintf("  %s: %.2f -> %.2f (%s%.2f)",
				sd.ModeID, sd.ScoreA, sd.ScoreB, sign, sd.Delta))
		}
		lines = append(lines, "")
	}

	if len(r.ContributionDiff.RankChanges) > 0 {
		lines = append(lines, "Rank Changes:")
		for _, rc := range r.ContributionDiff.RankChanges {
			arrow := "="
			if rc.Delta < 0 {
				arrow = "↑"
			} else if rc.Delta > 0 {
				arrow = "↓"
			}
			lines = append(lines, fmt.Sprintf("  %s: #%d -> #%d %s",
				rc.ModeID, rc.RankA, rc.RankB, arrow))
		}
	}

	if len(lines) == 3 {
		return "No contribution changes"
	}
	return strings.Join(lines, "\n")
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

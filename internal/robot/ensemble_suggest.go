// Package robot provides machine-readable output for AI agents.
// ensemble_suggest.go implements --robot-ensemble-suggest for preset recommendation.
package robot

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/Dicklesworthstone/ntm/internal/ensemble"
)

// EnsembleSuggestOutput is the structured response for --robot-ensemble-suggest.
type EnsembleSuggestOutput struct {
	RobotResponse
	Question    string                     `json:"question"`
	TopPick     *EnsembleSuggestion        `json:"top_pick,omitempty"`
	Suggestions []EnsembleSuggestion       `json:"suggestions"`
	AgentHints  *EnsembleSuggestAgentHints `json:"_agent_hints,omitempty"`
}

// EnsembleSuggestion represents a single preset suggestion.
type EnsembleSuggestion struct {
	PresetName  string   `json:"preset_name"`
	DisplayName string   `json:"display_name"`
	Description string   `json:"description"`
	Score       float64  `json:"score"`
	Reasons     []string `json:"reasons"`
	ModeCount   int      `json:"mode_count"`
	Tags        []string `json:"tags"`
	SpawnCmd    string   `json:"spawn_cmd,omitempty"`
}

// EnsembleSuggestAgentHints provides actionable guidance for AI agents.
type EnsembleSuggestAgentHints struct {
	Summary          string        `json:"summary,omitempty"`
	SuggestedActions []RobotAction `json:"suggested_actions,omitempty"`
	SpawnCommand     string        `json:"spawn_command,omitempty"`
}

// GetEnsembleSuggest retrieves ensemble preset suggestions for a question.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetEnsembleSuggest(question string) (*EnsembleSuggestOutput, error) {
	slog.Debug("[robot] ensemble-suggest",
		"question_len", len(question),
	)

	output := &EnsembleSuggestOutput{
		RobotResponse: NewRobotResponse(true),
		Question:      question,
		Suggestions:   []EnsembleSuggestion{},
	}

	question = strings.TrimSpace(question)
	if question == "" {
		output.RobotResponse = NewErrorResponse(
			ErrMissingQuestion,
			ErrCodeInvalidFlag,
			"Provide a question: herdctl --robot-ensemble-suggest=\"What security issues exist?\"",
		)
		return output, nil
	}

	engine := ensemble.GlobalSuggestionEngine()
	result := engine.Suggest(question)

	slog.Debug("[robot] ensemble-suggest result",
		"question", question,
		"suggestion_count", len(result.Suggestions),
		"top_pick", func() string {
			if result.TopPick != nil {
				return result.TopPick.PresetName
			}
			return "none"
		}(),
	)

	if len(result.Suggestions) == 0 {
		output.RobotResponse = NewRobotResponse(true) // Still success, just no match
		output.AgentHints = &EnsembleSuggestAgentHints{
			Summary: "No preset matched the question. " + result.NoMatchReason,
			SuggestedActions: []RobotAction{
				{
					Action:   "try-different-question",
					Target:   "ensemble-suggest",
					Reason:   "refine the question with more specific keywords",
					Priority: 1,
				},
				{
					Action:   "list-presets",
					Target:   "ensemble",
					Reason:   "view available presets and their descriptions",
					Priority: 2,
				},
			},
		}
		return output, nil
	}

	// Convert suggestions to output format
	for _, s := range result.Suggestions {
		preset := s.Preset
		if preset == nil {
			preset = engine.GetPreset(s.PresetName)
		}

		suggestion := EnsembleSuggestion{
			PresetName: s.PresetName,
			Score:      s.Score,
			Reasons:    s.Reasons,
		}
		// Ensure Reasons is never nil (JSON serializes nil as null)
		if suggestion.Reasons == nil {
			suggestion.Reasons = []string{}
		}

		if preset != nil {
			suggestion.DisplayName = preset.DisplayName
			suggestion.Description = preset.Description
			suggestion.ModeCount = len(preset.Modes)
			suggestion.Tags = preset.Tags
			// Ensure Tags is never nil
			if suggestion.Tags == nil {
				suggestion.Tags = []string{}
			}
			suggestion.SpawnCmd = "herdctl ensemble " + preset.Name + " \"" + escapeQuotes(question) + "\""
		}

		output.Suggestions = append(output.Suggestions, suggestion)
	}

	// Set top pick
	if len(output.Suggestions) > 0 {
		topPick := output.Suggestions[0]
		output.TopPick = &topPick
	}

	// Build agent hints
	output.AgentHints = buildEnsembleSuggestHints(*output)

	return output, nil
}

// PrintEnsembleSuggest outputs ensemble preset suggestions for a question.
func PrintEnsembleSuggest(question string, idOnly bool) error {
	output, err := GetEnsembleSuggest(question)
	if err != nil {
		return err
	}

	// If idOnly, simplify output
	if idOnly {
		return outputIDOnly(*output)
	}

	return outputJSON(output)
}

// ErrMissingQuestion is returned when no question is provided.
var ErrMissingQuestion = fmt.Errorf("question is required")

func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, "\"", "\\\"")
}

func buildEnsembleSuggestHints(output EnsembleSuggestOutput) *EnsembleSuggestAgentHints {
	if output.TopPick == nil {
		return nil
	}

	hints := &EnsembleSuggestAgentHints{
		Summary:      "Best match: " + output.TopPick.DisplayName,
		SpawnCommand: output.TopPick.SpawnCmd,
		SuggestedActions: []RobotAction{
			{
				Action:   "spawn-ensemble",
				Target:   output.TopPick.PresetName,
				Reason:   "run the suggested ensemble with the question",
				Priority: 1,
				Details:  output.TopPick.SpawnCmd,
			},
		},
	}

	// Add alternative suggestions if available
	if len(output.Suggestions) > 1 {
		hints.SuggestedActions = append(hints.SuggestedActions, RobotAction{
			Action:   "consider-alternatives",
			Target:   "suggestions",
			Reason:   output.Suggestions[1].PresetName + " also matches",
			Priority: 2,
		})
	}

	return hints
}

// EnsembleSuggestIDOnlyOutput is the simplified output when --id-only is used.
type EnsembleSuggestIDOnlyOutput struct {
	RobotResponse
	PresetName string `json:"preset_name"`
	SpawnCmd   string `json:"spawn_cmd"`
}

func outputIDOnly(full EnsembleSuggestOutput) error {
	if full.TopPick == nil {
		simple := EnsembleSuggestIDOnlyOutput{
			RobotResponse: full.RobotResponse,
			PresetName:    "",
			SpawnCmd:      "",
		}
		return outputJSON(simple)
	}

	simple := EnsembleSuggestIDOnlyOutput{
		RobotResponse: full.RobotResponse,
		PresetName:    full.TopPick.PresetName,
		SpawnCmd:      full.TopPick.SpawnCmd,
	}
	return outputJSON(simple)
}

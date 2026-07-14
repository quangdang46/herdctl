package cli

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Dicklesworthstone/ntm/internal/ensemble"
	"github.com/Dicklesworthstone/ntm/internal/output"
)

type ensembleSuggestOutput struct {
	Question    string                  `json:"question" yaml:"question"`
	TopPick     *ensembleSuggestionRow  `json:"top_pick,omitempty" yaml:"top_pick,omitempty"`
	Suggestions []ensembleSuggestionRow `json:"suggestions" yaml:"suggestions"`
	SpawnCmd    string                  `json:"spawn_cmd,omitempty" yaml:"spawn_cmd,omitempty"`
}

type ensembleSuggestionRow struct {
	Name        string   `json:"name" yaml:"name"`
	DisplayName string   `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Score       float64  `json:"score" yaml:"score"`
	Reasons     []string `json:"reasons,omitempty" yaml:"reasons,omitempty"`
	ModeCount   int      `json:"mode_count,omitempty" yaml:"mode_count,omitempty"`
	Tags        []string `json:"tags,omitempty" yaml:"tags,omitempty"`
}

func newEnsembleSuggestCmd() *cobra.Command {
	var (
		format string
		idOnly bool
	)

	cmd := &cobra.Command{
		Use:   "suggest <question>",
		Short: "Recommend best ensemble preset for a question",
		Long: `Analyze a question and suggest the best ensemble preset to use.

The suggestion engine uses keyword matching and semantic analysis to recommend
the most appropriate preset for your question.

Examples:
  herdctl ensemble suggest "What security vulnerabilities exist in this codebase?"
  herdctl ensemble suggest "Why did the login flow fail yesterday?"
  herdctl ensemble suggest "What features should we add next?" --json
  herdctl ensemble suggest "Review the architecture" --id-only`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			question := strings.TrimSpace(args[0])
			if question == "" {
				return fmt.Errorf("question cannot be empty")
			}

			return runEnsembleSuggest(cmd.OutOrStdout(), question, format, idOnly)
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "table", "Output format: table, json, yaml")
	cmd.Flags().BoolVar(&idOnly, "id-only", false, "Output only the top preset name (for piping)")

	return cmd
}

func runEnsembleSuggest(w io.Writer, question, format string, idOnly bool) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "table"
	}
	if jsonOutput {
		format = "json"
	}

	slog.Debug("ensemble suggest",
		"question", question,
		"format", format,
		"id_only", idOnly,
	)

	engine := ensemble.GlobalSuggestionEngine()
	result := engine.Suggest(question)

	slog.Debug("ensemble suggest result",
		"question_len", len(question),
		"suggestion_count", len(result.Suggestions),
		"top_pick", func() string {
			if result.TopPick != nil {
				return result.TopPick.PresetName
			}
			return "none"
		}(),
	)

	// Handle id-only mode
	if idOnly {
		if result.TopPick != nil {
			fmt.Fprintln(w, result.TopPick.PresetName)
		}
		return nil
	}

	// Build output
	out := ensembleSuggestOutput{
		Question:    question,
		Suggestions: make([]ensembleSuggestionRow, 0, len(result.Suggestions)),
	}

	for _, s := range result.Suggestions {
		row := ensembleSuggestionRow{
			Name:    s.PresetName,
			Score:   s.Score,
			Reasons: s.Reasons,
		}
		// Ensure Reasons is never nil (JSON serializes nil as null)
		if row.Reasons == nil {
			row.Reasons = []string{}
		}

		if s.Preset != nil {
			row.DisplayName = s.Preset.DisplayName
			row.Description = s.Preset.Description
			row.ModeCount = len(s.Preset.Modes)
			row.Tags = s.Preset.Tags
			// Ensure Tags is never nil
			if row.Tags == nil {
				row.Tags = []string{}
			}
		}

		out.Suggestions = append(out.Suggestions, row)
	}

	if len(out.Suggestions) > 0 {
		topPick := out.Suggestions[0]
		out.TopPick = &topPick
		out.SpawnCmd = "herdctl ensemble " + topPick.Name + " \"" + escapeShellQuotes(question) + "\""
	}

	return renderEnsembleSuggest(w, out, format, result.NoMatchReason)
}

func escapeShellQuotes(s string) string {
	return strings.ReplaceAll(s, "\"", "\\\"")
}

func renderEnsembleSuggest(w io.Writer, payload ensembleSuggestOutput, format, noMatchReason string) error {
	switch format {
	case "json":
		return output.WriteJSON(w, payload, true)
	case "yaml", "yml":
		data, err := yaml.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal yaml: %w", err)
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if len(data) == 0 || data[len(data)-1] != '\n' {
			_, err = w.Write([]byte("\n"))
			return err
		}
		return nil
	case "table", "text":
		if len(payload.Suggestions) == 0 {
			fmt.Fprintf(w, "No preset matched the question.\n")
			if noMatchReason != "" {
				fmt.Fprintf(w, "Reason: %s\n", noMatchReason)
			}
			fmt.Fprintln(w, "\nAvailable presets:")
			for _, name := range ensemble.EnsembleNames() {
				preset := ensemble.GetEmbeddedEnsemble(name)
				if preset != nil {
					fmt.Fprintf(w, "  %-20s %s\n", preset.Name, preset.Description)
				}
			}
			return nil
		}

		fmt.Fprintf(w, "Question: %s\n\n", payload.Question)

		if payload.TopPick != nil {
			fmt.Fprintf(w, "Recommended: %s\n", payload.TopPick.DisplayName)
			if payload.TopPick.Description != "" {
				fmt.Fprintf(w, "  %s\n", payload.TopPick.Description)
			}
			if len(payload.TopPick.Reasons) > 0 {
				fmt.Fprintf(w, "  Matched: %s\n", strings.Join(payload.TopPick.Reasons, ", "))
			}
			fmt.Fprintf(w, "  Modes: %d\n", payload.TopPick.ModeCount)
			if payload.SpawnCmd != "" {
				fmt.Fprintf(w, "\nSpawn command:\n  %s\n", payload.SpawnCmd)
			}
		}

		if len(payload.Suggestions) > 1 {
			fmt.Fprintln(w, "\nAlternatives:")
			table := output.NewTable(w, "RANK", "PRESET", "SCORE", "DESCRIPTION")
			for i, row := range payload.Suggestions {
				if i == 0 {
					continue // Skip top pick, already shown
				}
				table.AddRow(
					fmt.Sprintf("%d", i+1),
					row.Name,
					fmt.Sprintf("%.2f", row.Score),
					truncate(row.Description, 50),
				)
			}
			table.Render()
		}

		return nil
	default:
		return fmt.Errorf("invalid format %q (expected table, json, yaml)", format)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

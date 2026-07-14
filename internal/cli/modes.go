package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Dicklesworthstone/ntm/internal/ensemble"
	"github.com/Dicklesworthstone/ntm/internal/output"
)

type modesListOutput struct {
	GeneratedAt time.Time      `json:"generated_at" yaml:"generated_at"`
	Modes       []modesListRow `json:"modes" yaml:"modes"`
	Count       int            `json:"count" yaml:"count"`
	Filter      string         `json:"filter,omitempty" yaml:"filter,omitempty"`
}

type modesListRow struct {
	ID        string `json:"id" yaml:"id"`
	Code      string `json:"code" yaml:"code"`
	Name      string `json:"name" yaml:"name"`
	Category  string `json:"category" yaml:"category"`
	Tier      string `json:"tier" yaml:"tier"`
	ShortDesc string `json:"short_desc" yaml:"short_desc"`
}

type modesExplainOutput struct {
	GeneratedAt time.Time          `json:"generated_at" yaml:"generated_at"`
	Card        *ensemble.ModeCard `json:"card" yaml:"card"`
}

func newModesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "modes",
		Short: "Browse and explore reasoning modes",
		Long: `Browse, search, and get detailed explanations of reasoning modes.

Reasoning modes are specialized analytical perspectives that can be used
in ensemble runs to approach problems from different angles.

Use 'herdctl modes list' to see all available modes.
Use 'herdctl modes explain <mode>' to get detailed information about a mode.`,
		Example: `  ntm modes list
  ntm modes list --category Formal
  ntm modes list --tier core
  ntm modes explain deductive
  ntm modes explain A1 --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newModesListCmd())
	cmd.AddCommand(newModesExplainCmd())

	return cmd
}

func newModesListCmd() *cobra.Command {
	var (
		format   string
		category string
		tier     string
		all      bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available reasoning modes",
		Long: `List all available reasoning modes with their codes and descriptions.

By default, shows only core-tier modes. Use --all to show all tiers.
Filter by category or tier to narrow results.`,
		Example: `  ntm modes list
  ntm modes list --all
  ntm modes list --category Formal
  ntm modes list --tier advanced
  ntm modes list --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModesList(cmd.OutOrStdout(), format, category, tier, all)
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "text", "Output format: text, json, yaml")
	cmd.Flags().StringVarP(&category, "category", "c", "", "Filter by category (e.g., Formal, Causal)")
	cmd.Flags().StringVarP(&tier, "tier", "t", "", "Filter by tier (core, advanced, experimental)")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Show all tiers (default: core only)")

	return cmd
}

func runModesList(w io.Writer, format, category, tier string, all bool) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "text"
	}
	if jsonOutput {
		format = "json"
	}

	catalog, err := ensemble.LoadModeCatalog()
	if err != nil {
		return fmt.Errorf("load mode catalog: %w", err)
	}

	modes := catalog.ListModes()

	// Filter by tier
	if !all && tier == "" {
		tier = "core"
	}
	if tier != "" {
		var filtered []ensemble.ReasoningMode
		for _, m := range modes {
			if strings.EqualFold(string(m.Tier), tier) {
				filtered = append(filtered, m)
			}
		}
		modes = filtered
	}

	// Filter by category
	if category != "" {
		var filtered []ensemble.ReasoningMode
		for _, m := range modes {
			if strings.EqualFold(string(m.Category), category) {
				filtered = append(filtered, m)
			}
		}
		modes = filtered
	}

	rows := make([]modesListRow, 0, len(modes))
	for _, m := range modes {
		rows = append(rows, modesListRow{
			ID:        m.ID,
			Code:      m.Code,
			Name:      m.Name,
			Category:  string(m.Category),
			Tier:      string(m.Tier),
			ShortDesc: m.ShortDesc,
		})
	}

	filterDesc := ""
	if category != "" {
		filterDesc = "category=" + category
	}
	if tier != "" && tier != "core" {
		if filterDesc != "" {
			filterDesc += ", "
		}
		filterDesc += "tier=" + tier
	}

	result := modesListOutput{
		GeneratedAt: output.Timestamp(),
		Modes:       rows,
		Count:       len(rows),
		Filter:      filterDesc,
	}

	slog.Default().Info("modes list",
		"count", len(rows),
		"category", category,
		"tier", tier,
		"all", all,
	)

	return renderModesList(w, result, format)
}

func renderModesList(w io.Writer, payload modesListOutput, format string) error {
	switch format {
	case "json":
		return output.WriteJSON(w, payload, true)
	case "yaml":
		data, err := yaml.Marshal(payload)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	default:
		if len(payload.Modes) == 0 {
			fmt.Fprintf(w, "No modes found matching criteria\n")
			return nil
		}

		table := output.NewTable(w, "CODE", "ID", "NAME", "CATEGORY", "TIER")
		for _, row := range payload.Modes {
			table.AddRow(row.Code, row.ID, row.Name, row.Category, row.Tier)
		}
		table.Render()
		fmt.Fprintf(w, "\n%d modes total\n", payload.Count)
		return nil
	}
}

func newModesExplainCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "explain <mode>",
		Short: "Show detailed explanation of a reasoning mode",
		Long: `Display a detailed explanation card for a reasoning mode.

The card includes:
  - Full description and what makes the mode unique
  - Best use cases and example prompts
  - Common pitfalls to avoid
  - Estimated token cost
  - Complementary modes that work well together

You can reference a mode by ID (e.g., "deductive") or code (e.g., "A1").`,
		Example: `  ntm modes explain deductive
  ntm modes explain A1
  ntm modes explain bayesian --format json
  ntm modes explain root-cause --format yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			modeRef := args[0]
			return runModesExplain(cmd.OutOrStdout(), modeRef, format)
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "text", "Output format: text, json, yaml")

	return cmd
}

func runModesExplain(w io.Writer, modeRef, format string) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "text"
	}
	if jsonOutput {
		format = "json"
	}

	catalog, err := ensemble.LoadModeCatalog()
	if err != nil {
		return fmt.Errorf("load mode catalog: %w", err)
	}

	card, err := catalog.GetModeCard(modeRef)
	if err != nil {
		return err
	}

	slog.Default().Info("mode explain",
		"mode_ref", modeRef,
		"mode_id", card.ModeID,
	)

	result := modesExplainOutput{
		GeneratedAt: output.Timestamp(),
		Card:        card,
	}

	return renderModesExplain(w, result, format)
}

func renderModesExplain(w io.Writer, payload modesExplainOutput, format string) error {
	switch format {
	case "json":
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	case "yaml":
		data, err := yaml.Marshal(payload)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	default:
		fmt.Fprint(w, ensemble.FormatCard(payload.Card))
		return nil
	}
}

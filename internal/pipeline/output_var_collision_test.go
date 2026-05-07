package pipeline

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestValidateParallelDuplicateOutputVarWarns(t *testing.T) {
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "parallel-output-var-warning",
		Steps: []Step{
			{
				ID: "fanout",
				Parallel: ParallelSpec{Steps: []Step{
					{ID: "left", Prompt: "left", OutputVar: "shared"},
					{ID: "right", Prompt: "right", OutputVar: "shared"},
				}},
			},
		},
	}

	result := Validate(workflow)
	if !result.Valid {
		t.Fatalf("Validate() errors = %+v", result.Errors)
	}
	assertValidationWarning(t, result, "share output_var")
	assertValidationWarning(t, result, "output_var_mode=aggregate")
}

func TestValidateOutputVarModeRejectsUnknownValue(t *testing.T) {
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "bad-output-var-mode",
		Steps: []Step{
			{ID: "bad", Prompt: "bad", OutputVarMode: OutputVarMode("newest")},
		},
	}

	result := Validate(workflow)
	if result.Valid {
		t.Fatal("Validate() succeeded, want invalid output_var_mode error")
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0].Message, "invalid output_var_mode") {
		t.Fatalf("Validate() errors = %+v, want invalid output_var_mode", result.Errors)
	}
}

func TestValidateParallelForeachLastModeWarns(t *testing.T) {
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "foreach-last-warning",
		Steps: []Step{
			{
				ID:            "foreach",
				OutputVar:     "collected",
				OutputVarMode: OutputVarModeLast,
				Foreach: &ForeachConfig{
					Items:    `["a","b"]`,
					Parallel: true,
					Steps: []Step{
						{ID: "body", Prompt: "handle ${item}"},
					},
				},
			},
		},
	}

	result := Validate(workflow)
	if !result.Valid {
		t.Fatalf("Validate() errors = %+v", result.Errors)
	}
	assertValidationWarning(t, result, "parallel foreach")
	assertValidationWarning(t, result, "non-deterministic")
}

func TestExecuteParallelDuplicateOutputVarAggregatesInDeclarationOrder(t *testing.T) {
	e, workflow, step := newOutputVarParallelExecutor(OutputVarModeAggregate)

	result := e.executeParallel(context.Background(), step, workflow)
	if result.Status != StatusCompleted {
		t.Fatalf("executeParallel() status = %s, want completed; error=%+v", result.Status, result.Error)
	}

	got, ok := e.state.Variables["shared"].([]string)
	if !ok {
		t.Fatalf("shared variable type = %T, want []string (%#v)", e.state.Variables["shared"], e.state.Variables["shared"])
	}
	if len(got) != 2 {
		t.Fatalf("len(shared) = %d, want 2: %#v", len(got), got)
	}
	if !strings.Contains(got[0], "[left]") || !strings.Contains(got[1], "[right]") {
		t.Fatalf("shared outputs = %#v, want declaration order left/right", got)
	}
}

func TestExecuteParallelDuplicateOutputVarCollectsByStepID(t *testing.T) {
	e, workflow, step := newOutputVarParallelExecutor(OutputVarModeCollect)

	result := e.executeParallel(context.Background(), step, workflow)
	if result.Status != StatusCompleted {
		t.Fatalf("executeParallel() status = %s, want completed; error=%+v", result.Status, result.Error)
	}

	got, ok := e.state.Variables["shared"].(map[string]string)
	if !ok {
		t.Fatalf("shared variable type = %T, want map[string]string (%#v)", e.state.Variables["shared"], e.state.Variables["shared"])
	}
	if !strings.Contains(got["left"], "[left]") || !strings.Contains(got["right"], "[right]") {
		t.Fatalf("shared outputs = %#v, want entries keyed by step id", got)
	}

	// Regression: the documented nested-variable mechanism must support keyed
	// access into collect-mode outputs (bd-rdzch). Previously navigateNested
	// rejected map[string]string with "cannot access field on type".
	sub := NewSubstitutor(e.state, "", workflow.Name)
	resolved, err := sub.Substitute("${vars.shared.left}")
	if err != nil {
		t.Fatalf("Substitute(${vars.shared.left}) error = %v", err)
	}
	if !strings.Contains(resolved, "[left]") {
		t.Fatalf("Substitute(${vars.shared.left}) = %q, want substring [left]", resolved)
	}

	if _, err := sub.Substitute("${vars.shared.missing}"); err == nil {
		t.Fatalf("Substitute(${vars.shared.missing}) succeeded, want missing-key error")
	}
}

func TestExecuteParallelDuplicateOutputVarLastModeLogsDebug(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	e, workflow, step := newOutputVarParallelExecutor(OutputVarModeLast)
	result := e.executeParallel(context.Background(), step, workflow)
	if result.Status != StatusCompleted {
		t.Fatalf("executeParallel() status = %s, want completed; error=%+v", result.Status, result.Error)
	}
	if _, ok := e.state.Variables["shared"].(string); !ok {
		t.Fatalf("shared variable type = %T, want string", e.state.Variables["shared"])
	}
	if !strings.Contains(buf.String(), "parallel output_var last mode is non-deterministic") {
		t.Fatalf("debug log missing last-mode warning; logs=%s", buf.String())
	}
}

func newOutputVarParallelExecutor(mode OutputVarMode) (*Executor, *Workflow, *Step) {
	parallelSteps := []Step{
		{ID: "left", Prompt: "left", OutputVar: "shared", OutputVarMode: mode},
		{ID: "right", Prompt: "right", OutputVar: "shared"},
	}
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "parallel-output-vars",
		Settings:      DefaultWorkflowSettings(),
		Steps: []Step{
			{ID: "fanout", Parallel: ParallelSpec{Steps: parallelSteps}},
		},
	}
	cfg := DefaultExecutorConfig("test")
	cfg.DryRun = true
	e := NewExecutor(cfg)
	e.graph = NewDependencyGraph(workflow)
	e.state = &ExecutionState{
		RunID:      "test-run",
		WorkflowID: workflow.Name,
		Status:     StatusRunning,
		StartedAt:  time.Now(),
		Steps:      make(map[string]StepResult),
		Variables:  make(map[string]interface{}),
	}
	return e, workflow, &workflow.Steps[0]
}

func assertValidationWarning(t *testing.T, result ValidationResult, want string) {
	t.Helper()
	for _, warning := range result.Warnings {
		if strings.Contains(warning.Message, want) || strings.Contains(warning.Hint, want) {
			return
		}
	}
	t.Fatalf("missing validation warning containing %q; warnings=%+v", want, result.Warnings)
}

package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestResumeContinueRestartsFirstIncompleteLoopIteration(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "iterations.log")
	markerPath := filepath.Join(tmpDir, "failed-once")

	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "resume-loop",
		Settings:      DefaultWorkflowSettings(),
		Steps: []Step{
			{
				ID: "fanout",
				Loop: &LoopConfig{
					Items: "${vars.items}",
					As:    "row",
					Steps: []Step{
						{
							ID: "work",
							Command: "printf '%s\n' '${loop.index}' >> " + strconv.Quote(logPath) +
								"; if [ '${loop.index}' = '1' ] && [ ! -f " + strconv.Quote(markerPath) + " ]; then touch " + strconv.Quote(markerPath) + "; exit 7; fi",
						},
					},
				},
			},
		},
	}

	cfg := DefaultExecutorConfig("resume-loop-session")
	cfg.ProjectDir = tmpDir
	cfg.DefaultTimeout = 2 * time.Second
	first := NewExecutor(cfg)
	prior, err := first.Run(context.Background(), workflow, map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}, nil)
	if err == nil {
		t.Fatal("first Run() error = nil, want first pass to fail at iteration 1")
	}
	if prior.Status != StatusFailed {
		t.Fatalf("prior.Status = %s, want failed", prior.Status)
	}
	if got := prior.ForeachState["fanout"].CurrentIteration; got != 1 {
		t.Fatalf("prior current iteration = %d, want 1", got)
	}

	second := NewExecutor(cfg)
	final, err := second.ResumeWithOptions(context.Background(), workflow, prior, ResumeOptions{
		Mode:           ResumeModeContinue,
		KeepState:      true,
		OnRosterChange: ResumeRosterAbort,
	}, nil)
	if err != nil {
		t.Fatalf("ResumeWithOptions() error: %v", err)
	}
	if final.Status != StatusCompleted {
		t.Fatalf("final.Status = %s, want completed", final.Status)
	}
	if got := final.ForeachState["fanout"].CurrentIteration; got != 3 {
		t.Fatalf("final current iteration = %d, want 3", got)
	}
	if got := final.ForeachState["fanout"].CompletedIterationIDs; !reflect.DeepEqual(got, []string{"fanout_iter0", "fanout_iter1", "fanout_iter2"}) {
		t.Fatalf("completed iterations = %#v", got)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read iteration log: %v", err)
	}
	if got, want := strings.TrimSpace(string(data)), "0\n1\n1\n2"; got != want {
		t.Fatalf("iteration log = %q, want %q", got, want)
	}
}

func TestResumeRestartFailedRerunsFailedStepOnly(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "resume.log")
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "resume-failed",
		Settings:      DefaultWorkflowSettings(),
		Steps: []Step{
			{ID: "done", Command: "printf done >> " + strconv.Quote(logPath)},
			{ID: "flaky", Command: "printf flaky >> " + strconv.Quote(logPath), DependsOn: []string{"done"}},
		},
	}
	prior := &ExecutionState{
		RunID:      "restart-failed",
		WorkflowID: workflow.Name,
		Session:    "resume-session",
		Status:     StatusFailed,
		StartedAt:  time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 5, 7, 10, 1, 0, 0, time.UTC),
		Steps: map[string]StepResult{
			"done":  {StepID: "done", Status: StatusCompleted, Output: "prior"},
			"flaky": {StepID: "flaky", Status: StatusFailed, Error: &StepError{Type: "command", Message: "exit 7", Timestamp: time.Date(2026, 5, 7, 10, 1, 0, 0, time.UTC)}},
		},
		Variables: map[string]interface{}{},
	}

	cfg := DefaultExecutorConfig("resume-session")
	cfg.ProjectDir = tmpDir
	cfg.DefaultTimeout = 2 * time.Second
	executor := NewExecutor(cfg)
	final, err := executor.ResumeWithOptions(context.Background(), workflow, prior, ResumeOptions{
		Mode:           ResumeModeRestartFailed,
		KeepState:      true,
		OnRosterChange: ResumeRosterAbort,
	}, nil)
	if err != nil {
		t.Fatalf("ResumeWithOptions() error: %v", err)
	}
	if final.Status != StatusCompleted {
		t.Fatalf("final.Status = %s, want completed", final.Status)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if got := string(data); got != "flaky" {
		t.Fatalf("log = %q, want only failed step to rerun", got)
	}
}

func TestResumeRestartFailedPreservesStateWhenKeepStateOmitted(t *testing.T) {
	// bd-uyjdn: callers that pass a partial ResumeOptions (e.g. only Mode)
	// without explicitly setting KeepState=true should still preserve
	// completed step state. Previously normalizeResumeOptions left KeepState
	// at the Go zero-value (false) for any non-zero options struct, which
	// caused applyResumeOptions to call resetResumeState and silently rerun
	// completed dependencies.
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "resume.log")
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "resume-failed-partial-opts",
		Settings:      DefaultWorkflowSettings(),
		Steps: []Step{
			{ID: "done", Command: "printf done >> " + strconv.Quote(logPath)},
			{ID: "flaky", Command: "printf flaky >> " + strconv.Quote(logPath), DependsOn: []string{"done"}},
		},
	}
	prior := &ExecutionState{
		RunID:      "restart-failed-partial",
		WorkflowID: workflow.Name,
		Session:    "resume-session",
		Status:     StatusFailed,
		StartedAt:  time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 5, 7, 10, 1, 0, 0, time.UTC),
		Steps: map[string]StepResult{
			"done":  {StepID: "done", Status: StatusCompleted, Output: "prior"},
			"flaky": {StepID: "flaky", Status: StatusFailed, Error: &StepError{Type: "command", Message: "exit 7", Timestamp: time.Date(2026, 5, 7, 10, 1, 0, 0, time.UTC)}},
		},
		Variables: map[string]interface{}{},
	}

	cfg := DefaultExecutorConfig("resume-session")
	cfg.ProjectDir = tmpDir
	cfg.DefaultTimeout = 2 * time.Second
	executor := NewExecutor(cfg)
	final, err := executor.ResumeWithOptions(context.Background(), workflow, prior, ResumeOptions{
		Mode: ResumeModeRestartFailed,
	}, nil)
	if err != nil {
		t.Fatalf("ResumeWithOptions() error: %v", err)
	}
	if final.Status != StatusCompleted {
		t.Fatalf("final.Status = %s, want completed", final.Status)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if got := string(data); got != "flaky" {
		t.Fatalf("log = %q, want only failed step to rerun (completed dependency 'done' must not re-execute)", got)
	}
}

func TestResumeResetOptInClearsCompletedSteps(t *testing.T) {
	// bd-uyjdn: explicit Reset=true reproduces the legacy KeepState=false
	// behavior — prior step state is cleared and the workflow runs from the
	// beginning.
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "reset.log")
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "resume-reset-optin",
		Settings:      DefaultWorkflowSettings(),
		Steps: []Step{
			{ID: "first", Command: "printf first >> " + strconv.Quote(logPath)},
		},
	}
	prior := &ExecutionState{
		RunID:      "reset-optin",
		WorkflowID: workflow.Name,
		Session:    "reset-session",
		Status:     StatusFailed,
		StartedAt:  time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 5, 7, 10, 1, 0, 0, time.UTC),
		Steps: map[string]StepResult{
			"first": {StepID: "first", Status: StatusCompleted, Output: "prior"},
		},
		Variables: map[string]interface{}{},
	}

	cfg := DefaultExecutorConfig("reset-session")
	cfg.ProjectDir = tmpDir
	cfg.DefaultTimeout = 2 * time.Second
	executor := NewExecutor(cfg)
	final, err := executor.ResumeWithOptions(context.Background(), workflow, prior, ResumeOptions{
		Mode:  ResumeModeContinue,
		Reset: true,
	}, nil)
	if err != nil {
		t.Fatalf("ResumeWithOptions() error: %v", err)
	}
	if final.Status != StatusCompleted {
		t.Fatalf("final.Status = %s, want completed", final.Status)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if got := string(data); got != "first" {
		t.Fatalf("log = %q, want completed step to rerun under Reset=true", got)
	}
}

func TestNormalizeResumeOptionsDefaultsKeepStateOnNonZero(t *testing.T) {
	// bd-uyjdn: every non-zero ResumeOptions normalizes to KeepState=true
	// unless Reset=true is set explicitly.
	tests := []struct {
		name   string
		opts   ResumeOptions
		want   bool
		reset  bool
		errMsg string
	}{
		{name: "mode only", opts: ResumeOptions{Mode: ResumeModeRestartFailed}, want: true},
		{name: "max age only", opts: ResumeOptions{MaxResumeAge: time.Hour}, want: true},
		{name: "explicit reset", opts: ResumeOptions{Mode: ResumeModeContinue, Reset: true}, want: false, reset: true},
		{name: "legacy KeepState=true survives", opts: ResumeOptions{Mode: ResumeModeContinue, KeepState: true}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeResumeOptions(tt.opts)
			if err != nil {
				t.Fatalf("normalizeResumeOptions() error: %v", err)
			}
			if got.KeepState != tt.want {
				t.Fatalf("KeepState = %v, want %v", got.KeepState, tt.want)
			}
			if got.Reset != tt.reset {
				t.Fatalf("Reset = %v, want %v", got.Reset, tt.reset)
			}
		})
	}
}

func TestResumeRosterChangeAbort(t *testing.T) {
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "resume-roster",
		Settings:      DefaultWorkflowSettings(),
		Steps:         []Step{{ID: "step", Command: "true"}},
	}
	prior := &ExecutionState{
		RunID:      "roster-change",
		WorkflowID: workflow.Name,
		Session:    "old-session",
		Status:     StatusRunning,
		Steps:      map[string]StepResult{},
		Variables:  map[string]interface{}{},
	}
	executor := NewExecutor(DefaultExecutorConfig("new-session"))
	final, err := executor.ResumeWithOptions(context.Background(), workflow, prior, ResumeOptions{
		Mode:           ResumeModeContinue,
		KeepState:      true,
		OnRosterChange: ResumeRosterAbort,
	}, nil)
	if err == nil {
		t.Fatal("ResumeWithOptions() error = nil, want roster-change abort")
	}
	if final == nil || final.Status != StatusFailed {
		t.Fatalf("final status = %#v, want failed state", final)
	}
	if !strings.Contains(err.Error(), "old-session") || !strings.Contains(err.Error(), "new-session") {
		t.Fatalf("error = %q, want both sessions named", err.Error())
	}
}

func TestResumeRejectsStaleState(t *testing.T) {
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "resume-stale",
		Settings:      DefaultWorkflowSettings(),
		Steps:         []Step{{ID: "step", Command: "true"}},
	}
	prior := &ExecutionState{
		RunID:            "stale",
		WorkflowID:       workflow.Name,
		Session:          "session",
		Status:           StatusRunning,
		UpdatedAt:        time.Now().Add(-48 * time.Hour),
		LastCheckpointAt: time.Now().Add(-48 * time.Hour),
		Steps:            map[string]StepResult{},
		Variables:        map[string]interface{}{},
	}
	executor := NewExecutor(DefaultExecutorConfig("session"))
	_, err := executor.ResumeWithOptions(context.Background(), workflow, prior, ResumeOptions{
		Mode:           ResumeModeContinue,
		KeepState:      true,
		MaxResumeAge:   time.Hour,
		OnRosterChange: ResumeRosterAbort,
	}, nil)
	if err == nil {
		t.Fatal("ResumeWithOptions() error = nil, want stale-state error")
	}
	if !strings.Contains(err.Error(), "older than MaxResumeAge") {
		t.Fatalf("error = %q, want stale-state message", err.Error())
	}
}

func TestResumeRejectsLegacyStateWithoutCheckpoint(t *testing.T) {
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "resume-legacy-stale",
		Settings:      DefaultWorkflowSettings(),
		Steps:         []Step{{ID: "step", Command: "true"}},
	}
	stale := time.Now().Add(-72 * time.Hour)
	prior := &ExecutionState{
		RunID:      "legacy-stale",
		WorkflowID: workflow.Name,
		Session:    "session",
		Status:     StatusRunning,
		StartedAt:  stale.Add(-time.Hour),
		UpdatedAt:  stale,
		Steps: map[string]StepResult{
			"step": {
				StepID:     "step",
				Status:     StatusCompleted,
				StartedAt:  stale.Add(-30 * time.Minute),
				FinishedAt: stale,
			},
		},
		Variables: map[string]interface{}{},
	}

	executor := NewExecutor(DefaultExecutorConfig("session"))
	_, err := executor.ResumeWithOptions(context.Background(), workflow, prior, ResumeOptions{
		Mode:           ResumeModeContinue,
		KeepState:      true,
		MaxResumeAge:   2 * time.Hour,
		OnRosterChange: ResumeRosterAbort,
	}, nil)
	if err == nil {
		t.Fatal("ResumeWithOptions() error = nil, want stale-state error for legacy checkpoint-less state")
	}
	if !strings.Contains(err.Error(), "older than MaxResumeAge") {
		t.Fatalf("error = %q, want stale-state message", err.Error())
	}
}

func TestExecutionStateResumeMetadataJSONRoundTrip(t *testing.T) {
	stamp := time.Date(2026, 5, 7, 12, 30, 0, 0, time.UTC)
	original := ExecutionState{
		RunID:            "roundtrip",
		WorkflowID:       "workflow",
		Session:          "session",
		Status:           StatusRunning,
		StartedAt:        stamp.Add(-time.Minute),
		UpdatedAt:        stamp,
		LastCheckpointAt: stamp,
		Steps: map[string]StepResult{
			"step": {StepID: "step", Status: StatusCompleted, Output: "ok", StartedAt: stamp.Add(-time.Second), FinishedAt: stamp},
		},
		Variables: map[string]interface{}{"input": "value"},
		ForeachState: map[string]ForeachIterationState{
			"fanout": {
				StepID:                "fanout",
				CurrentIteration:      2,
				Total:                 4,
				CompletedIterationIDs: []string{"fanout_iter0", "fanout_iter1"},
				StartedAt:             stamp.Add(-time.Minute),
				UpdatedAt:             stamp,
			},
		},
		ParallelState: map[string]ParallelGroupState{
			"group": {
				StepID:           "group",
				Total:            3,
				CompletedStepIDs: []string{"a"},
				FailedStepIDs:    []string{"b"},
				InFlightStepIDs:  []string{"c"},
				StartedAt:        stamp.Add(-time.Minute),
				UpdatedAt:        stamp,
			},
		},
		ScopeStack: []ScopeFrame{
			{Kind: StepKindLoop, Name: "row", Variables: map[string]interface{}{"loop.item": "a", "loop.first": true}},
		},
		InFlightSteps: map[string]InFlightStepState{
			"group.c": {StepID: "group.c", Kind: "parallel_step", StartedAt: stamp, Iteration: 2, Output: "partial"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}
	var decoded ExecutionState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("round-trip mismatch:\n got: %#v\nwant: %#v", decoded, original)
	}
}

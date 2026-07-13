// Package robot provides JSON output functions for AI agent integration.
package robot

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Dicklesworthstone/ntm/internal/session"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// SaveOptions configures the robot-save operation.
type SaveOptions struct {
	Session    string // Session name to save
	OutputFile string // Optional custom output file path
}

// RestoreOptions configures the robot-restore operation.
type RestoreOptions struct {
	SavedName string // Name of saved state to restore
	DryRun    bool   // Preview without executing
}

// SaveResult represents the JSON output for robot-save.
type SaveResult struct {
	Success  bool                  `json:"success"`
	Session  string                `json:"session"`
	SavedAs  string                `json:"saved_as"`
	FilePath string                `json:"file_path"`
	State    *session.SessionState `json:"state,omitempty"`
	Error    string                `json:"error,omitempty"`
}

// RestoreResult represents the JSON output for robot-restore.
type RestoreResult struct {
	Success    bool                  `json:"success"`
	SavedName  string                `json:"saved_name"`
	RestoredAs string                `json:"restored_as,omitempty"`
	DryRun     bool                  `json:"dry_run"`
	State      *session.SessionState `json:"state,omitempty"`
	Preview    *RestorePreview       `json:"preview,omitempty"`
	Error      string                `json:"error,omitempty"`
}

// RestorePreview describes what would happen during restore.
type RestorePreview struct {
	SessionName string   `json:"session_name"`
	WorkDir     string   `json:"work_dir"`
	PaneCount   int      `json:"pane_count"`
	AgentCount  int      `json:"agent_count"`
	Layout      string   `json:"layout"`
	Actions     []string `json:"actions"`
}

// GetSave saves a session state and returns the result.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetSave(opts SaveOptions) (*SaveResult, error) {
	if err := tmux.EnsureInstalled(); err != nil {
		return &SaveResult{
			Success: false,
			Session: opts.Session,
			Error:   err.Error(),
		}, nil
	}

	sessionName := opts.Session
	if sessionName == "" {
		return &SaveResult{
			Success: false,
			Session: "",
			Error:   "session name is required",
		}, nil
	}

	if !backendSessionExists(sessionName) {
		return &SaveResult{
			Success: false,
			Session: sessionName,
			Error:   fmt.Sprintf("session '%s' not found", sessionName),
		}, nil
	}

	// Capture session state
	state, err := session.Capture(sessionName)
	if err != nil {
		return &SaveResult{
			Success: false,
			Session: sessionName,
			Error:   fmt.Sprintf("failed to capture session state: %v", err),
		}, nil
	}

	// Save state
	saveOpts := session.SaveOptions{
		Overwrite: true, // Robot mode always overwrites
	}
	path, err := session.Save(state, saveOpts)
	if err != nil {
		return &SaveResult{
			Success: false,
			Session: sessionName,
			Error:   fmt.Sprintf("failed to save session state: %v", err),
		}, nil
	}

	// If custom output file requested, also write there
	if opts.OutputFile != "" {
		data, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return &SaveResult{
				Success: false,
				Session: sessionName,
				Error:   fmt.Sprintf("failed to marshal state: %v", err),
			}, nil
		}
		if err := os.WriteFile(opts.OutputFile, data, 0644); err != nil {
			return &SaveResult{
				Success: false,
				Session: sessionName,
				Error:   fmt.Sprintf("failed to write to %s: %v", opts.OutputFile, err),
			}, nil
		}
		path = opts.OutputFile
	}

	return &SaveResult{
		Success:  true,
		Session:  sessionName,
		SavedAs:  sessionName,
		FilePath: path,
		State:    state,
	}, nil
}

// PrintSave saves a session state and outputs JSON.
// This is a thin wrapper around GetSave() for CLI output.
func PrintSave(opts SaveOptions) error {
	result, err := GetSave(opts)
	if err != nil {
		return err
	}
	return encodeJSON(result)
}

// GetRestore restores a session from saved state and returns the result.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetRestore(opts RestoreOptions) (*RestoreResult, error) {
	if err := tmux.EnsureInstalled(); err != nil {
		return &RestoreResult{
			Success:   false,
			SavedName: opts.SavedName,
			Error:     err.Error(),
		}, nil
	}

	if opts.SavedName == "" {
		return &RestoreResult{
			Success:   false,
			SavedName: "",
			Error:     "saved state name is required",
		}, nil
	}

	// Load saved state
	state, err := session.Load(opts.SavedName)
	if err != nil {
		return &RestoreResult{
			Success:   false,
			SavedName: opts.SavedName,
			Error:     fmt.Sprintf("failed to load saved state: %v", err),
		}, nil
	}

	// Dry run mode - preview what would happen
	if opts.DryRun {
		preview := buildRestorePreview(state)
		return &RestoreResult{
			Success:   true,
			SavedName: opts.SavedName,
			DryRun:    true,
			State:     state,
			Preview:   preview,
		}, nil
	}

	// Check if session already exists
	if backendSessionExists(state.Name) {
		return &RestoreResult{
			Success:   false,
			SavedName: opts.SavedName,
			Error:     fmt.Sprintf("session '%s' already exists (use 'herdctl sessions restore' with --force to overwrite)", state.Name),
		}, nil
	}

	// Restore session
	restoreOpts := session.RestoreOptions{
		Force: false, // Robot mode is cautious by default
	}
	if err := session.Restore(state, restoreOpts); err != nil {
		return &RestoreResult{
			Success:   false,
			SavedName: opts.SavedName,
			Error:     fmt.Sprintf("failed to restore session: %v", err),
		}, nil
	}

	return &RestoreResult{
		Success:    true,
		SavedName:  opts.SavedName,
		RestoredAs: state.Name,
		DryRun:     false,
		State:      state,
	}, nil
}

// PrintRestore restores a session from saved state and outputs JSON.
// This is a thin wrapper around GetRestore() for CLI output.
func PrintRestore(opts RestoreOptions) error {
	result, err := GetRestore(opts)
	if err != nil {
		return err
	}
	return encodeJSON(result)
}

func buildRestorePreview(state *session.SessionState) *RestorePreview {
	actions := []string{
		fmt.Sprintf("Create tmux session '%s'", state.Name),
		fmt.Sprintf("Set working directory to '%s'", state.WorkDir),
	}

	if len(state.Panes) > 1 {
		actions = append(actions, fmt.Sprintf("Create %d panes", len(state.Panes)))
	}

	if state.Layout != "" && state.Layout != "tiled" {
		actions = append(actions, fmt.Sprintf("Apply layout '%s'", state.Layout))
	}

	for _, p := range state.Panes {
		if p.Command != "" {
			actions = append(actions, fmt.Sprintf("Start '%s' in pane %d", p.Title, p.Index))
		}
	}

	return &RestorePreview{
		SessionName: state.Name,
		WorkDir:     state.WorkDir,
		PaneCount:   len(state.Panes),
		AgentCount:  state.Agents.Total(),
		Layout:      state.Layout,
		Actions:     actions,
	}
}

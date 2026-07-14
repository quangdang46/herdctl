//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-CLEANUP] Tests for ntm cleanup workflow.
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type cleanupResult struct {
	Path     string    `json:"path"`
	Type     string    `json:"type"`
	Size     int64     `json:"size_bytes"`
	Age      string    `json:"age"`
	AgeHours float64   `json:"age_hours"`
	ModTime  time.Time `json:"mod_time"`
	Deleted  bool      `json:"deleted"`
	Error    string    `json:"error,omitempty"`
	Pattern  string    `json:"pattern"`
}

type cleanupResponse struct {
	GeneratedAt    time.Time       `json:"generated_at"`
	DryRun         bool            `json:"dry_run"`
	MaxAgeHours    int             `json:"max_age_hours"`
	Results        []cleanupResult `json:"results"`
	TotalFiles     int             `json:"total_files"`
	TotalSize      int64           `json:"total_size_bytes"`
	DeletedFiles   int             `json:"deleted_files"`
	DeletedSize    int64           `json:"deleted_size_bytes"`
	SkippedFiles   int             `json:"skipped_files"`
	ErrorCount     int             `json:"error_count"`
	LastOutput     string          `json:"-"`
	LastCommand    []string        `json:"-"`
	LastParseError string          `json:"-"`
}

func runCleanupJSON(t *testing.T, logger *TestLogger, args ...string) (cleanupResponse, []byte, error) {
	allArgs := append([]string{"cleanup", "--json"}, args...)
	cmd := exec.Command(mustE2EBin(), allArgs...)
	output, err := cmd.CombinedOutput()

	logger.Log("[E2E-CLEANUP] Running: ntm %v", allArgs)
	logger.Log("[E2E-CLEANUP] Output: %s", string(output))

	var resp cleanupResponse
	if jsonErr := json.Unmarshal(output, &resp); jsonErr != nil {
		resp.LastParseError = jsonErr.Error()
		resp.LastOutput = string(output)
		resp.LastCommand = allArgs
		if err == nil {
			return resp, output, fmt.Errorf("parse cleanup response: %w", jsonErr)
		}
		return resp, output, err
	}

	return resp, output, err
}

func createCleanupFile(t *testing.T, logger *TestLogger, name string, modTime time.Time) string {
	path := filepath.Join(os.TempDir(), name)
	if err := os.WriteFile(path, []byte("cleanup-e2e"), 0o644); err != nil {
		t.Fatalf("[E2E-CLEANUP] failed to create file %s: %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("[E2E-CLEANUP] failed to set mod time for %s: %v", path, err)
	}
	logger.Log("[E2E-CLEANUP] Created file: %s (mod=%s)", path, modTime.Format(time.RFC3339))
	return path
}

func createCleanupDir(t *testing.T, logger *TestLogger, name string, modTime time.Time) string {
	path := filepath.Join(os.TempDir(), name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("[E2E-CLEANUP] failed to create dir %s: %v", path, err)
	}
	payload := filepath.Join(path, "payload.txt")
	if err := os.WriteFile(payload, []byte("cleanup-e2e"), 0o644); err != nil {
		t.Fatalf("[E2E-CLEANUP] failed to create payload %s: %v", payload, err)
	}
	if err := os.Chtimes(payload, modTime, modTime); err != nil {
		t.Fatalf("[E2E-CLEANUP] failed to set payload mod time for %s: %v", payload, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("[E2E-CLEANUP] failed to set dir mod time for %s: %v", path, err)
	}
	logger.Log("[E2E-CLEANUP] Created dir: %s (mod=%s)", path, modTime.Format(time.RFC3339))
	return path
}

func findCleanupResult(results []cleanupResult, path string) (cleanupResult, bool) {
	for _, r := range results {
		if r.Path == path {
			return r, true
		}
	}
	return cleanupResult{}, false
}

func TestE2E_CleanupWorkflow(t *testing.T) {
	SkipIfShort(t)
	SkipIfNoNTM(t)

	logger := NewTestLogger(t, "cleanup-workflow")
	t.Cleanup(func() {
		logger.Close()
	})

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	oldTime := time.Now().Add(-48 * time.Hour)
	recentTime := time.Now().Add(-30 * time.Minute)

	oldDir := createCleanupDir(t, logger, "test-ntm-cleanup-old-"+suffix, oldTime)
	oldFile := createCleanupFile(t, logger, "ntm-atomic-cleanup-old-"+suffix, oldTime)
	newDir := createCleanupDir(t, logger, "ntm-lifecycle-cleanup-new-"+suffix, recentTime)
	newFile := createCleanupFile(t, logger, "ntm-prompt-cleanup-new-"+suffix+".md", recentTime)

	cleanupPaths := []string{oldDir, oldFile, newDir, newFile}
	t.Cleanup(func() {
		for _, path := range cleanupPaths {
			_ = os.RemoveAll(path)
		}
	})

	logger.Log("[E2E-CLEANUP] Created %d test items", len(cleanupPaths))

	dryResp, _, err := runCleanupJSON(t, logger, "--dry-run", "--max-age=1")
	if err != nil {
		t.Fatalf("[E2E-CLEANUP] cleanup dry-run failed: %v", err)
	}

	logger.Log("[E2E-CLEANUP] Dry-run results: total=%d deleted=%d skipped=%d errors=%d",
		dryResp.TotalFiles, dryResp.DeletedFiles, dryResp.SkippedFiles, dryResp.ErrorCount)

	for _, path := range []string{oldDir, oldFile} {
		res, ok := findCleanupResult(dryResp.Results, path)
		if !ok {
			t.Fatalf("[E2E-CLEANUP] missing dry-run result for old item: %s", path)
		}
		if res.Pattern == "" {
			t.Fatalf("[E2E-CLEANUP] missing pattern for %s", path)
		}
		if res.AgeHours < 1.0 {
			t.Fatalf("[E2E-CLEANUP] expected old item age >= 1h for %s, got %.2f", path, res.AgeHours)
		}
		if res.Deleted {
			t.Fatalf("[E2E-CLEANUP] dry-run should not mark deleted for %s", path)
		}
		logger.Log("[E2E-CLEANUP] Dry-run would delete: %s (age=%.2fh pattern=%s)", path, res.AgeHours, res.Pattern)
	}

	for _, path := range []string{newDir, newFile} {
		res, ok := findCleanupResult(dryResp.Results, path)
		if !ok {
			t.Fatalf("[E2E-CLEANUP] missing dry-run result for new item: %s", path)
		}
		if res.Pattern == "" {
			t.Fatalf("[E2E-CLEANUP] missing pattern for %s", path)
		}
		if res.AgeHours >= 1.0 {
			t.Fatalf("[E2E-CLEANUP] expected new item age < 1h for %s, got %.2f", path, res.AgeHours)
		}
		if res.Deleted {
			t.Fatalf("[E2E-CLEANUP] dry-run should not mark deleted for %s", path)
		}
		logger.Log("[E2E-CLEANUP] Dry-run preserved: %s (age=%.2fh pattern=%s)", path, res.AgeHours, res.Pattern)
	}

	if dryResp.DeletedFiles < 2 {
		t.Fatalf("[E2E-CLEANUP] expected at least 2 would-delete items, got %d", dryResp.DeletedFiles)
	}
	if dryResp.SkippedFiles < 2 {
		t.Fatalf("[E2E-CLEANUP] expected at least 2 skipped items, got %d", dryResp.SkippedFiles)
	}

	forceResp, _, err := runCleanupJSON(t, logger, "--dry-run", "--force")
	if err != nil {
		t.Fatalf("[E2E-CLEANUP] cleanup force dry-run failed: %v", err)
	}

	logger.Log("[E2E-CLEANUP] Force dry-run results: total=%d deleted=%d skipped=%d errors=%d",
		forceResp.TotalFiles, forceResp.DeletedFiles, forceResp.SkippedFiles, forceResp.ErrorCount)

	for _, path := range cleanupPaths {
		res, ok := findCleanupResult(forceResp.Results, path)
		if !ok {
			t.Fatalf("[E2E-CLEANUP] missing force dry-run result for %s", path)
		}
		if res.Pattern == "" {
			t.Fatalf("[E2E-CLEANUP] missing pattern for %s", path)
		}
		logger.Log("[E2E-CLEANUP] Force dry-run would delete: %s (age=%.2fh pattern=%s)", path, res.AgeHours, res.Pattern)
	}

	if forceResp.DeletedFiles < len(cleanupPaths) {
		t.Fatalf("[E2E-CLEANUP] expected at least %d would-delete items in force mode, got %d",
			len(cleanupPaths), forceResp.DeletedFiles)
	}
}

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/bv"
)

func TestEvaluateQueueDrySyncInSync(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	mustMkdirAll(t, beadsDir)

	issuesPath := filepath.Join(beadsDir, "issues.jsonl")
	dbPath := filepath.Join(beadsDir, "beads.db")
	mustWriteFile(t, issuesPath, []byte("[]"))
	mustWriteFile(t, dbPath, []byte("sqlite"))

	now := time.Now().Add(-5 * time.Minute).UTC()
	mustChtimes(t, issuesPath, now, now)
	mustChtimes(t, dbPath, now, now)

	got := evaluateQueueDrySync(dir, 10*time.Minute)
	if !got.HasLocalBeadsDB {
		t.Fatalf("expected HasLocalBeadsDB=true")
	}
	if got.Status != "in_sync" {
		t.Fatalf("status=%q, want in_sync", got.Status)
	}
	if got.NeedsFlush {
		t.Fatalf("NeedsFlush=true, want false")
	}
}

func TestEvaluateQueueDrySyncDBNewerNeedsFlush(t *testing.T) {
	dir := t.TempDir()
	beadsDir := filepath.Join(dir, ".beads")
	mustMkdirAll(t, beadsDir)

	issuesPath := filepath.Join(beadsDir, "issues.jsonl")
	dbPath := filepath.Join(beadsDir, "beads.db")
	mustWriteFile(t, issuesPath, []byte("[]"))
	mustWriteFile(t, dbPath, []byte("sqlite"))

	now := time.Now().UTC()
	mustChtimes(t, issuesPath, now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	mustChtimes(t, dbPath, now, now)

	got := evaluateQueueDrySync(dir, 10*time.Minute)
	if got.Status != "beads_db_newer_than_jsonl" {
		t.Fatalf("status=%q, want beads_db_newer_than_jsonl", got.Status)
	}
	if !got.NeedsFlush {
		t.Fatalf("NeedsFlush=false, want true")
	}
}

func TestFindStaleInProgressSortAndLimit(t *testing.T) {
	now := time.Now().UTC()
	inProgress := []bv.BeadInProgress{
		{ID: "bd-newer", Title: "newer", UpdatedAt: now.Add(-30 * time.Hour)},
		{ID: "bd-oldest", Title: "oldest", UpdatedAt: now.Add(-90 * time.Hour)},
		{ID: "bd-fresh", Title: "fresh", UpdatedAt: now.Add(-2 * time.Hour)},
	}

	got := findStaleInProgress(inProgress, now, 24*time.Hour, 2)
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	if got[0].ID != "bd-oldest" || got[1].ID != "bd-newer" {
		t.Fatalf("order=%v, want [bd-oldest bd-newer]", []string{got[0].ID, got[1].ID})
	}
}

func TestBuildQueueDryRecommendationsQueueDry(t *testing.T) {
	report := QueueDryResponse{
		QueueDry: true,
		Evidence: QueueDryEvidence{
			ActionableCount: 0,
			ReadyCount:      0,
			Sync: QueueDrySyncStatus{
				NeedsFlush: true,
				Status:     "beads_db_newer_than_jsonl",
			},
			StaleInProgress: []QueueDryStaleIssue{
				{ID: "bd-stale-1", AgeHours: 72},
			},
			Reservations: QueueDryReservations{
				Available: true,
				Count:     2,
			},
		},
	}

	recs := buildQueueDryRecommendations(report)
	got := make([]string, 0, len(recs))
	for _, rec := range recs {
		got = append(got, rec.Code)
	}
	for _, code := range []string{"flush_jsonl", "inspect_stale_in_progress", "inspect_active_reservations", "review_pass", "alerts_sweep", "seed_new_task"} {
		if !containsStringSlice(got, code) {
			t.Fatalf("missing recommendation code %q in %v", code, got)
		}
	}
}

func TestBuildQueueDryRecommendationsActionable(t *testing.T) {
	report := QueueDryResponse{
		QueueDry: false,
		Evidence: QueueDryEvidence{
			ActionableCount: 1,
			ReadyCount:      1,
			TriageTopIDs:    []string{"bd-123", "bd-456"},
		},
	}

	recs := buildQueueDryRecommendations(report)
	if len(recs) == 0 {
		t.Fatalf("expected at least one recommendation")
	}
	if recs[len(recs)-1].Code != "claim_top_ready" {
		t.Fatalf("last code=%q, want claim_top_ready", recs[len(recs)-1].Code)
	}
	if !strings.Contains(recs[len(recs)-1].Command, "bd-123") {
		t.Fatalf("command=%q, expected top ID", recs[len(recs)-1].Command)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func mustChtimes(t *testing.T, path string, atime, mtime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, atime, mtime); err != nil {
		t.Fatalf("Chtimes(%q): %v", path, err)
	}
}

func containsStringSlice(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

package herdr

import (
	"path/filepath"
	"testing"
)

func TestRegistryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")

	reg, err := LoadRegistry(path)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	rec := SessionRecord{
		Name:        "api",
		WorkspaceID: "w9",
		Directory:   "/tmp/api",
		RootPaneID:  "w9:p1",
		Panes: map[string]PaneMeta{
			"w9:p1": {
				PaneID:    "w9:p1",
				Session:   "api",
				AgentType: AgentClaude,
				NTMIndex:  1,
				Title:     FormatPaneName("api", "cc", 1, ""),
			},
		},
	}
	if err := reg.PutSession(rec); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	reg2, err := LoadRegistry(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := reg2.GetSession("api")
	if !ok {
		t.Fatal("expected session api")
	}
	if got.WorkspaceID != "w9" {
		t.Fatalf("workspace_id=%s", got.WorkspaceID)
	}
	meta, ok := got.Panes["w9:p1"]
	if !ok || meta.AgentType != AgentClaude || meta.NTMIndex != 1 {
		t.Fatalf("pane meta=%+v ok=%v", meta, ok)
	}

	if err := reg2.DeleteSession("api"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, ok := reg2.GetSession("api"); ok {
		t.Fatal("session should be gone")
	}
}

func TestNextNTMIndex(t *testing.T) {
	panes := map[string]PaneMeta{
		"a": {AgentType: AgentClaude, NTMIndex: 1},
		"b": {AgentType: AgentClaude, NTMIndex: 3},
		"c": {AgentType: AgentCodex, NTMIndex: 9},
	}
	if n := NextNTMIndex(panes, AgentClaude); n != 4 {
		t.Fatalf("claude next=%d", n)
	}
	if n := NextNTMIndex(panes, AgentGemini); n != 1 {
		t.Fatalf("gemini next=%d", n)
	}
}

func TestFormatAndParsePaneName(t *testing.T) {
	title := FormatPaneName("proj", "cc", 2, "opus") + FormatTags([]string{"api", "backend"})
	// FormatPaneName does not include tags; append like SetPaneTitle callers do.
	t.Logf("title=%s", title)
	typ, idx, variant, tags := ParseAgentFromTitle(FormatPaneName("proj", "cc", 2, "opus") + "[api,backend]")
	if typ != AgentClaude && string(typ) != "cc" {
		// AgentClaude constant is "cc"
		if string(typ) != "cc" {
			t.Fatalf("type=%s", typ)
		}
	}
	if idx != 2 || variant != "opus" {
		t.Fatalf("idx=%d variant=%s", idx, variant)
	}
	if len(tags) != 2 || tags[0] != "api" {
		t.Fatalf("tags=%v", tags)
	}
}

func TestValidateSessionName(t *testing.T) {
	if err := ValidateSessionName("good_name-1"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSessionName("bad name"); err == nil {
		t.Fatal("expected error")
	}
	if err := ValidateSessionName(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestTranslateNamedKey(t *testing.T) {
	if k, ok := translateNamedKey("C-c"); !ok || k != "ctrl+c" {
		t.Fatalf("got %s %v", k, ok)
	}
	if _, ok := translateNamedKey("hello world"); ok {
		t.Fatal("plain text should not translate")
	}
}

func TestPaneIndexFromID(t *testing.T) {
	if got := paneIndexFromID("w4G:p2"); got != 2 {
		t.Fatalf("w4G:p2 → %d, want 2", got)
	}
	if got := paneIndexFromID("w1:p10"); got != 10 {
		t.Fatalf("w1:p10 → %d, want 10", got)
	}
	if got := paneIndexFromID(""); got != 0 {
		t.Fatalf("empty → %d, want 0", got)
	}
	if got := paneIndexFromID("not-a-pane"); got != 0 {
		t.Fatalf("bad → %d, want 0", got)
	}
}

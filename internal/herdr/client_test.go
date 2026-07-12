package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsInstalled(t *testing.T) {
	c := NewClient()
	// Should not panic; result depends on environment.
	_ = c.IsInstalled()
}

func TestNotSupportedErrors(t *testing.T) {
	c := NewClient()
	if err := c.ApplyTiledLayout("x"); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("ApplyTiledLayout: %v", err)
	}
	if err := c.AttachOrSwitch("x"); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("AttachOrSwitch: %v", err)
	}
	if err := c.SetPaneBorderStyle("p", "red"); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("SetPaneBorderStyle: %v", err)
	}
}

func TestRunJSONDecodeWorkspaceList(t *testing.T) {
	// Unit-test the envelope decoder without requiring a live herdr server by
	// shelling out to a fake binary.
	dir := t.TempDir()
	fake := filepath.Join(dir, "herdr-fake")
	script := `#!/bin/sh
cat <<'EOF'
{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[{"workspace_id":"w1","number":1,"label":"demo","focused":true,"pane_count":1,"tab_count":1,"active_tab_id":"w1:t1","agent_status":"idle"}]}}
EOF
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	c := &Client{Binary: fake, RegistryPath: filepath.Join(dir, "reg.json")}
	var out workspaceListResult
	if err := c.runJSON(context.Background(), &out, "workspace", "list"); err != nil {
		t.Fatalf("runJSON: %v", err)
	}
	if len(out.Workspaces) != 1 || out.Workspaces[0].Label != "demo" {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestCreateSessionWithFakeHerdr(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "herdr-fake")
	// Respond based on argv keywords.
	script := `#!/bin/sh
case " $* " in
  *" workspace list "*)
    cat <<'EOF'
{"id":"x","result":{"type":"workspace_list","workspaces":[]}}
EOF
    ;;
  *" workspace create "*)
    cat <<'EOF'
{"id":"x","result":{"type":"workspace_created","workspace":{"workspace_id":"w7","number":7,"label":"demo","focused":false,"pane_count":1,"tab_count":1,"active_tab_id":"w7:t1","agent_status":"unknown"},"tab":{"tab_id":"w7:t1","workspace_id":"w7","number":1,"label":"1","focused":false,"pane_count":1,"agent_status":"unknown"},"root_pane":{"pane_id":"w7:p1","workspace_id":"w7","tab_id":"w7:t1","terminal_id":"term_x","cwd":"/tmp","focused":false,"agent_status":"unknown"}}}
EOF
    ;;
  *" pane rename "*)
    cat <<'EOF'
{"id":"x","result":{"type":"ok"}}
EOF
    ;;
  *)
    echo '{"id":"x","error":{"code":"unknown","message":"unhandled"}}' >&2
    exit 1
    ;;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	regPath := filepath.Join(dir, "reg.json")
	c := &Client{Binary: fake, RegistryPath: regPath}

	if err := c.CreateSession("demo", dir); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if !c.SessionExists("demo") {
		// SessionExists also checks live workspace get — our fake only handles list/create.
		// Direct registry check:
		reg, err := LoadRegistry(regPath)
		if err != nil {
			t.Fatal(err)
		}
		rec, ok := reg.GetSession("demo")
		if !ok || rec.WorkspaceID != "w7" || rec.RootPaneID != "w7:p1" {
			t.Fatalf("registry record=%+v ok=%v", rec, ok)
		}
	}

	raw, _ := os.ReadFile(regPath)
	var file RegistryFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if file.Sessions["demo"].WorkspaceID != "w7" {
		t.Fatalf("file=%s", raw)
	}
}

func TestLiveIntegration(t *testing.T) {
	if os.Getenv("HERDCTL_HERDR_LIVE") != "1" {
		t.Skip("set HERDCTL_HERDR_LIVE=1 to run live herdr integration tests")
	}
	if _, err := exec.LookPath("herdr"); err != nil {
		t.Skip("herdr not on PATH")
	}
	c := NewClient()
	c.RegistryPath = filepath.Join(t.TempDir(), "reg.json")
	if err := c.EnsureInstalled(); err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}

	name := "herdctl-live-test"
	_ = c.KillSession(name) // cleanup leftover
	cwd := t.TempDir()
	if err := c.CreateSession(name, cwd); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Cleanup(func() { _ = c.KillSession(name) })

	if !c.SessionExists(name) {
		t.Fatal("SessionExists false")
	}
	panes, err := c.GetPanes(name)
	if err != nil {
		t.Fatalf("GetPanes: %v", err)
	}
	if len(panes) < 1 {
		t.Fatal("expected at least root pane")
	}
	text, err := c.CapturePaneOutput(panes[0].ID, 20)
	if err != nil {
		t.Fatalf("CapturePaneOutput: %v", err)
	}
	t.Logf("capture len=%d", len(text))
}

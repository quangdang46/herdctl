package herdr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/util"
)

const registryVersion = 1

// Registry persists NTM session/pane identity on top of Herdr ids.
//
// Default path: ~/.ntm/herdr/registry.json
// Override with HERDCTL_HERDR_REGISTRY or Client.RegistryPath.
type Registry struct {
	mu   sync.Mutex
	path string
	data RegistryFile
}

func defaultRegistryPath() (string, error) {
	if env := os.Getenv("HERDCTL_HERDR_REGISTRY"); env != "" {
		return env, nil
	}
	dir, err := util.NTMDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "herdr", "registry.json"), nil
}

// LoadRegistry opens (or creates) the registry at path.
func LoadRegistry(path string) (*Registry, error) {
	if path == "" {
		var err error
		path, err = defaultRegistryPath()
		if err != nil {
			return nil, err
		}
	}
	r := &Registry{
		path: path,
		data: RegistryFile{
			Version:  registryVersion,
			Sessions: map[string]SessionRecord{},
		},
	}
	if err := r.reload(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return r, nil
}

func (r *Registry) Path() string { return r.path }

func (r *Registry) reload() error {
	raw, err := os.ReadFile(r.path)
	if err != nil {
		return err
	}
	var file RegistryFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return fmt.Errorf("parse registry %s: %w", r.path, err)
	}
	if file.Sessions == nil {
		file.Sessions = map[string]SessionRecord{}
	}
	if file.Version == 0 {
		file.Version = registryVersion
	}
	r.data = file
	return nil
}

func (r *Registry) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	r.data.Version = registryVersion
	raw, err := json.MarshalIndent(r.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

func (r *Registry) GetSession(name string) (SessionRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.data.Sessions[name]
	return rec, ok
}

func (r *Registry) PutSession(rec SessionRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec.Panes == nil {
		rec.Panes = map[string]PaneMeta{}
	}
	rec.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	r.data.Sessions[rec.Name] = rec
	return r.saveLocked()
}

func (r *Registry) DeleteSession(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.data.Sessions, name)
	return r.saveLocked()
}

func (r *Registry) UpsertPane(session string, meta PaneMeta) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.data.Sessions[session]
	if !ok {
		return fmt.Errorf("%w: session %q", ErrNotFound, session)
	}
	if rec.Panes == nil {
		rec.Panes = map[string]PaneMeta{}
	}
	meta.Session = session
	if meta.WorkspaceID == "" {
		meta.WorkspaceID = rec.WorkspaceID
	}
	if meta.CreatedAt == "" {
		meta.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	rec.Panes[meta.PaneID] = meta
	rec.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	r.data.Sessions[session] = rec
	return r.saveLocked()
}

func (r *Registry) RemovePane(session, paneID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.data.Sessions[session]
	if !ok {
		return nil
	}
	delete(rec.Panes, paneID)
	rec.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	r.data.Sessions[session] = rec
	return r.saveLocked()
}

func (r *Registry) ListSessions() []SessionRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SessionRecord, 0, len(r.data.Sessions))
	for _, rec := range r.data.Sessions {
		out = append(out, rec)
	}
	return out
}

// FindSessionByWorkspace returns the NTM session name bound to workspaceID.
func (r *Registry) FindSessionByWorkspace(workspaceID string) (SessionRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range r.data.Sessions {
		if rec.WorkspaceID == workspaceID {
			return rec, true
		}
	}
	return SessionRecord{}, false
}

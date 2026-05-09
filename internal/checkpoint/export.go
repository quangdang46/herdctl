package checkpoint

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/redaction"
	"github.com/Dicklesworthstone/ntm/internal/util"
)

// ExportFormat specifies the archive format for export.
type ExportFormat string

const (
	FormatTarGz ExportFormat = "tar.gz"
	FormatZip   ExportFormat = "zip"
)

var maxImportEntrySize int64 = 100 << 20

// ExportOptions configures checkpoint export.
type ExportOptions struct {
	// Format specifies the archive format (default: tar.gz)
	Format ExportFormat
	// RedactSecrets removes potential secrets from scrollback
	RedactSecrets bool
	// RewritePaths makes paths portable across machines
	RewritePaths bool
	// IncludeScrollback includes scrollback files in export
	IncludeScrollback bool
	// IncludeGitPatch includes git patch file in export
	IncludeGitPatch bool
}

// DefaultExportOptions returns sensible defaults for export.
func DefaultExportOptions() ExportOptions {
	return ExportOptions{
		Format:            FormatTarGz,
		RedactSecrets:     false,
		RewritePaths:      true,
		IncludeScrollback: true,
		IncludeGitPatch:   true,
	}
}

// ExportManifest contains metadata about an exported checkpoint.
type ExportManifest struct {
	Version        int               `json:"version"`
	ExportedAt     time.Time         `json:"exported_at"`
	SessionName    string            `json:"session_name"`
	CheckpointID   string            `json:"checkpoint_id"`
	CheckpointName string            `json:"checkpoint_name"`
	OriginalPath   string            `json:"original_path"`
	Files          []ManifestEntry   `json:"files"`
	Checksums      map[string]string `json:"checksums"`
}

// ManifestEntry describes a file in the export.
type ManifestEntry struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Checksum string `json:"checksum"`
}

// ImportOptions configures checkpoint import.
type ImportOptions struct {
	// TargetSession overrides the session name on import
	TargetSession string
	// TargetDir overrides the working directory on import
	TargetDir string
	// VerifyChecksums validates file integrity on import
	VerifyChecksums bool
	// AllowOverwrite permits overwriting existing checkpoints
	AllowOverwrite bool
}

// DefaultImportOptions returns sensible defaults for import.
func DefaultImportOptions() ImportOptions {
	return ImportOptions{
		VerifyChecksums: true,
		AllowOverwrite:  false,
	}
}

var (
	redactionConfig *redaction.Config
	redactionMu     sync.RWMutex
)

// SetRedactionConfig sets the global redaction config for checkpoint export redaction.
// Pass nil to use the default redaction config.
func SetRedactionConfig(cfg *redaction.Config) {
	redactionMu.Lock()
	defer redactionMu.Unlock()
	if cfg != nil {
		// bd-pmdpn: deep-copy reference-typed fields so a caller
		// mutating cfg after Set cannot reach into stored state.
		c := cfg.DeepCopy()
		redactionConfig = &c
	} else {
		redactionConfig = nil
	}
}

// GetRedactionConfig returns the current redaction config (or nil if unset).
// Returned value is independent of the stored config — mutating its
// reference-typed fields does not leak into future Get/Set calls.
func GetRedactionConfig() *redaction.Config {
	redactionMu.RLock()
	defer redactionMu.RUnlock()
	if redactionConfig == nil {
		return nil
	}
	c := redactionConfig.DeepCopy()
	return &c
}

// Export creates a portable archive of a checkpoint.
func (s *Storage) Export(sessionName, checkpointID string, destPath string, opts ExportOptions) (*ExportManifest, error) {
	if opts.Format == "" {
		opts.Format = FormatTarGz
	}

	// Load the checkpoint
	cp, err := s.Load(sessionName, checkpointID)
	if err != nil {
		return nil, fmt.Errorf("failed to load checkpoint: %w", err)
	}

	// Build the checkpoint directory path
	cpDir, err := s.safeCheckpointDir(sessionName, checkpointID)
	if err != nil {
		return nil, err
	}

	// Determine output path
	if destPath == "" {
		ext := ".tar.gz"
		if opts.Format == FormatZip {
			ext = ".zip"
		}
		destPath = fmt.Sprintf("%s_%s%s", sessionName, checkpointID, ext)
	}

	// Collect files to export
	var files []string
	files = append(files, MetadataFile)
	files = append(files, SessionFile)

	if opts.IncludeScrollback {
		for _, pane := range cp.Session.Panes {
			if pane.ScrollbackFile != "" {
				files = append(files, pane.ScrollbackFile)
			}
		}
	}

	if opts.IncludeGitPatch && cp.Git.PatchFile != "" {
		files = append(files, cp.Git.PatchFile)
	}
	if cp.Git.StatusFile != "" {
		files = append(files, cp.Git.StatusFile)
	}

	// Create manifest
	manifest := &ExportManifest{
		Version:        1,
		ExportedAt:     time.Now(),
		SessionName:    sessionName,
		CheckpointID:   cp.ID,
		CheckpointName: cp.Name,
		OriginalPath:   cp.WorkingDir,
		Checksums:      make(map[string]string),
	}

	// Prepare checkpoint data (potentially with path rewriting)
	cpData := rewriteCheckpointForExport(cp, opts)

	// Create the archive
	switch opts.Format {
	case FormatTarGz:
		err = s.exportTarGz(destPath, cpDir, cpData, files, opts, manifest)
	case FormatZip:
		err = s.exportZip(destPath, cpDir, cpData, files, opts, manifest)
	default:
		return nil, fmt.Errorf("unsupported export format: %s", opts.Format)
	}

	if err != nil {
		return nil, err
	}

	return manifest, nil
}

func (s *Storage) exportTarGz(destPath, cpDir string, cp *Checkpoint, files []string, opts ExportOptions, manifest *ExportManifest) error {
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create export file: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Write metadata.json
	cpJSON, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	checksum := sha256sum(cpJSON)
	manifest.Checksums[MetadataFile] = checksum
	manifest.Files = append(manifest.Files, ManifestEntry{
		Path:     MetadataFile,
		Size:     int64(len(cpJSON)),
		Checksum: checksum,
	})

	if err := writeTarEntry(tw, MetadataFile, cpJSON); err != nil {
		return err
	}

	sessionJSON, err := json.MarshalIndent(cp.Session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session state: %w", err)
	}

	checksum = sha256sum(sessionJSON)
	manifest.Checksums[SessionFile] = checksum
	manifest.Files = append(manifest.Files, ManifestEntry{
		Path:     SessionFile,
		Size:     int64(len(sessionJSON)),
		Checksum: checksum,
	})

	if err := writeTarEntry(tw, SessionFile, sessionJSON); err != nil {
		return err
	}

	// Write other files
	for _, file := range files {
		if file == MetadataFile || file == SessionFile {
			continue
		}

		srcPath, err := resolveExistingCheckpointArtifactPath(cpDir, file)
		if err != nil {
			return fmt.Errorf("invalid checkpoint file path %s: %w", file, err)
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("failed to read checkpoint file %s: %w", file, err)
		}

		// Redact secrets from scrollback files
		if opts.RedactSecrets && strings.HasPrefix(file, PanesDir+"/") {
			data = redactSecrets(data)
		}

		checksum := sha256sum(data)
		manifest.Checksums[file] = checksum
		manifest.Files = append(manifest.Files, ManifestEntry{
			Path:     file,
			Size:     int64(len(data)),
			Checksum: checksum,
		})

		if err := writeTarEntry(tw, file, data); err != nil {
			return err
		}
	}

	// Write manifest
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	if err := writeTarEntry(tw, "MANIFEST.json", manifestJSON); err != nil {
		return err
	}

	return nil
}

func (s *Storage) exportZip(destPath, cpDir string, cp *Checkpoint, files []string, opts ExportOptions, manifest *ExportManifest) error {
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create export file: %w", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	// Write metadata.json
	cpJSON, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	checksum := sha256sum(cpJSON)
	manifest.Checksums[MetadataFile] = checksum
	manifest.Files = append(manifest.Files, ManifestEntry{
		Path:     MetadataFile,
		Size:     int64(len(cpJSON)),
		Checksum: checksum,
	})

	if err := writeZipEntry(zw, MetadataFile, cpJSON); err != nil {
		return err
	}

	sessionJSON, err := json.MarshalIndent(cp.Session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session state: %w", err)
	}

	checksum = sha256sum(sessionJSON)
	manifest.Checksums[SessionFile] = checksum
	manifest.Files = append(manifest.Files, ManifestEntry{
		Path:     SessionFile,
		Size:     int64(len(sessionJSON)),
		Checksum: checksum,
	})

	if err := writeZipEntry(zw, SessionFile, sessionJSON); err != nil {
		return err
	}

	// Write other files
	for _, file := range files {
		if file == MetadataFile || file == SessionFile {
			continue
		}

		srcPath, err := resolveExistingCheckpointArtifactPath(cpDir, file)
		if err != nil {
			return fmt.Errorf("invalid checkpoint file path %s: %w", file, err)
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("failed to read checkpoint file %s: %w", file, err)
		}

		if opts.RedactSecrets && strings.HasPrefix(file, PanesDir+"/") {
			data = redactSecrets(data)
		}

		checksum := sha256sum(data)
		manifest.Checksums[file] = checksum
		manifest.Files = append(manifest.Files, ManifestEntry{
			Path:     file,
			Size:     int64(len(data)),
			Checksum: checksum,
		})

		if err := writeZipEntry(zw, file, data); err != nil {
			return err
		}
	}

	// Write manifest
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	if err := writeZipEntry(zw, "MANIFEST.json", manifestJSON); err != nil {
		return err
	}

	return nil
}

// Import loads a checkpoint from an exported archive.
func (s *Storage) Import(archivePath string, opts ImportOptions) (*Checkpoint, error) {
	var format ExportFormat
	switch {
	case strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz"):
		format = FormatTarGz
	case strings.HasSuffix(archivePath, ".zip"):
		format = FormatZip
	default:
		return nil, fmt.Errorf("unknown archive format: %s", filepath.Ext(archivePath))
	}

	switch format {
	case FormatTarGz:
		return s.importTarGz(archivePath, opts)
	case FormatZip:
		return s.importZip(archivePath, opts)
	default:
		return nil, fmt.Errorf("unsupported import format: %s", format)
	}
}

func (s *Storage) importTarGz(archivePath string, opts ImportOptions) (*Checkpoint, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open archive: %w", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	var manifest *ExportManifest
	var cp *Checkpoint
	fileContents := make(map[string][]byte)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar entry: %w", err)
		}

		data, err := readImportEntryLimited(tr, header.Name, maxImportEntrySize)
		if err != nil {
			return nil, err
		}

		if _, exists := fileContents[header.Name]; exists {
			return nil, fmt.Errorf("archive contains duplicate entry: %s", header.Name)
		}
		fileContents[header.Name] = data

		switch header.Name {
		case "MANIFEST.json":
			manifest = &ExportManifest{}
			if err := json.Unmarshal(data, manifest); err != nil {
				return nil, fmt.Errorf("failed to parse manifest: %w", err)
			}
		case MetadataFile:
			cp = &Checkpoint{}
			if err := json.Unmarshal(data, cp); err != nil {
				return nil, fmt.Errorf("failed to parse checkpoint: %w", err)
			}
		}
	}

	if cp == nil {
		return nil, fmt.Errorf("archive missing %s", MetadataFile)
	}

	// Verify checksums if requested
	if opts.VerifyChecksums {
		if err := verifyImportChecksums(fileContents, manifest); err != nil {
			return nil, err
		}
	}
	if err := validateImportedSessionState(fileContents, cp); err != nil {
		return nil, err
	}
	if err := validateImportedManifestMetadata(manifest, cp); err != nil {
		return nil, err
	}
	if err := validateImportedArchiveFiles(fileContents, cp); err != nil {
		return nil, err
	}

	sessionName := cp.SessionName

	// Apply overrides
	if opts.TargetSession != "" {
		sessionName = opts.TargetSession
	}
	cp.SessionName = sessionName

	// Apply TargetDir override or expand ${WORKING_DIR} placeholder
	if opts.TargetDir != "" {
		cp.WorkingDir = opts.TargetDir
	} else if cp.WorkingDir == "${WORKING_DIR}" {
		// No explicit target dir and checkpoint was exported with path rewriting
		// Use current working directory as default
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get current directory for path expansion: %w", err)
		}
		cp.WorkingDir = cwd
	}

	cpJSON, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal imported checkpoint: %w", err)
	}
	fileContents[MetadataFile] = cpJSON

	sessionJSON, err := json.MarshalIndent(cp.Session, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal imported session state: %w", err)
	}
	fileContents[SessionFile] = sessionJSON

	// Check for existing checkpoint
	cpDir, err := s.safeCheckpointDir(sessionName, cp.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid imported checkpoint metadata: %w", err)
	}
	if _, err := os.Stat(cpDir); err == nil && !opts.AllowOverwrite {
		return nil, fmt.Errorf("checkpoint %s already exists (use AllowOverwrite to replace)", cp.ID)
	}
	if opts.AllowOverwrite {
		if err := validateImportOverwrite(cpDir, fileContents); err != nil {
			return nil, err
		}
	}

	// Create checkpoint directory
	if err := os.MkdirAll(cpDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create checkpoint directory: %w", err)
	}

	// Write all files
	for name, data := range fileContents {
		if name == "MANIFEST.json" {
			continue
		}

		// Validate path doesn't escape checkpoint directory (path traversal protection)
		// First pass: textual validation before creating directories
		if !isPathWithinDir(cpDir, name) {
			return nil, fmt.Errorf("invalid path in archive (path traversal attempt): %s", name)
		}

		destPath := filepath.Join(cpDir, name)
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory for %s: %w", name, err)
		}

		// Second pass: symlink-safe validation after directories are created (TOCTOU protection)
		resolvedPath, err := isPathWithinDirResolved(cpDir, name)
		if err != nil {
			return nil, fmt.Errorf("invalid path in archive (symlink escape): %s", name)
		}

		if err := util.AtomicWriteFile(resolvedPath, data, 0600); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	return cp, nil
}

func (s *Storage) importZip(archivePath string, opts ImportOptions) (*Checkpoint, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip archive: %w", err)
	}
	defer zr.Close()

	var manifest *ExportManifest
	var cp *Checkpoint
	fileContents := make(map[string][]byte)

	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open %s: %w", f.Name, err)
		}

		data, readErr := readImportEntryLimited(rc, f.Name, maxImportEntrySize)
		closeErr := rc.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, fmt.Errorf("failed to close %s: %w", f.Name, closeErr)
		}

		if _, exists := fileContents[f.Name]; exists {
			return nil, fmt.Errorf("archive contains duplicate entry: %s", f.Name)
		}
		fileContents[f.Name] = data

		switch f.Name {
		case "MANIFEST.json":
			manifest = &ExportManifest{}
			if err := json.Unmarshal(data, manifest); err != nil {
				return nil, fmt.Errorf("failed to parse manifest: %w", err)
			}
		case MetadataFile:
			cp = &Checkpoint{}
			if err := json.Unmarshal(data, cp); err != nil {
				return nil, fmt.Errorf("failed to parse checkpoint: %w", err)
			}
		}
	}

	if cp == nil {
		return nil, fmt.Errorf("archive missing %s", MetadataFile)
	}

	// Verify checksums
	if opts.VerifyChecksums {
		if err := verifyImportChecksums(fileContents, manifest); err != nil {
			return nil, err
		}
	}
	if err := validateImportedSessionState(fileContents, cp); err != nil {
		return nil, err
	}
	if err := validateImportedManifestMetadata(manifest, cp); err != nil {
		return nil, err
	}
	if err := validateImportedArchiveFiles(fileContents, cp); err != nil {
		return nil, err
	}

	sessionName := cp.SessionName

	// Apply overrides
	if opts.TargetSession != "" {
		sessionName = opts.TargetSession
	}
	cp.SessionName = sessionName

	// Apply TargetDir override or expand ${WORKING_DIR} placeholder
	if opts.TargetDir != "" {
		cp.WorkingDir = opts.TargetDir
	} else if cp.WorkingDir == "${WORKING_DIR}" {
		// No explicit target dir and checkpoint was exported with path rewriting
		// Use current working directory as default
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get current directory for path expansion: %w", err)
		}
		cp.WorkingDir = cwd
	}

	cpJSON, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal imported checkpoint: %w", err)
	}
	fileContents[MetadataFile] = cpJSON

	sessionJSON, err := json.MarshalIndent(cp.Session, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal imported session state: %w", err)
	}
	fileContents[SessionFile] = sessionJSON

	// Check for existing
	cpDir, err := s.safeCheckpointDir(sessionName, cp.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid imported checkpoint metadata: %w", err)
	}
	if _, err := os.Stat(cpDir); err == nil && !opts.AllowOverwrite {
		return nil, fmt.Errorf("checkpoint %s already exists", cp.ID)
	}
	if opts.AllowOverwrite {
		if err := validateImportOverwrite(cpDir, fileContents); err != nil {
			return nil, err
		}
	}

	// Create checkpoint directory
	if err := os.MkdirAll(cpDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create checkpoint directory: %w", err)
	}

	// Write all files
	for name, data := range fileContents {
		if name == "MANIFEST.json" {
			continue
		}

		// Validate path doesn't escape checkpoint directory (path traversal protection)
		// First pass: textual validation before creating directories
		if !isPathWithinDir(cpDir, name) {
			return nil, fmt.Errorf("invalid path in archive (path traversal attempt): %s", name)
		}

		destPath := filepath.Join(cpDir, name)
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory for %s: %w", name, err)
		}

		// Second pass: symlink-safe validation after directories are created (TOCTOU protection)
		resolvedPath, err := isPathWithinDirResolved(cpDir, name)
		if err != nil {
			return nil, fmt.Errorf("invalid path in archive (symlink escape): %s", name)
		}

		if err := util.AtomicWriteFile(resolvedPath, data, 0600); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	return cp, nil
}

// Helper functions

func writeTarEntry(tw *tar.Writer, name string, data []byte) error {
	header := &tar.Header{
		Name:    name,
		Mode:    0644,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header for %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("failed to write tar content for %s: %w", name, err)
	}
	return nil
}

func writeZipEntry(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("failed to create zip entry for %s: %w", name, err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("failed to write zip content for %s: %w", name, err)
	}
	return nil
}

func readImportEntryLimited(r io.Reader, name string, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("invalid import size limit for %s: %d", name, limit)
	}

	reader := &io.LimitedReader{R: r, N: limit + 1}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", name, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("archive entry too large: %s exceeds %d bytes", name, limit)
	}
	return data, nil
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func redactSecrets(data []byte) []byte {
	redactionMu.RLock()
	cfg := redactionConfig
	redactionMu.RUnlock()

	var effective redaction.Config
	if cfg == nil {
		effective = redaction.DefaultConfig()
	} else {
		effective = *cfg
	}
	// Export redaction is explicitly requested by flag; always redact.
	effective.Mode = redaction.ModeRedact

	result := redaction.ScanAndRedact(string(data), effective)
	return []byte(result.Output)
}

func rewriteCheckpointForExport(cp *Checkpoint, opts ExportOptions) *Checkpoint {
	result := *cp
	result.Session.WindowLayouts = cloneWindowLayouts(cp.Session.WindowLayouts)
	if opts.RewritePaths && result.WorkingDir != "" {
		result.WorkingDir = "${WORKING_DIR}"
	}
	if !opts.IncludeScrollback && result.Session.Panes != nil {
		result.Session.Panes = make([]PaneState, len(cp.Session.Panes))
		copy(result.Session.Panes, cp.Session.Panes)
		for i := range result.Session.Panes {
			result.Session.Panes[i].ScrollbackFile = ""
			result.Session.Panes[i].ScrollbackLines = 0
			result.Session.Panes[i].Scrollback = nil
		}
	}
	if !opts.IncludeGitPatch {
		result.Git.PatchFile = ""
	}
	return &result
}

func validateImportOverwrite(cpDir string, fileContents map[string][]byte) error {
	if _, err := os.Stat(cpDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to inspect existing checkpoint directory: %w", err)
	}

	incomingFiles := make(map[string]struct{}, len(fileContents))
	for name := range fileContents {
		if name == "MANIFEST.json" {
			continue
		}
		incomingFiles[name] = struct{}{}
	}

	var staleFiles []string
	err := filepath.WalkDir(cpDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == cpDir || d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(cpDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		if _, ok := incomingFiles[relPath]; !ok {
			staleFiles = append(staleFiles, relPath)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to inspect existing checkpoint directory: %w", err)
	}

	if len(staleFiles) == 0 {
		return nil
	}
	if len(staleFiles) == 1 {
		return fmt.Errorf("overwrite would leave stale checkpoint artifact behind: %s", staleFiles[0])
	}
	return fmt.Errorf("overwrite would leave stale checkpoint artifacts behind: %s", strings.Join(staleFiles, ", "))
}

func verifyImportChecksums(fileContents map[string][]byte, manifest *ExportManifest) error {
	if manifest == nil {
		return fmt.Errorf("checksum verification requested but archive missing MANIFEST.json")
	}

	for file, data := range fileContents {
		if file == "MANIFEST.json" {
			continue
		}
		expectedSum, ok := manifest.Checksums[file]
		if !ok {
			return fmt.Errorf("manifest missing checksum for %s", file)
		}
		actualSum := sha256sum(data)
		if actualSum != expectedSum {
			return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", file, expectedSum, actualSum)
		}
	}

	for file := range manifest.Checksums {
		if file == "MANIFEST.json" {
			continue
		}
		if _, ok := fileContents[file]; !ok {
			return fmt.Errorf("manifest lists missing file: %s", file)
		}
	}

	return nil
}

func validateImportedSessionState(fileContents map[string][]byte, cp *Checkpoint) error {
	sessionData, ok := fileContents[SessionFile]
	if !ok {
		return fmt.Errorf("archive missing %s", SessionFile)
	}

	var session SessionState
	if err := json.Unmarshal(sessionData, &session); err != nil {
		return fmt.Errorf("failed to parse session state: %w", err)
	}

	metadataJSON, err := json.Marshal(cp.Session)
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint session state: %w", err)
	}
	sessionJSON, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal imported session state: %w", err)
	}
	if !bytes.Equal(metadataJSON, sessionJSON) {
		return fmt.Errorf("archive %s does not match %s session state", SessionFile, MetadataFile)
	}

	return nil
}

func validateImportedManifestMetadata(manifest *ExportManifest, cp *Checkpoint) error {
	if manifest == nil {
		return nil
	}
	if manifest.SessionName != "" && manifest.SessionName != cp.SessionName {
		return fmt.Errorf("manifest session name %q does not match %s session name %q", manifest.SessionName, MetadataFile, cp.SessionName)
	}
	if manifest.CheckpointID != "" && manifest.CheckpointID != cp.ID {
		return fmt.Errorf("manifest checkpoint id %q does not match %s checkpoint id %q", manifest.CheckpointID, MetadataFile, cp.ID)
	}
	if manifest.CheckpointName != "" && manifest.CheckpointName != cp.Name {
		return fmt.Errorf("manifest checkpoint name %q does not match %s checkpoint name %q", manifest.CheckpointName, MetadataFile, cp.Name)
	}

	return nil
}

func validateImportedArchiveFiles(fileContents map[string][]byte, cp *Checkpoint) error {
	expectedFiles := expectedManifestFiles(cp)
	for name := range fileContents {
		if name == "MANIFEST.json" {
			continue
		}
		if !isPathWithinDir(".", name) {
			return fmt.Errorf("invalid path in archive (path traversal attempt): %s", name)
		}
		if _, ok := expectedFiles[name]; !ok {
			return fmt.Errorf("archive contains unexpected file: %s", name)
		}
	}

	for _, pane := range cp.Session.Panes {
		if pane.ScrollbackFile == "" {
			continue
		}
		if _, ok := fileContents[pane.ScrollbackFile]; !ok {
			return fmt.Errorf("archive missing scrollback referenced by metadata: %s", pane.ScrollbackFile)
		}
	}
	if cp.Git.PatchFile != "" {
		if _, ok := fileContents[cp.Git.PatchFile]; !ok {
			return fmt.Errorf("archive missing git patch referenced by metadata: %s", cp.Git.PatchFile)
		}
	}
	if cp.Git.StatusFile != "" {
		if _, ok := fileContents[cp.Git.StatusFile]; !ok {
			return fmt.Errorf("archive missing git status referenced by metadata: %s", cp.Git.StatusFile)
		}
	}

	return nil
}

// isPathWithinDir checks if a path (after cleaning) stays within the base directory.
// This prevents path traversal attacks like "../../../etc/passwd".
func isPathWithinDir(baseDir, targetPath string) bool {
	// Clean and make absolute
	cleanBase := filepath.Clean(baseDir)
	fullPath := filepath.Join(cleanBase, targetPath)
	cleanPath := filepath.Clean(fullPath)

	// The clean path must be within or equal to the base directory
	// Use filepath.Rel to check - if it requires ".." then it's outside
	rel, err := filepath.Rel(cleanBase, cleanPath)
	if err != nil {
		return false
	}

	// If the relative path starts with "..", it's outside the base
	return !strings.HasPrefix(rel, "..")
}

// isPathWithinDirResolved validates a path after resolving symlinks to prevent TOCTOU attacks.
// Returns the resolved absolute path if valid, or an error if the path escapes the base directory.
func isPathWithinDirResolved(baseDir, targetPath string) (string, error) {
	// First do textual validation (fast path)
	if !isPathWithinDir(baseDir, targetPath) {
		return "", fmt.Errorf("path escapes base directory: %s", targetPath)
	}

	// Resolve symlinks in the base directory to get canonical path
	resolvedBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		// If base doesn't exist yet, fall back to Clean
		resolvedBase = filepath.Clean(baseDir)
	}

	// Build the full path
	fullPath := filepath.Join(resolvedBase, targetPath)

	// For the target, resolve parent directories but not the final component
	// (since we're about to create it). This catches symlink attacks in intermediate dirs.
	parentDir := filepath.Dir(fullPath)

	// Try to resolve symlinks in the parent path (if it exists)
	resolvedParent, err := filepath.EvalSymlinks(parentDir)
	if err == nil {
		// Verify the resolved parent is still within base
		relParent, err := filepath.Rel(resolvedBase, resolvedParent)
		if err != nil || strings.HasPrefix(relParent, "..") {
			return "", fmt.Errorf("symlink escape detected in path: %s", targetPath)
		}
		// Reconstruct full path with resolved parent
		fullPath = filepath.Join(resolvedParent, filepath.Base(fullPath))
	}

	// Final validation: clean path must be within resolved base
	cleanPath := filepath.Clean(fullPath)
	rel, err := filepath.Rel(resolvedBase, cleanPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("resolved path escapes base directory: %s", targetPath)
	}

	return cleanPath, nil
}

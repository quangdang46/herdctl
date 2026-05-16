package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/Dicklesworthstone/ntm/internal/config"
	"github.com/Dicklesworthstone/ntm/internal/output"
	"github.com/Dicklesworthstone/ntm/internal/redaction"
	"github.com/Dicklesworthstone/ntm/internal/util"
)

type RedactPreviewFinding struct {
	Category redaction.Category `json:"category"`
	Redacted string             `json:"redacted"`
	Start    int                `json:"start"`
	End      int                `json:"end"`
	Line     int                `json:"line,omitempty"`
	Column   int                `json:"column,omitempty"`
}

type RedactPreviewResponse struct {
	output.TimestampedResponse

	Source   string                 `json:"source"`         // text|file
	Path     string                 `json:"path,omitempty"` // only when source=file
	InputLen int                    `json:"input_len"`      // bytes
	Findings []RedactPreviewFinding `json:"findings"`       // never includes raw matches
	Output   string                 `json:"output"`         // redacted output (safe)
}

func newRedactCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "redact",
		Short: "Redaction utilities",
		Long: `Redaction utilities for previewing and debugging secret detection.

These commands NEVER print raw matched secrets. Output is always safe-redacted.`,
	}

	cmd.AddCommand(
		newRedactPreviewCmd(),
		newRedactPrepareMailCmd(),
	)

	return cmd
}

// =============================================================================
// `ntm redact prepare-mail` (ntm#126)
// =============================================================================
//
// Token-handle contract for Agent Mail prepare/send workflows. Raw token
// material is read from an env var or file by `prepare-mail` itself,
// scanned for redaction findings, and stashed in an in-process store
// keyed by a random handle. The caller then invokes `ntm mail send …
// --prepared-redaction <handle>` and the raw bytes never leave the
// redaction surface (no wrapper logs, no prompt packets, no dry-run
// output carry the token text).
//
// Storage is in-process by design — restart kills the handle, which is
// the right behavior for a send-once flow. Each handle has a 10-minute
// TTL so a forgotten handle doesn't keep a token resident forever.

const preparedRedactionTTL = 10 * time.Minute

type preparedRedactionEntry struct {
	redactedBody string
	findings     []RedactPreviewFinding
	createdAt    time.Time
	// rawSecret is held only so the eventual send-side consumer can
	// reach back through `consumePreparedRedaction` and use the
	// original text. We deliberately do NOT expose this through any
	// JSON or log path; the only way to retrieve it is the
	// consume-and-drop API below.
	rawSecret string
}

var (
	preparedRedactionsMu sync.Mutex
	preparedRedactions   = map[string]*preparedRedactionEntry{}
)

// stashPreparedRedaction stores the raw secret + its redaction
// summary under a freshly-generated handle. Sweeps expired entries
// opportunistically. Returns the handle.
func stashPreparedRedaction(raw, redactedBody string, findings []RedactPreviewFinding) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate handle: %w", err)
	}
	handle := "rh_" + hex.EncodeToString(b[:])
	preparedRedactionsMu.Lock()
	defer preparedRedactionsMu.Unlock()
	now := time.Now()
	for h, e := range preparedRedactions {
		if now.Sub(e.createdAt) > preparedRedactionTTL {
			delete(preparedRedactions, h)
		}
	}
	preparedRedactions[handle] = &preparedRedactionEntry{
		redactedBody: redactedBody,
		findings:     findings,
		createdAt:    now,
		rawSecret:    raw,
	}
	return handle, nil
}

// consumePreparedRedaction is the send-side accessor. Returns the raw
// secret bytes ONCE and drops the entry from the store. The returned
// `redacted` value is what should be surfaced in JSON envelopes and
// logs; `raw` is intended only for the wire payload to the downstream
// transport. Returns an error when the handle is missing or expired.
func consumePreparedRedaction(handle string) (raw, redacted string, findings []RedactPreviewFinding, err error) {
	preparedRedactionsMu.Lock()
	defer preparedRedactionsMu.Unlock()
	entry, ok := preparedRedactions[handle]
	if !ok {
		return "", "", nil, fmt.Errorf("prepared-redaction handle %q not found (expired or never created)", handle)
	}
	delete(preparedRedactions, handle)
	if time.Since(entry.createdAt) > preparedRedactionTTL {
		return "", "", nil, fmt.Errorf("prepared-redaction handle %q expired", handle)
	}
	return entry.rawSecret, entry.redactedBody, entry.findings, nil
}

// RedactPrepareMailResponse is the JSON envelope returned by
// `ntm redact prepare-mail`. The raw token never appears; callers
// receive a handle they can pass to `ntm mail send --prepared-redaction`.
type RedactPrepareMailResponse struct {
	output.TimestampedResponse

	Handle       string                 `json:"handle"`
	ExpiresIn    string                 `json:"expires_in"`
	InputSource  string                 `json:"input_source"` // env|file
	InputLen     int                    `json:"input_len"`
	Findings     []RedactPreviewFinding `json:"findings"`
	RedactedView string                 `json:"redacted_view"`
}

func newRedactPrepareMailCmd() *cobra.Command {
	var (
		senderTokenEnv  string
		senderTokenFile string
	)

	cmd := &cobra.Command{
		Use:   "prepare-mail",
		Short: "Prepare an Agent Mail payload with a redaction handle (ntm#126)",
		Long: `Read sensitive sender/token material from --sender-token-env or
--sender-token-file, scan it for redaction findings, and return a
short-lived handle. The raw bytes never leave this process — they are
never echoed to stdout, never logged, never serialized into the JSON
envelope. The caller then passes the handle to:

  ntm mail send <session> --prepared-redaction <handle>

…which consumes the handle exactly once. Handles expire after 10
minutes if not consumed.

Examples:
  SENDER_TOKEN=secret-value ntm redact prepare-mail --sender-token-env=SENDER_TOKEN --json
  ntm redact prepare-mail --sender-token-file=./secret.txt --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env := senderTokenEnv
			file := senderTokenFile
			senderTokenEnv = ""
			senderTokenFile = ""

			if env == "" && file == "" {
				return fmt.Errorf("must provide exactly one of --sender-token-env or --sender-token-file")
			}
			if env != "" && file != "" {
				return fmt.Errorf("flags --sender-token-env and --sender-token-file are mutually exclusive")
			}

			var (
				raw    string
				source string
			)
			if env != "" {
				source = "env"
				raw = os.Getenv(env)
				if raw == "" {
					return fmt.Errorf("environment variable %q is empty or unset", env)
				}
			} else {
				source = "file"
				p, err := filepath.Abs(util.ExpandPath(file))
				if err != nil {
					return fmt.Errorf("resolve --sender-token-file %q: %w", file, err)
				}
				b, err := os.ReadFile(p)
				if err != nil {
					return fmt.Errorf("read %q: %w", p, err)
				}
				raw = string(b)
			}

			if cfg == nil {
				cfg = config.Default()
			}
			redactCfg := cfg.Redaction.ToRedactionLibConfig()
			redactCfg.Mode = redaction.ModeRedact

			res := redaction.ScanAndRedact(raw, redactCfg)
			redaction.AddLineInfo(raw, res.Findings)
			findings := make([]RedactPreviewFinding, 0, len(res.Findings))
			for _, f := range res.Findings {
				findings = append(findings, RedactPreviewFinding{
					Category: f.Category,
					Redacted: f.Redacted,
					Start:    f.Start,
					End:      f.End,
					Line:     f.Line,
					Column:   f.Column,
				})
			}

			handle, err := stashPreparedRedaction(raw, res.Output, findings)
			if err != nil {
				return err
			}

			resp := RedactPrepareMailResponse{
				TimestampedResponse: output.NewTimestamped(),
				Handle:              handle,
				ExpiresIn:           preparedRedactionTTL.String(),
				InputSource:         source,
				InputLen:            len(raw),
				Findings:            findings,
				RedactedView:        res.Output,
			}

			if IsJSONOutput() {
				return output.PrintJSON(resp)
			}
			fmt.Printf("handle: %s\n", resp.Handle)
			fmt.Printf("expires in: %s\n", resp.ExpiresIn)
			fmt.Printf("input source: %s (%d bytes)\n", resp.InputSource, resp.InputLen)
			fmt.Printf("findings: %d\n", len(resp.Findings))
			for _, f := range resp.Findings {
				fmt.Printf("- %s %s\n", f.Category, f.Redacted)
			}
			fmt.Println()
			fmt.Println("Pass the handle to: ntm mail send <session> --prepared-redaction <handle>")
			return nil
		},
	}

	cmd.Flags().StringVar(&senderTokenEnv, "sender-token-env", "", "Environment variable holding the sensitive token to prepare")
	cmd.Flags().StringVar(&senderTokenFile, "sender-token-file", "", "File path holding the sensitive token to prepare")

	return cmd
}

func newRedactPreviewCmd() *cobra.Command {
	var (
		text string
		file string
	)

	cmd := &cobra.Command{
		Use:   "preview",
		Short: "Preview redaction findings and safe-redacted output",
		Long: `Preview secret detection on input text (or a file) and print:
- A list of findings (category + position + placeholder)
- A safe-redacted output

This command never prints raw matched secrets, even if your configured redaction mode is warn/off.

Examples:
  ntm redact preview --text "password=hunter2hunter2"
  ntm redact preview --file ./notes.txt
  ntm redact preview --text "..." --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Cobra commands are reused across tests within the same process; flags bound via
			// StringVar can retain values between Execute() calls when a flag is omitted.
			// Snapshot and then reset to keep behavior deterministic for both tests and CLI.
			currentText := text
			currentFile := file
			text = ""
			file = ""

			if currentText == "" && currentFile == "" {
				return fmt.Errorf("must provide exactly one of --text or --file")
			}
			if currentText != "" && currentFile != "" {
				return fmt.Errorf("flags --text and --file are mutually exclusive")
			}

			source := "text"
			absPath := ""
			input := currentText
			if currentFile != "" {
				source = "file"
				p := util.ExpandPath(currentFile)
				abs, err := filepath.Abs(p)
				if err != nil {
					return fmt.Errorf("resolve --file %q: %w", currentFile, err)
				}
				b, err := os.ReadFile(abs)
				if err != nil {
					return fmt.Errorf("read %q: %w", abs, err)
				}
				absPath = abs
				input = string(b)
			}

			if cfg == nil {
				cfg = config.Default()
			}

			// Always compute a safe-redacted output for preview. This prevents accidental leaks
			// when the global config/flags are set to warn/off.
			redactCfg := cfg.Redaction.ToRedactionLibConfig()
			redactCfg.Mode = redaction.ModeRedact

			res := redaction.ScanAndRedact(input, redactCfg)
			redaction.AddLineInfo(input, res.Findings)

			findings := make([]RedactPreviewFinding, 0, len(res.Findings))
			for _, f := range res.Findings {
				findings = append(findings, RedactPreviewFinding{
					Category: f.Category,
					Redacted: f.Redacted,
					Start:    f.Start,
					End:      f.End,
					Line:     f.Line,
					Column:   f.Column,
				})
			}

			resp := RedactPreviewResponse{
				TimestampedResponse: output.NewTimestamped(),
				Source:              source,
				Path:                absPath,
				InputLen:            len(input),
				Findings:            findings,
				Output:              res.Output,
			}

			if IsJSONOutput() {
				return output.PrintJSON(resp)
			}

			if resp.Source == "file" {
				fmt.Printf("Source: %s\n", resp.Path)
			} else {
				fmt.Println("Source: text")
			}
			fmt.Printf("Findings: %d\n", len(resp.Findings))
			for _, f := range resp.Findings {
				if f.Line > 0 && f.Column > 0 {
					fmt.Printf("- %d:%d %s %s\n", f.Line, f.Column, f.Category, f.Redacted)
					continue
				}
				fmt.Printf("- %d-%d %s %s\n", f.Start, f.End, f.Category, f.Redacted)
			}
			fmt.Println()
			fmt.Println("Redacted output:")
			fmt.Print(resp.Output)
			if resp.Output != "" && !strings.HasSuffix(resp.Output, "\n") {
				fmt.Println()
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&text, "text", "", "Input text to scan/redact (mutually exclusive with --file)")
	cmd.Flags().StringVar(&file, "file", "", "File to scan/redact (mutually exclusive with --text)")

	return cmd
}

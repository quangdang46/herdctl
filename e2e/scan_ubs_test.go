//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM commands.
// scan_ubs_test.go validates UBS diff warnings surface in JSON output.
//
// Bead: bd-8pmq6 - Investigate UBS diff warning output (details not surfaced)
package e2e

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"testing"
)

type scanJSONEnvelope struct {
	Scan struct {
		Warnings []string `json:"warnings"`
	} `json:"scan"`
}

func TestE2E_UBSDiffWarningsSurfaceInJSON(t *testing.T) {
	CommonE2EPrerequisites(t)

	if _, err := exec.LookPath("ubs"); err != nil {
		t.Skip("ubs not installed; skipping UBS diff warning test")
	}
	ntmPath, err := lookPathCLI()
	if err != nil {
		t.Skip("ntm not found on PATH")
	}

	cmd := exec.Command(ntmPath, "scan", "--json", "--diff", "--only=python")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			slog.Warn(fmt.Sprintf("[E2E-UBS] ntm scan exited non-zero: %d", exitErr.ExitCode()))
		} else {
			t.Fatalf("[E2E-UBS] ntm scan failed: %v", err)
		}
	}

	var payload scanJSONEnvelope
	if err := json.Unmarshal(output, &payload); err != nil {
		t.Fatalf("[E2E-UBS] JSON parse failed: %v output=%s", err, string(output))
	}

	if len(payload.Scan.Warnings) == 0 {
		t.Fatalf("[E2E-UBS] expected diff warnings in JSON output")
	}

	slog.Info(fmt.Sprintf("[E2E-UBS] warnings=%d first=%q", len(payload.Scan.Warnings), payload.Scan.Warnings[0]))
}

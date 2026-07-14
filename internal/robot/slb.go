// Package robot provides machine-readable output for AI agents.
// slb.go implements the --robot-slb-* commands.
package robot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/tools"
)

// SLBPendingOutput represents the output for --robot-slb-pending.
type SLBPendingOutput struct {
	RobotResponse
	Count   int             `json:"count"`
	Pending json.RawMessage `json:"pending"`
}

// SLBActionOutput represents the output for --robot-slb-approve and --robot-slb-deny.
type SLBActionOutput struct {
	RobotResponse
	RequestID string          `json:"request_id"`
	Result    json.RawMessage `json:"result,omitempty"`
}

// GetSLBPending returns pending SLB approval requests.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetSLBPending() (*SLBPendingOutput, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adapter := tools.NewSLBAdapter()
	output := &SLBPendingOutput{
		RobotResponse: NewRobotResponse(true),
		Count:         0,
		Pending:       json.RawMessage("[]"),
	}

	if _, installed := adapter.Detect(); !installed {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("slb not installed"),
			ErrCodeDependencyMissing,
			"Install slb to enable two-person approvals",
		)
		return output, nil
	}

	raw, err := adapter.Pending(ctx)
	if err != nil {
		code := ErrCodeInternalError
		hint := "Run 'slb pending --json' to diagnose"
		if errors.Is(err, tools.ErrTimeout) {
			code = ErrCodeTimeout
			hint = "SLB timed out; try again or check daemon status"
		}
		output.RobotResponse = NewErrorResponse(err, code, hint)
		return output, nil
	}

	if len(raw) > 0 {
		output.Pending = raw
	}
	output.Count = countJSONArray(output.Pending)
	return output, nil
}

// PrintSLBPending outputs pending SLB approvals as JSON/TOON.
// This is a thin wrapper around GetSLBPending() for CLI output.
func PrintSLBPending() error {
	output, err := GetSLBPending()
	if err != nil {
		return err
	}
	return outputJSON(output)
}

// GetSLBApprove approves a pending SLB request.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetSLBApprove(requestID string) (*SLBActionOutput, error) {
	requestID = strings.TrimSpace(requestID)
	output := &SLBActionOutput{
		RobotResponse: NewRobotResponse(true),
		RequestID:     requestID,
	}

	if requestID == "" {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("missing request id"),
			ErrCodeInvalidFlag,
			"Provide a request ID: herdctl --robot-slb-approve=req-123",
		)
		return output, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adapter := tools.NewSLBAdapter()
	if _, installed := adapter.Detect(); !installed {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("slb not installed"),
			ErrCodeDependencyMissing,
			"Install slb to enable two-person approvals",
		)
		return output, nil
	}

	raw, err := adapter.Approve(ctx, requestID)
	if err != nil {
		code := ErrCodeInternalError
		hint := "Run 'slb approve <id> --json' to diagnose"
		if errors.Is(err, tools.ErrTimeout) {
			code = ErrCodeTimeout
			hint = "SLB timed out; try again or check daemon status"
		}
		output.RobotResponse = NewErrorResponse(err, code, hint)
		return output, nil
	}

	output.Result = raw
	return output, nil
}

// PrintSLBApprove outputs the SLB approval response as JSON/TOON.
// This is a thin wrapper around GetSLBApprove() for CLI output.
func PrintSLBApprove(requestID string) error {
	output, err := GetSLBApprove(requestID)
	if err != nil {
		return err
	}
	return outputJSON(output)
}

// GetSLBDeny denies a pending SLB request.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetSLBDeny(requestID, reason string) (*SLBActionOutput, error) {
	requestID = strings.TrimSpace(requestID)
	output := &SLBActionOutput{
		RobotResponse: NewRobotResponse(true),
		RequestID:     requestID,
	}

	if requestID == "" {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("missing request id"),
			ErrCodeInvalidFlag,
			"Provide a request ID: herdctl --robot-slb-deny=req-123 --reason='Too risky'",
		)
		return output, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adapter := tools.NewSLBAdapter()
	if _, installed := adapter.Detect(); !installed {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("slb not installed"),
			ErrCodeDependencyMissing,
			"Install slb to enable two-person approvals",
		)
		return output, nil
	}

	raw, err := adapter.Deny(ctx, requestID, strings.TrimSpace(reason))
	if err != nil {
		code := ErrCodeInternalError
		hint := "Run 'slb deny <id> --json' to diagnose"
		if errors.Is(err, tools.ErrTimeout) {
			code = ErrCodeTimeout
			hint = "SLB timed out; try again or check daemon status"
		}
		output.RobotResponse = NewErrorResponse(err, code, hint)
		return output, nil
	}

	output.Result = raw
	return output, nil
}

// PrintSLBDeny outputs the SLB denial response as JSON/TOON.
// This is a thin wrapper around GetSLBDeny() for CLI output.
func PrintSLBDeny(requestID, reason string) error {
	output, err := GetSLBDeny(requestID, reason)
	if err != nil {
		return err
	}
	return outputJSON(output)
}

func countJSONArray(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return 0
	}
	return len(list)
}

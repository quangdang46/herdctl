package herdr

import (
	"errors"
	"fmt"
)

var (
	// ErrNotSupported means the operation has no Herdr equivalent yet.
	// Callers must not treat this as a transient failure.
	ErrNotSupported = errors.New("herdr backend: operation not supported")

	// ErrUnavailable means the herdr binary or server is not reachable.
	ErrUnavailable = errors.New("herdr backend: herdr unavailable")

	// ErrNotFound means a session/workspace/pane could not be resolved.
	ErrNotFound = errors.New("herdr backend: not found")

	// ErrInvalidName means a session/label failed validation.
	ErrInvalidName = errors.New("herdr backend: invalid name")

	// ErrConflict means a create raced or a label is already bound differently.
	ErrConflict = errors.New("herdr backend: conflict")
)

// UnsupportedError carries the operation name for clearer diagnostics.
type UnsupportedError struct {
	Op     string
	Detail string
}

func (e *UnsupportedError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("%v: %s", ErrNotSupported, e.Op)
	}
	return fmt.Sprintf("%v: %s (%s)", ErrNotSupported, e.Op, e.Detail)
}

func (e *UnsupportedError) Is(target error) bool {
	return target == ErrNotSupported
}

func notSupported(op, detail string) error {
	return &UnsupportedError{Op: op, Detail: detail}
}

// CommandError is returned when a herdr CLI invocation fails.
type CommandError struct {
	Args   []string
	Status int
	Stdout string
	Stderr string
	Err    error
}

func (e *CommandError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("herdr %v: %v", e.Args, e.Err)
	}
	if e.Stderr != "" {
		return fmt.Sprintf("herdr %v failed (status=%d): %s", e.Args, e.Status, e.Stderr)
	}
	return fmt.Sprintf("herdr %v failed (status=%d)", e.Args, e.Status)
}

func (e *CommandError) Unwrap() error { return e.Err }

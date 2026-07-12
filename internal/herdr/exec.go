package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultTimeout = 15 * time.Second

// cliResponse is the common Herdr CLI envelope: {"id":"...","result":{...}} or {"error":...}.
type cliResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *cliErrorBody   `json:"error"`
}

type cliErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

func (c *Client) binary() string {
	if c.Binary != "" {
		return c.Binary
	}
	if env := os.Getenv("HERDR_BIN_PATH"); env != "" {
		return env
	}
	return "herdr"
}

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return defaultTimeout
}

// runJSON executes `herdr <args...>` and decodes the CLI JSON envelope.
// resultOut, when non-nil, is json.Unmarshal'd from the result object.
func (c *Client) runJSON(ctx context.Context, resultOut any, args ...string) error {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), c.timeout())
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, c.binary(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	outStr := strings.TrimSpace(stdout.String())
	errStr := strings.TrimSpace(stderr.String())

	if err != nil {
		// Prefer JSON error body when present on stdout/stderr.
		if parsed := tryParseCLIError(outStr, errStr); parsed != nil {
			return &CommandError{
				Args:   args,
				Status: exitStatus(err),
				Stdout: outStr,
				Stderr: errStr,
				Err:    parsed,
			}
		}
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return &CommandError{
			Args:   args,
			Status: exitStatus(err),
			Stdout: outStr,
			Stderr: errStr,
			Err:    err,
		}
	}

	if outStr == "" {
		// Some pane I/O commands succeed with no JSON body. Treat empty stdout as
		// success when the caller does not need a decoded result object, or when it
		// only needs an ok ack (okResult zero value).
		if resultOut == nil {
			return nil
		}
		switch resultOut.(type) {
		case *okResult:
			return nil
		default:
			return fmt.Errorf("%w: empty stdout for herdr %v", ErrUnavailable, args)
		}
	}

	var envelope cliResponse
	if err := json.Unmarshal([]byte(outStr), &envelope); err != nil {
		return fmt.Errorf("decode herdr response for %v: %w\nstdout=%s", args, err, outStr)
	}
	if envelope.Error != nil {
		return &CommandError{
			Args:   args,
			Stdout: outStr,
			Err: fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message),
		}
	}
	if resultOut == nil {
		return nil
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return fmt.Errorf("herdr %v: missing result", args)
	}
	if err := json.Unmarshal(envelope.Result, resultOut); err != nil {
		return fmt.Errorf("decode herdr result for %v: %w\nresult=%s", args, err, string(envelope.Result))
	}
	return nil
}

func tryParseCLIError(stdout, stderr string) error {
	for _, raw := range []string{stdout, stderr} {
		raw = strings.TrimSpace(raw)
		if raw == "" || raw[0] != '{' {
			continue
		}
		var envelope cliResponse
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			continue
		}
		if envelope.Error != nil {
			return fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
		}
	}
	return nil
}

func exitStatus(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// lookPath reports whether the herdr binary is on PATH / HERDR_BIN_PATH.
func (c *Client) lookPath() (string, error) {
	bin := c.binary()
	if strings.Contains(bin, string(os.PathSeparator)) {
		if _, err := os.Stat(bin); err != nil {
			return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return bin, nil
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return path, nil
}

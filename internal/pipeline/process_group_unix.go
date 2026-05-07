package pipeline

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const commandCancelGracePeriod = 5 * time.Second

type commandCleanupResult struct {
	Err        error
	Cancelled  bool
	SignalSent string
}

func configureCommandProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func waitCommandWithProcessGroupCleanup(ctx context.Context, cmd *exec.Cmd) commandCleanupResult {
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return commandCleanupResult{Err: err}
	case <-ctx.Done():
		result := commandCleanupResult{
			Cancelled:  true,
			SignalSent: "SIGTERM",
		}
		_ = signalCommandProcessGroup(cmd, syscall.SIGTERM)

		select {
		case err := <-done:
			result.Err = err
			if result.Err == nil {
				result.Err = ctx.Err()
			}
			return result
		case <-time.After(commandCancelGracePeriod):
			result.SignalSent = "SIGTERM,SIGKILL"
			_ = signalCommandProcessGroup(cmd, syscall.SIGKILL)
			err := <-done
			result.Err = err
			if result.Err == nil {
				result.Err = ctx.Err()
			}
			return result
		}
	}
}

func signalCommandProcessGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, sig)
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	if sig == syscall.SIGKILL {
		return cmd.Process.Kill()
	}
	return cmd.Process.Signal(sig)
}

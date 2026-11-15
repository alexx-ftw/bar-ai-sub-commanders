//go:build unix

package codex

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Codex may be a wrapper that starts the actual CLI; stop both on cancellation.
func isolateProcess(cmd *exec.Cmd) error {
	cmd.WaitDelay = time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return nil
}

func cleanupProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

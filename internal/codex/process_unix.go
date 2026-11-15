//go:build unix

package codex

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Codex may be a wrapper that starts the actual CLI; stop both on cancellation.
func isolateProcess(cmd *exec.Cmd) error {
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

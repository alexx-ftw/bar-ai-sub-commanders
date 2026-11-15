//go:build !unix

package codex

import (
	"errors"
	"os/exec"
)

func isolateProcess(*exec.Cmd) error {
	return errors.New("codex backend requires process-group cancellation support")
}

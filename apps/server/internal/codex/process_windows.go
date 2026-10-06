//go:build windows

package codex

import (
	"errors"
	"os"
	"os/exec"
)

func prepareProcessGroup(_ *exec.Cmd) {}

func killProcessGroup(process *os.Process) error {
	if process == nil {
		return nil
	}
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

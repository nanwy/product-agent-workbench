//go:build windows

package targetruntime

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureProcessGroup(_ *exec.Cmd) {}

func signalProcessGroup(process *os.Process, _ syscall.Signal) error {
	if process == nil {
		return nil
	}
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

//go:build windows

package verification

import (
	"os/exec"
	"time"
)

func configureCommandTermination(command *exec.Cmd) {
	command.WaitDelay = time.Second
}

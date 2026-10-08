//go:build !windows

package verification

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// configureCommandTermination 让取消和超时作用于整个进程组。
// pnpm 往往还会启动 tsc/vitest 子进程，只杀父进程会留下后台任务继续修改构建目录。
func configureCommandTermination(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	command.WaitDelay = time.Second
}

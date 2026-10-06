//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package codex

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func prepareProcessGroup(command *exec.Cmd) {
	// app-server 后续会启动工具子进程。单独的进程组确保强制关闭时不会只杀掉父进程，
	// 却把正在修改 Worktree 的命令遗留在后台。
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessGroup(process *os.Process) error {
	if process == nil {
		return nil
	}
	if err := syscall.Kill(-process.Pid, syscall.SIGKILL); err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// gitCommandError 保留退出码和受限输出，调用方可以区分“引用不存在”和真正的 Git 故障。
type gitCommandError struct {
	output string
	err    error
}

func (err *gitCommandError) Error() string {
	if err.output == "" {
		return fmt.Sprintf("git 命令失败: %v", err.err)
	}
	return fmt.Sprintf("git 命令失败: %v: %s", err.err, err.output)
}

func (err *gitCommandError) Unwrap() error {
	return err.err
}

func (m *GitManager) git(ctx context.Context, cwd string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 不经过 shell：分支、路径和 revision 始终作为独立参数传给 Git，避免命令注入。
	command := exec.CommandContext(ctx, m.gitBinary, args...)
	command.Dir = cwd
	command.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_EDITOR=true",
		"GIT_SEQUENCE_EDITOR=true",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, &gitCommandError{
			output: truncateGitOutput(output),
			err:    err,
		}
	}
	return output, nil
}

func gitExitCode(err error) (int, bool) {
	var commandError *gitCommandError
	if !errors.As(err, &commandError) {
		return 0, false
	}
	var exitError *exec.ExitError
	if !errors.As(commandError.err, &exitError) {
		return 0, false
	}
	return exitError.ExitCode(), true
}

func truncateGitOutput(output []byte) string {
	const maximum = 8 * 1024
	if len(output) > maximum {
		output = output[:maximum]
	}
	return strings.TrimSpace(string(output))
}

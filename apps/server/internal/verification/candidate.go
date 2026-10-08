package verification

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Candidate 是验证开始时读取到的 Git 真值。
type Candidate struct {
	Revision         string
	Dirty            bool
	MissingRevisions []string
}

// CandidateInspector 检查命令究竟运行在哪一个候选版本上。
type CandidateInspector interface {
	Inspect(
		ctx context.Context,
		workspacePath string,
		expectedRevisions []string,
	) (Candidate, error)
}

// GitCandidateInspector 使用参数数组读取 HEAD 和工作区状态，不经过 Shell。
type GitCandidateInspector struct {
	executable string
}

func NewGitCandidateInspector(executable string) (*GitCandidateInspector, error) {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return nil, ErrInvalidDependencies
	}
	return &GitCandidateInspector{executable: executable}, nil
}

func (i *GitCandidateInspector) Inspect(
	ctx context.Context,
	workspacePath string,
	expectedRevisions []string,
) (Candidate, error) {
	revision, err := i.git(ctx, workspacePath, "rev-parse", "HEAD")
	if err != nil {
		return Candidate{}, fmt.Errorf("读取候选 revision: %w", err)
	}
	status, err := i.git(ctx, workspacePath, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return Candidate{}, fmt.Errorf("读取候选工作区状态: %w", err)
	}
	candidate := Candidate{
		Revision:         strings.TrimSpace(revision),
		Dirty:            strings.TrimSpace(status) != "",
		MissingRevisions: make([]string, 0),
	}
	for _, revision := range expectedRevisions {
		_, err := i.git(ctx, workspacePath, "merge-base", "--is-ancestor", revision, "HEAD")
		if err == nil {
			continue
		}
		if exitCode, ok := commandExitCode(err); ok && exitCode == 1 {
			candidate.MissingRevisions = append(candidate.MissingRevisions, revision)
			continue
		}
		return Candidate{}, fmt.Errorf("检查已整合 revision %q: %w", revision, err)
	}
	return candidate, nil
}

func commandExitCode(err error) (int, bool) {
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		return 0, false
	}
	return exitError.ExitCode(), true
}

func (i *GitCandidateInspector) git(
	ctx context.Context,
	workspacePath string,
	args ...string,
) (string, error) {
	commandContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(commandContext, i.executable, args...)
	command.Dir = workspacePath
	command.Env = validationEnvironment(Command{
		Environment: EnvironmentProduction,
		ProxyPolicy: ProxyPolicyOmit,
	})
	configureCommandTermination(command)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s: %w", args[0], redactSecrets(strings.TrimSpace(string(output))), err)
	}
	return string(output), nil
}

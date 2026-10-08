package verification

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ReportRepository 保存每一次验证尝试，旧报告不能被覆盖。
type ReportRepository interface {
	SaveReport(ctx context.Context, report Report) error
	LatestReport(
		ctx context.Context,
		requirementID string,
		ticketVersionID string,
	) (Report, error)
}

type IDGenerator func() string
type Clock func() time.Time

type ServiceDependencies struct {
	Commands  []Command
	Executor  CommandExecutor
	Candidate CandidateInspector
	Reports   ReportRepository
	Runtime   RuntimeProbe
	NewID     IDGenerator
	Now       Clock
}

// Service 把“候选版本检查 + 多条宿主命令”收敛成一份持久化 Matrix。
//
// 前端类比：它类似一个 mutation orchestration hook：依次调用多个 adapter，把每个结果映射
// 成统一 DTO，最后计算 `records.some(blocking)`。它不负责 HTTP，也不修改 Requirement。
type Service struct {
	commands  []Command
	executor  CommandExecutor
	candidate CandidateInspector
	reports   ReportRepository
	runtime   RuntimeProbe
	newID     IDGenerator
	now       Clock
}

type VerifyRequest struct {
	RequirementID     string
	TicketVersionID   string
	WorkspacePath     string
	ExpectedRevisions []string
}

func NewService(dependencies ServiceDependencies) (*Service, error) {
	if dependencies.Executor == nil || dependencies.Candidate == nil || dependencies.Reports == nil ||
		dependencies.NewID == nil || dependencies.Now == nil {
		return nil, ErrInvalidDependencies
	}
	commands := append([]Command{}, dependencies.Commands...)
	if err := validateCommands(commands); err != nil {
		return nil, err
	}
	for _, command := range commands {
		if command.RequiresRuntime && dependencies.Runtime == nil {
			return nil, ErrInvalidDependencies
		}
	}
	return &Service{
		commands:  commands,
		executor:  dependencies.Executor,
		candidate: dependencies.Candidate,
		reports:   dependencies.Reports,
		runtime:   dependencies.Runtime,
		newID:     dependencies.NewID,
		now:       dependencies.Now,
	}, nil
}

// Verify 执行完整验证并保存报告。
//
// 引导步骤：
//  1. 校验身份、受管 Worktree 路径和候选 revision 集合；
//  2. 读取 Git HEAD/dirty 状态，生成第一条候选证据；
//  3. 串行执行命令。构建和测试共享目录时，并行可能争用同一个输出目录；
//  4. 把 exit=0 映射为 verified，普通非零映射为 failed，超时/无法启动映射为 not-run；
//  5. BuildMatrix 统一校验，再用 BlocksDelivery 计算 passed/blocked；
//  6. 无论通过还是阻塞都保存报告。阻塞是业务结果，不是 Repository error。
func (s *Service) Verify(ctx context.Context, request VerifyRequest) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	request.RequirementID = strings.TrimSpace(request.RequirementID)
	request.TicketVersionID = strings.TrimSpace(request.TicketVersionID)
	request.WorkspacePath = filepath.Clean(strings.TrimSpace(request.WorkspacePath))
	if request.RequirementID == "" || request.TicketVersionID == "" ||
		!filepath.IsAbs(request.WorkspacePath) || len(request.ExpectedRevisions) == 0 {
		return Report{}, ErrInvalidPlan
	}
	expectedRevisions := make([]string, 0, len(request.ExpectedRevisions))
	for _, revision := range request.ExpectedRevisions {
		revision = strings.TrimSpace(revision)
		if revision == "" || slices.Contains(expectedRevisions, revision) {
			continue
		}
		expectedRevisions = append(expectedRevisions, revision)
	}
	if len(expectedRevisions) == 0 {
		return Report{}, ErrInvalidPlan
	}

	startedAt := s.now().UTC()
	records := make([]Evidence, 0, len(s.commands)+1)
	candidate, inspectErr := s.candidate.Inspect(ctx, request.WorkspacePath, expectedRevisions)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Report{}, ctxErr
	}
	records = append(records, candidateEvidence(candidate, inspectErr, expectedRevisions, startedAt, s.now().UTC()))

	// 先执行不依赖服务的 typecheck/build，再检查运行时并执行集成测试，避免 build 与 dev
	// server 同时写同一个输出目录。结果仍按配置顺序展示，执行顺序不泄漏到 UI 排序。
	commandRecords := make(map[string]Evidence, len(s.commands))
	for _, requiresRuntime := range []bool{false, true} {
		for _, command := range s.commands {
			if command.RequiresRuntime != requiresRuntime {
				continue
			}
			evidence, executeErr := s.executeCommand(ctx, request.WorkspacePath, command)
			if executeErr != nil {
				return Report{}, executeErr
			}
			commandRecords[command.ID] = evidence
		}
	}
	for _, command := range s.commands {
		records = append(records, commandRecords[command.ID])
	}

	matrix, err := BuildMatrix(records)
	if err != nil {
		return Report{}, err
	}
	status := ReportStatusPassed
	if matrix.BlocksDelivery() {
		status = ReportStatusBlocked
	}
	report := Report{
		ID:                strings.TrimSpace(s.newID()),
		RequirementID:     request.RequirementID,
		TicketVersionID:   request.TicketVersionID,
		CandidateRevision: strings.TrimSpace(candidate.Revision),
		WorkspacePath:     request.WorkspacePath,
		Status:            status,
		Matrix:            matrix,
		StartedAt:         startedAt,
		CompletedAt:       s.now().UTC(),
	}
	if report.ID == "" || report.StartedAt.IsZero() || report.CompletedAt.Before(report.StartedAt) {
		return Report{}, ErrInvalidPlan
	}
	if err := s.reports.SaveReport(ctx, report); err != nil {
		return Report{}, fmt.Errorf("保存 Verification 报告: %w", err)
	}
	return report, nil
}

func (s *Service) executeCommand(
	ctx context.Context,
	workspacePath string,
	command Command,
) (Evidence, error) {
	if err := ctx.Err(); err != nil {
		return Evidence{}, err
	}
	if command.RequiresRuntime {
		probeStartedAt := s.now().UTC()
		probeErr := s.runtime.Check(ctx)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Evidence{}, ctxErr
		}
		if probeErr != nil {
			return unavailableRuntimeEvidence(
				command,
				probeErr,
				probeStartedAt,
				s.now().UTC(),
			), nil
		}
	}
	result := s.executor.Execute(ctx, workspacePath, command)
	if result.Termination == TerminationCancelled && ctx.Err() != nil {
		return Evidence{}, ctx.Err()
	}
	return commandEvidence(command, result), nil
}

func unavailableRuntimeEvidence(
	command Command,
	err error,
	startedAt time.Time,
	completedAt time.Time,
) Evidence {
	exitCode := 1
	return Evidence{
		ID: command.ID, Label: command.Label, Status: StatusNotRun,
		Detail:   redactSecrets(err.Error()),
		Reason:   "environment: 依赖的目标运行时未就绪",
		Required: command.Required, Source: SourceCommand,
		Command:  displayCommand(command),
		ExitCode: &exitCode, StartedAt: startedAt, CompletedAt: completedAt,
	}
}

func (s *Service) LatestReport(
	ctx context.Context,
	requirementID string,
	ticketVersionID string,
) (Report, error) {
	return s.reports.LatestReport(ctx, strings.TrimSpace(requirementID), strings.TrimSpace(ticketVersionID))
}

func candidateEvidence(
	candidate Candidate,
	err error,
	expectedRevisions []string,
	startedAt time.Time,
	completedAt time.Time,
) Evidence {
	evidence := Evidence{
		ID:          "candidate-revision",
		Label:       "候选 Git 版本与工作区",
		Status:      StatusVerified,
		Detail:      "候选 revision 与已整合 Ticket 一致，工作区干净",
		Required:    true,
		Source:      SourceCandidate,
		Command:     "git rev-parse HEAD; git status --porcelain",
		StartedAt:   startedAt,
		CompletedAt: completedAt,
	}
	if err != nil {
		evidence.Status = StatusNotRun
		evidence.Detail = redactSecrets(err.Error())
		evidence.Reason = "environment: 无法读取受管候选 Worktree 的 Git 状态"
		return evidence
	}
	if candidate.Dirty {
		evidence.Status = StatusFailed
		evidence.Detail = "候选 Worktree 存在验证开始前未提交的修改"
		return evidence
	}
	if len(candidate.MissingRevisions) > 0 {
		evidence.Status = StatusFailed
		evidence.Detail = "候选 HEAD 没有包含全部已完成 Ticket 的整合 revision"
		return evidence
	}
	if !slices.Contains(expectedRevisions, strings.TrimSpace(candidate.Revision)) {
		evidence.Status = StatusFailed
		evidence.Detail = "候选 HEAD 不属于已完成 Ticket 记录的整合 revision"
	}
	return evidence
}

func commandEvidence(command Command, result CommandResult) Evidence {
	exitCode := result.ExitCode
	evidence := Evidence{
		ID:          command.ID,
		Label:       command.Label,
		Status:      StatusFailed,
		Detail:      strings.TrimSpace(result.Output),
		Required:    command.Required,
		Source:      SourceCommand,
		Command:     displayCommand(command),
		ExitCode:    &exitCode,
		StartedAt:   result.StartedAt,
		CompletedAt: result.CompletedAt,
	}
	if evidence.Detail == "" {
		evidence.Detail = fmt.Sprintf("命令退出码 %d", result.ExitCode)
	}
	switch result.Termination {
	case TerminationExit:
		if result.ExitCode == 0 {
			evidence.Status = StatusVerified
			evidence.Detail = strings.TrimSpace(result.Output)
			if evidence.Detail == "" {
				evidence.Detail = "命令执行成功"
			}
		}
	case TerminationTimeout:
		evidence.Status = StatusNotRun
		evidence.Reason = "environment: 验证命令在截止时间内未完成"
	case TerminationStartFailed:
		evidence.Status = StatusNotRun
		evidence.Reason = "environment: 验证命令无法启动"
	case TerminationCancelled:
		evidence.Status = StatusNotRun
		evidence.Reason = "environment: 验证命令被取消"
	default:
		evidence.Status = StatusNotRun
		evidence.Reason = "environment: 验证命令没有产生可识别结果"
	}
	return evidence
}

func displayCommand(command Command) string {
	parts := append([]string{command.Executable}, command.Args...)
	return redactSecrets(strings.Join(parts, " "))
}

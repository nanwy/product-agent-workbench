package app

import (
	"context"
	"fmt"
	"strings"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/scheduler"
	"product-agent-workbench/internal/verification"
)

type VerifyRequirementInput struct {
	RequirementID   string
	TicketVersionID string
}

// VerifyRequirementResult 同时返回最新 Requirement 和本次报告。
// Deliverable=false 是正常业务结果，前端应该展示矩阵，而不是弹出“服务器异常”。
type VerifyRequirementResult struct {
	Requirement domain.Requirement
	Report      verification.Report
	Deliverable bool
}

// VerifyRequirement 是“所有 Ticket 完成”到“等待产品验收”之间的门禁。
//
// 前端视角可以把它理解成一次 mutation：
//
//  1. 先用服务端事实检查按钮当前是否应该可点；
//  2. 执行验证，得到类似 CI jobs 的 Matrix；
//  3. Matrix 有阻塞项时返回 200 + deliverable=false，页面展示失败详情；
//  4. 全部必要项通过后，才用 revision 乐观锁把 Requirement 推进到 acceptance。
//
// 引导步骤：
//  1. 清理并校验两个 ID，读取 Requirement；
//  2. 要求当前阶段是 implementation；
//  3. 同时读取确认 TicketVersion 与执行历史，逐票核对最新 attempt 已 completed；
//  4. 所有完成记录必须指向同一个 Requirement 整合 Worktree；
//  5. 把每次整合 revision 交给 Verifier，Verifier 再读取真实 HEAD 防止验证错版本；
//  6. 报告 blocked 时保留 implementation，不把完成 Ticket 改成 failed；
//  7. 报告 passed 时使用 expectedRevision 保存 acceptance，防止覆盖并发修改。
func (a *Application) VerifyRequirement(
	ctx context.Context,
	input VerifyRequirementInput,
) (VerifyRequirementResult, error) {
	if err := ctx.Err(); err != nil {
		return VerifyRequirementResult{}, err
	}
	if a.tickets == nil || a.verification == nil {
		return VerifyRequirementResult{}, ErrInvalidDependencies
	}
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	input.TicketVersionID = strings.TrimSpace(input.TicketVersionID)
	if input.RequirementID == "" || input.TicketVersionID == "" {
		return VerifyRequirementResult{}, domain.ErrInvalidInput
	}

	requirement, err := a.store.GetRequirement(ctx, input.RequirementID)
	if err != nil {
		return VerifyRequirementResult{}, err
	}
	if requirement.Stage != domain.RequirementStageImplementation {
		return VerifyRequirementResult{}, domain.ErrInvalidTransition
	}
	version, err := a.tickets.ReadConfirmedTicketVersion(
		ctx,
		input.RequirementID,
		input.TicketVersionID,
	)
	if err != nil {
		return VerifyRequirementResult{}, fmt.Errorf("读取 Verification TicketVersion: %w", err)
	}
	if version.ID != input.TicketVersionID || version.RequirementID != input.RequirementID {
		return VerifyRequirementResult{}, fmt.Errorf("%w: TicketVersion 身份与请求不一致", domain.ErrInvalidInput)
	}
	history, err := a.store.ListTasksByTicketVersion(ctx, input.RequirementID, input.TicketVersionID)
	if err != nil {
		return VerifyRequirementResult{}, fmt.Errorf("读取 Verification TaskRun 历史: %w", err)
	}
	workspacePath, revisions, err := completedCandidateFacts(version, history)
	if err != nil {
		return VerifyRequirementResult{}, err
	}

	report, err := a.verification.Verify(ctx, verification.VerifyRequest{
		RequirementID:     input.RequirementID,
		TicketVersionID:   input.TicketVersionID,
		WorkspacePath:     workspacePath,
		ExpectedRevisions: revisions,
	})
	if err != nil {
		return VerifyRequirementResult{}, err
	}
	result := VerifyRequirementResult{
		Requirement: requirement,
		Report:      report,
		Deliverable: !report.Matrix.BlocksDelivery(),
	}
	if !result.Deliverable {
		return result, nil
	}

	updated := requirement
	updated.Stage = domain.RequirementStageAcceptance
	newRevision, err := a.store.SaveRequirement(ctx, updated, requirement.Revision)
	if err != nil {
		return VerifyRequirementResult{}, fmt.Errorf("推进 Requirement 到 acceptance: %w", err)
	}
	updated.Revision = newRevision
	result.Requirement = updated
	return result, nil
}

// GetLatestVerification 读取最近一次报告，供刷新后的前端恢复验证矩阵。
func (a *Application) GetLatestVerification(
	ctx context.Context,
	requirementID string,
	ticketVersionID string,
) (verification.Report, error) {
	if err := ctx.Err(); err != nil {
		return verification.Report{}, err
	}
	if a.verification == nil {
		return verification.Report{}, ErrInvalidDependencies
	}
	requirementID = strings.TrimSpace(requirementID)
	ticketVersionID = strings.TrimSpace(ticketVersionID)
	if requirementID == "" || ticketVersionID == "" {
		return verification.Report{}, domain.ErrInvalidInput
	}
	if _, err := a.store.GetRequirement(ctx, requirementID); err != nil {
		return verification.Report{}, err
	}
	return a.verification.LatestReport(ctx, requirementID, ticketVersionID)
}

func completedCandidateFacts(
	version scheduler.TicketVersion,
	history []domain.TaskRun,
) (string, []string, error) {
	latest := make(map[string]domain.TaskRun, len(version.Tickets))
	for _, run := range history {
		previous, exists := latest[run.TicketID]
		if !exists || run.Attempt > previous.Attempt {
			latest[run.TicketID] = run
		}
	}

	workspacePath := ""
	revisions := make([]string, 0, len(version.Tickets))
	for _, ticket := range version.Tickets {
		run, exists := latest[ticket.ID]
		validResult := exists && run.Status == domain.TaskStatusCompleted && run.Result != nil
		if !validResult || strings.TrimSpace(run.Result.WorktreePath) == "" ||
			strings.TrimSpace(run.Result.Revision) == "" {
			return "", nil, fmt.Errorf(
				"%w: Ticket %q 尚未形成可验证的 completed 事实",
				domain.ErrInvalidTransition,
				ticket.ID,
			)
		}
		if workspacePath == "" {
			workspacePath = run.Result.WorktreePath
		}
		if workspacePath != run.Result.WorktreePath {
			return "", nil, fmt.Errorf("%w: Ticket 指向不同的整合 Worktree", domain.ErrInvalidInput)
		}
		revisions = append(revisions, run.Result.Revision)
	}
	return workspacePath, revisions, nil
}

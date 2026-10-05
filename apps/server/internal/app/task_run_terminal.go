package app

import (
	"context"
	"strings"

	"product-agent-workbench/internal/domain"
)

// FailTaskRunInput 描述一次失败执行需要保存的事实。
type FailTaskRunInput struct {
	ID     string
	Reason string
}

// FailTaskRun 把 claimed 或 running TaskRun 迁移为 failed。
//
// 对应原 TypeScript `workbench-execution.ts/recordExecutionFailure` 中记录当前执行失败的
// 部分。更新 Ticket、对话消息、事件流等投影不属于这个用例，后续由 Runner 与事件模块处理。
//
// 实现顺序：
//  1. 检查 ctx.Err()。
//  2. TrimSpace 清理 ID 和 Reason；任一空白返回 domain.ErrInvalidInput。
//  3. Store.GetTask；不存在直接返回，不能读取时钟。
//  4. app.now() → run.Fail(reason, now)。
//  5. Store.SaveTask；成功后返回 failed TaskRun。
func (app *Application) FailTaskRun(
	ctx context.Context,
	input FailTaskRunInput,
) (domain.TaskRun, error) {
	if ctx.Err() != nil {
		return domain.TaskRun{}, ctx.Err()
	}

	input.ID = strings.TrimSpace(input.ID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ID == "" || input.Reason == "" {
		return domain.TaskRun{}, domain.ErrInvalidInput
	}
	taskRun, err := app.store.GetTask(ctx, input.ID)
	if err != nil {
		return domain.TaskRun{}, err
	}
	now := app.now()
	originalStatus := taskRun.Status
	updated, err := taskRun.Fail(input.Reason, now)
	if err != nil {
		return domain.TaskRun{}, err
	}

	if err := app.store.SaveTask(ctx, updated, originalStatus); err != nil {
		return domain.TaskRun{}, err
	}

	return updated, nil
}

// CancelTaskRun 把 queued、claimed 或 running TaskRun 迁移为 cancelled。
//
// 对应原 TypeScript `cancelRequirementExecution` 中持久化 cancelled 的部分。向 Codex
// 发送 interrupt/abort 属于 Runner；这个用例只保存控制面的取消事实。
//
// 实现顺序：
//  1. 检查 ctx.Err()。
//  2. TrimSpace 清理 ID；空白返回 domain.ErrInvalidInput。
//  3. Store.GetTask；不存在直接返回，不能读取时钟。
//  4. app.now() → run.Cancel(now)。
//  5. Store.SaveTask；成功后返回 cancelled TaskRun。
func (app *Application) CancelTaskRun(ctx context.Context, id string) (domain.TaskRun, error) {
	if ctx.Err() != nil {
		return domain.TaskRun{}, ctx.Err()
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return domain.TaskRun{}, domain.ErrInvalidInput
	}
	taskRun, err := app.store.GetTask(ctx, id)
	if err != nil {
		return domain.TaskRun{}, err
	}
	now := app.now()
	originalStatus := taskRun.Status
	updated, err := taskRun.Cancel(now)
	if err != nil {
		return domain.TaskRun{}, err
	}

	if err := app.store.SaveTask(ctx, updated, originalStatus); err != nil {
		return domain.TaskRun{}, err
	}

	return updated, nil
}

package app

import (
	"context"
	"strings"

	"product-agent-workbench/internal/domain"
)

// ClaimNextTaskRun 原子领取队列中最早的 queued TaskRun。
//
// 它对应原 TypeScript `workbench-execution.ts/prepareExecution` 中查找 nextIndex、把
// queued Ticket 设为 active 的部分。Go 不再直接改 Ticket，而是把一次执行尝试从
// queued 迁移为 claimed。
//
// 建议按以下顺序实现：
//  1. 先检查 ctx.Err()；取消后不能读取时钟或领取任务。
//  2. 调用 app.now() 得到领取时间。
//  3. 把同一个 ctx 和时间交给 Store.ClaimNextTask，并原样返回结果与错误。
//
// 原子性已经封装在 Store 中。不要在 Application 里先 GetTask 再 SaveTask，否则两个
// Runner 可能同时领取同一任务。
func (app *Application) ClaimNextTaskRun(ctx context.Context) (domain.TaskRun, error) {
	if ctx.Err() != nil {
		return domain.TaskRun{}, ctx.Err()
	}

	now := app.now()
	taskRun, err := app.store.ClaimNextTask(ctx, now)
	if err != nil {
		return domain.TaskRun{}, err
	}

	return taskRun, nil
}

// StartTaskRun 记录 Runner 已经真正开始执行一个 claimed TaskRun。
//
// 它对应原 TypeScript `executeQueue` 即将调用 `runCodexTurn` 的位置。claimed 只表示
// 调度器保留了任务；running 才表示 Runner 已完成准备、准备启动 Codex turn。拆成两个
// 状态后，重启恢复时可以判断任务卡在调度阶段还是执行阶段。
//
// 建议按以下顺序实现：
//  1. 先检查 ctx.Err()。
//  2. 使用 strings.TrimSpace 清理 id；空白返回 domain.ErrInvalidInput。
//  3. 使用同一个 ctx 调用 Store.GetTask；不存在时直接返回 Store 错误，不读取时钟。
//  4. 读取 app.now()，调用 run.Start(now)，由领域层检查前态和时间顺序。
//  5. 领域迁移成功后调用 Store.SaveTask；保存成功才返回 running TaskRun。
//
// 不要直接修改 Status、StartedAt，也不要在这里调用 Codex。Application 只编排状态事实；
// 真正启动子进程属于后续 Runner 模块。
func (app *Application) StartTaskRun(ctx context.Context, id string) (domain.TaskRun, error) {
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
	taskRun, err = taskRun.Start(now)
	if err != nil {
		return domain.TaskRun{}, err
	}

	if err := app.store.SaveTask(ctx, taskRun, originalStatus); err != nil {
		return domain.TaskRun{}, err
	}
	return taskRun, nil
}

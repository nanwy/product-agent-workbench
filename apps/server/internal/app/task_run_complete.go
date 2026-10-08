package app

import (
	"context"
	"strings"

	"product-agent-workbench/internal/domain"
)

// CompleteTaskRunInput 描述一次成功执行需要落盘的结果。
//
// ID 用来定位 TaskRun；Result 是 Runner 在 Codex turn 成功退出并完成宿主检查后收集的
// 可审计事实。FinishedAt 不由调用方提供，Application 使用注入的 Clock 生成。
type CompleteTaskRunInput struct {
	ID     string
	Result domain.TaskResult
}

// CompleteTaskRun 把 running TaskRun 迁移为 completed 并持久化结果。
//
// 它对应原 TypeScript `workbench-execution.ts/executeQueue` 中 `runCodexTurn` 成功、Git
// 范围检查通过后，把当前 Ticket 标记 completed 的那一小段。以下职责仍不在本用例中：
//
//   - 选择并启动下一张 Ticket：以后由 Runner 循环负责；
//   - 执行 typecheck/test/build：由 18B verification 用例在全部 Ticket 完成后负责；
//   - 发布 SSE：以后由事件写入和 HTTP 层负责。
//
// 建议按以下顺序实现：
//  1. 先检查 ctx.Err()。
//  2. 使用 strings.TrimSpace 清理 input.ID；空白返回 domain.ErrInvalidInput。
//  3. 使用同一个 ctx 调用 Store.GetTask；不存在时直接返回 Store 错误，不读取时钟。
//  4. 读取 app.now()，调用 run.Complete(input.Result, now)。结果是否合法、当前是否
//     running、时间是否倒流，都交给领域方法判断。
//  5. 领域迁移成功后调用 Store.SaveTask；保存失败原样返回错误。
//  6. 保存成功才返回 completed TaskRun。
//
// 不要直接给 Status、FinishedAt 或 Result 赋值，也不要在这里运行验证命令。
func (app *Application) CompleteTaskRun(
	ctx context.Context,
	input CompleteTaskRunInput,
) (domain.TaskRun, error) {
	if ctx.Err() != nil {
		return domain.TaskRun{}, ctx.Err()
	}

	input.ID = strings.TrimSpace(input.ID)
	if input.ID == "" {
		return domain.TaskRun{}, domain.ErrInvalidInput
	}
	taskRun, err := app.store.GetTask(ctx, input.ID)
	if err != nil {
		return domain.TaskRun{}, err
	}
	now := app.now()
	originalStatus := taskRun.Status
	updated, err := taskRun.Complete(input.Result, now)
	if err != nil {
		return domain.TaskRun{}, err
	}
	if err := app.store.SaveTask(ctx, updated, originalStatus); err != nil {
		return domain.TaskRun{}, err
	}
	return updated, nil
}

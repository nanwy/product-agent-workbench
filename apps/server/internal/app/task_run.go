package app

import (
	"context"
	"strings"

	"product-agent-workbench/internal/domain"
)

// EnqueueTaskRunInput 是创建一次执行尝试时由调用方提供的数据。
//
// ID 和 CreatedAt 不属于调用方输入：Application 使用注入的 NewID 和 Now 生成它们。
// 这样 HTTP handler 不需要知道领域对象如何初始化。
type EnqueueTaskRunInput struct {
	RequirementID string
	TicketID      string
	Attempt       int
}

// EnqueueTaskRun 创建并持久化 queued TaskRun。
//
// 建议按以下顺序实现：
//  1. 先检查 ctx.Err()；已经取消的请求不能继续读写 Store，也不能消耗 ID 或时间。
//  2. 使用 strings.TrimSpace 清理 RequirementID、TicketID，并校验 Attempt >= 1。
//  3. 调用 Store.GetRequirement 确认父 Requirement 存在；不要允许孤儿 TaskRun。
//  4. 校验通过后再调用 NewID 和 Now，并把结果交给 domain.NewTaskRun。
//  5. 调用 Store.EnqueueTask；成功后返回领域对象，Store 错误原样向上传播。
//
// 注意：不要在这里手写 queued、CreatedAt 等领域初始化规则，也不要直接调用 time.Now()。
// 这些分别属于 domain.NewTaskRun 和注入的 Clock。
func (app *Application) EnqueueTaskRun(
	ctx context.Context,
	input EnqueueTaskRunInput,
) (domain.TaskRun, error) {
	if ctx.Err() != nil {
		return domain.TaskRun{}, ctx.Err()
	}
	requirementID := strings.TrimSpace(input.RequirementID)
	ticketID := strings.TrimSpace(input.TicketID)
	attempt := input.Attempt

	if requirementID == "" || ticketID == "" || attempt < 1 {
		return domain.TaskRun{}, domain.ErrInvalidInput
	}

	_, err := app.store.GetRequirement(ctx, requirementID)
	if err != nil {
		return domain.TaskRun{}, err
	}

	newID := strings.TrimSpace(app.newID())
	createdAt := app.now()

	taskRunInput := domain.NewTaskRunInput{
		ID:            newID,
		RequirementID: requirementID,
		TicketID:      ticketID,
		Attempt:       attempt,
	}

	taskRun, err := domain.NewTaskRun(taskRunInput, createdAt)
	if err != nil {
		return domain.TaskRun{}, err
	}

	err = app.store.EnqueueTask(ctx, taskRun)
	if err != nil {
		return domain.TaskRun{}, err
	}

	return taskRun, nil
}

// GetTaskRun 返回指定 TaskRun。
//
// 建议按以下顺序实现：
//  1. 先检查 ctx.Err()。
//  2. 清理 ID；空白 ID 返回 domain.ErrInvalidInput。
//  3. 把同一个 ctx 和清理后的 ID 传给 Store.GetTask，并原样返回结果与错误。
func (app *Application) GetTaskRun(ctx context.Context, id string) (domain.TaskRun, error) {
	if ctx.Err() != nil {
		return domain.TaskRun{}, ctx.Err()
	}
	cleanID := strings.TrimSpace(id)
	if cleanID == "" {
		return domain.TaskRun{}, domain.ErrInvalidInput
	}

	taskRun, err := app.store.GetTask(ctx, cleanID)
	if err != nil {
		return domain.TaskRun{}, err
	}

	return taskRun, nil
}

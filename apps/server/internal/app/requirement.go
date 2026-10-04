package app

import (
	"context"
	"strings"

	"product-agent-workbench/internal/domain"
)

// CreateRequirementInput 是创建 Requirement 用例接受的输入。
//
// ID、Stage 和 Revision 不由 HTTP 调用方提供：ID 由 Application 生成，初始阶段和版本
// 是应用规则。这样调用方无法伪造一个已经 delivered 或 revision 很大的新 Requirement。
type CreateRequirementInput struct {
	ProjectID string
	Title     string
}

// CreateRequirement 校验输入、建立初始领域事实并持久化。
//
// 实现要点：
//  1. 先检查 ctx.Err()，取消的请求不能消耗 ID 或写入 Store。
//  2. 使用 strings.TrimSpace 处理 ProjectID 和 Title；任一为空时返回 domain.ErrInvalidInput。
//  3. 输入合法后再调用 app.newID；生成的 ID 为空白也返回 domain.ErrInvalidInput。
//  4. 创建 Requirement：Stage 固定为 grilling，Revision 固定为 1。
//  5. 把同一个 ctx 传给 Store.CreateRequirement，错误原样返回，不在这里改写错误语义。
//  6. Store 成功后返回实际保存的 Requirement 值。
func (app *Application) CreateRequirement(
	ctx context.Context,
	input CreateRequirementInput,
) (domain.Requirement, error) {
	if ctx.Err() != nil {
		return domain.Requirement{}, ctx.Err()
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.Title = strings.TrimSpace(input.Title)

	if input.ProjectID == "" || input.Title == "" {
		return domain.Requirement{}, domain.ErrInvalidInput
	}

	id := strings.TrimSpace(app.newID())

	if id == "" {
		return domain.Requirement{}, domain.ErrInvalidInput
	}

	requirement := domain.Requirement{
		ID:        id,
		ProjectID: input.ProjectID,
		Title:     input.Title,
		Stage:     domain.RequirementStageGrilling,
		Revision:  1,
	}

	if err := app.store.CreateRequirement(ctx, requirement); err != nil {
		return domain.Requirement{}, err
	}

	return requirement, nil
}

// GetRequirement 返回指定 Requirement。
//
// 实现要点：
//  1. 检查 ctx.Err()。
//  2. trim ID；空白 ID 返回 domain.ErrInvalidInput，而不是 Store 的 ErrNotFound。
//  3. 把同一个 ctx 和清理后的 ID 传给 Store.GetRequirement，并原样返回结果与错误。
func (app *Application) GetRequirement(
	ctx context.Context,
	id string,
) (domain.Requirement, error) {
	if ctx.Err() != nil {
		return domain.Requirement{}, ctx.Err()
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return domain.Requirement{}, domain.ErrInvalidInput
	}

	return app.store.GetRequirement(ctx, id)
}

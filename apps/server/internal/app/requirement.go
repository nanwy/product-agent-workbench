package app

import (
	"context"
	"strings"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
)

// CreateRequirementInput 是创建 Requirement 用例接受的输入。
//
// ID、Stage 和 Revision 不由 HTTP 调用方提供：ID 由 Application 生成，初始阶段和版本
// 是应用规则。这样调用方无法伪造一个已经 delivered 或 revision 很大的新 Requirement。
type CreateRequirementInput struct {
	ProjectID string
	Title     string
}

// CreateRequirement 校验输入，并原子建立 Requirement 及其逻辑对话。
//
// 前端视角：这不是依次发送三个 mutation，而是先在内存中组装完整的 normalized entities，
// 最后只调用一次 Store mutation。这样页面不会读到“Requirement 已出现，但聊天区域还没有
// Conversation”的半完成状态。
func (app *Application) CreateRequirement(
	ctx context.Context,
	input CreateRequirementInput,
) (domain.Requirement, error) {
	// 第 1 步：先响应请求取消。
	// 和前端在 mutation 开始前检查 AbortSignal 类似；已经取消的请求不应消耗 ID 或访问 Store。
	if err := ctx.Err(); err != nil {
		return domain.Requirement{}, err
	}

	// 第 2 步：在应用边界清理浏览器输入。
	// TrimSpace 只做格式规范化；“新需求必须有项目和标题”才是这里真正执行的业务规则。
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.Title = strings.TrimSpace(input.Title)
	if input.ProjectID == "" || input.Title == "" {
		return domain.Requirement{}, domain.ErrInvalidInput
	}

	// 第 3 步：为本次原子创建中的四个对象分别生成身份。
	// 前缀相当于 normalized cache 的 entity type，可以从日志中的 ID 看出对象种类。
	requirementID, err := app.newEntityID("")
	if err != nil {
		return domain.Requirement{}, err
	}
	conversationID, err := app.newEntityID("conversation-")
	if err != nil {
		return domain.Requirement{}, err
	}
	messageID, err := app.newEntityID("system-message-")
	if err != nil {
		return domain.Requirement{}, err
	}
	segmentID, err := app.newEntityID("gate-segment-")
	if err != nil {
		return domain.Requirement{}, err
	}

	// 第 4 步：所有无时间依赖的校验通过后只读一次时钟。
	// 同一个 now 会写入 Conversation 和初始消息，表示它们属于同一次业务提交。
	now := app.now()
	if now.IsZero() {
		return domain.Requirement{}, domain.ErrInvalidTime
	}
	now = now.UTC()

	// 第 5 步：建立最小 Requirement 聚合头。
	// Stage 和 Revision 是服务端初始规则，浏览器无权把新需求伪造成已交付或高版本数据。
	requirement := domain.Requirement{
		ID:        requirementID,
		ProjectID: input.ProjectID,
		Title:     input.Title,
		Stage:     domain.RequirementStageGrilling,
		Revision:  1,
	}

	// 第 6 步：通过领域构造函数创建 Conversation，而不是在应用层手写初始状态。
	// 这对应 React 中复用 reducer 的 initialState 工厂，避免不同入口拼出不同默认值。
	conversation, err := domain.NewConversation(domain.NewConversationInput{
		ID:            conversationID,
		RequirementID: requirement.ID,
	}, now)
	if err != nil {
		return domain.Requirement{}, err
	}

	// 第 7 步：创建页面时间线中的第一条 system 门禁消息。
	// 它不属于某个 Turn，所以 TurnID 留空；sequence=1 为后续 user/assistant 消息建立起点。
	initialMessage := domain.ConversationMessage{
		ID:             messageID,
		ConversationID: conversation.ID,
		Sequence:       1,
		Role:           domain.ConversationRoleSystem,
		Phase:          domain.ConversationPhaseGrilling,
		Status:         domain.ConversationMessageCompleted,
		Segments: []domain.ConversationSegment{{
			ID:     segmentID,
			Type:   domain.ConversationSegmentGate,
			Label:  "进入 Grilling",
			Detail: "只读规划 · 等待 Agent 建立设计树",
		}},
		RecordedAt: now,
	}

	// 第 8 步：把三个实体作为一个事务输入交给 Store。
	// 任何唯一键、context 或数据库错误都会让整组写入回滚；应用层不做补偿式 delete。
	err = app.store.CreateRequirementConversation(ctx, store.CreateRequirementConversationInput{
		Requirement:    requirement,
		Conversation:   conversation,
		InitialMessage: initialMessage,
	})
	if err != nil {
		return domain.Requirement{}, err
	}

	// 第 9 步：只在事务成功后返回 Requirement。
	// HTTP 契约暂时仍返回旧 DTO；Conversation 会由后续聚合读取接口提供。
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

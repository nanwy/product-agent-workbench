package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
)

const maxConversationUserContentLength = 4000

const maxConversationEventPayloadBytes = 64 << 10
const maxConversationTextBytes = 32 << 10

const grillingReadyMarker = "[WORKBENCH:READY_FOR_CONFIRMATION]"

const (
	// ConversationEventTextDelta 是 Codex assistant 文本增量。
	ConversationEventTextDelta = "codex.text.delta"
	// ConversationEventItemStarted 表示工具、命令或文件活动开始。
	ConversationEventItemStarted = "codex.item.started"
	// ConversationEventItemCompleted 表示工具、命令或文件活动结束。
	ConversationEventItemCompleted = "codex.item.completed"
)

// BeginConversationTurnInput 是浏览器请求开始一轮 Agent 对话时允许提交的字段。
//
// RequirementID 用于找到逻辑对话；ExpectedRevision 来自浏览器上次读取的服务端快照；
// UserContent 是这一轮交给 Agent 的用户文本。Turn ID、消息 ID、sequence 和时间都由服务端生成，
// 防止浏览器伪造运行状态或覆盖另一页面刚开始的 Turn。
type BeginConversationTurnInput struct {
	RequirementID    string
	ExpectedRevision int64
	UserContent      string
}

// ConversationTurnStart 是一次成功开始 Turn 后的完整稳定快照。
//
// 前端视角：它类似 mutation 成功后用于更新缓存的 payload。页面拿到它时，Conversation、Turn、
// 用户消息和 assistant 占位消息已经一起持久化，不需要在本地乐观拼出第二份业务真值。
type ConversationTurnStart struct {
	Conversation     domain.Conversation
	Turn             domain.ConversationTurn
	UserMessage      domain.ConversationMessage
	AssistantMessage domain.ConversationMessage
}

// ApplyConversationEventInput 标识一个已经由 Codex Adapter 解析和脱敏的稳定事件。
// Payload 沿用 16D 的 JSON 格式；Application 不接收原始 JSON-RPC notification。
type ApplyConversationEventInput struct {
	RequirementID    string
	TurnID           string
	ExpectedRevision int64
	EventType        string
	Payload          []byte
}

// FinishConversationTurnInput 是成功结束 Turn 所需的用户可确认信息。
type FinishConversationTurnInput struct {
	RequirementID        string
	TurnID               string
	ExpectedRevision     int64
	ReadyForConfirmation bool
}

// FailConversationTurnInput 保存一次 Codex 或事件处理失败的稳定原因。
type FailConversationTurnInput struct {
	RequirementID    string
	TurnID           string
	ExpectedRevision int64
	Reason           string
}

// EndConversationTurnResult 是成功、失败和取消共用的终态返回值。
type EndConversationTurnResult struct {
	Conversation     domain.Conversation
	Turn             domain.ConversationTurn
	AssistantMessage domain.ConversationMessage
}

// RunConversationTurnInput 描述已经由 23C 建立、现在需要交给 Codex 执行的本地 Turn。
type RunConversationTurnInput struct {
	RequirementID    string
	TurnID           string
	ExpectedRevision int64
	Prompt           string
}

// ReplyConversationInput 是用户在 Grilling 对话中继续回答问题的意图。
type ReplyConversationInput struct {
	RequirementID    string
	ExpectedRevision int64
	Content          string
}

// RetryConversationInput 是失败或取消后继续同一规划 Thread 的意图。
// 重试不带 Content，因为上一条用户消息已经是持久化历史，不能重复追加。
type RetryConversationInput struct {
	RequirementID    string
	ExpectedRevision int64
}

// CancelActiveConversationInput 标识浏览器想中止的当前运行 Turn。
type CancelActiveConversationInput struct {
	RequirementID    string
	TurnID           string
	ExpectedRevision int64
}

// activeConversationTurn 是运行期资源句柄，不是领域模型。
// cancel 对应前端 AbortController.abort；done 让取消请求等待数据库终态已经收口。
type activeConversationTurn struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// BeginConversationTurn 原子开始一次“用户输入 → Agent 响应”的对话 Turn。
//
// 这个用例只建立可靠的运行事实，不调用 Codex。下一步运行时即使启动失败，也能依据这里保存的
// running Turn 和 streaming assistant 消息执行失败收口或重启恢复。
func (app *Application) BeginConversationTurn(
	ctx context.Context,
	input BeginConversationTurnInput,
) (ConversationTurnStart, error) {
	// 第 1 步：在做任何读取或生成 ID 之前处理请求取消。
	// 前端类比：AbortSignal 已触发时，mutation 应直接退出，不能偷偷产生服务端副作用。
	if err := ctx.Err(); err != nil {
		return ConversationTurnStart{}, err
	}

	// 第 2 步：清理并验证浏览器输入。
	// revision 小于 1 说明客户端没有基于真实快照提交；空文本和过长文本都不进入持久化层。
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	input.UserContent = strings.TrimSpace(input.UserContent)
	invalidInput := input.RequirementID == "" || input.ExpectedRevision < 1 ||
		input.UserContent == "" || utf8.RuneCountInString(input.UserContent) > maxConversationUserContentLength
	if invalidInput {
		return ConversationTurnStart{}, domain.ErrInvalidInput
	}

	// 第 3 步：读取当前 Conversation，而不是信任浏览器提交完整状态。
	// 浏览器只携带 identity + intent；phase、status、Thread ID 等可信事实始终来自 Store。
	current, err := app.store.GetConversationByRequirement(ctx, input.RequirementID)
	if err != nil {
		return ConversationTurnStart{}, err
	}

	// 第 4 步：先做一次便宜的 revision 检查，尽早向旧页面返回冲突。
	// Store 事务中还会再次 CAS；这里的检查不能替代事务，只是避免无意义地生成 ID 和时间。
	if current.Revision != input.ExpectedRevision {
		return ConversationTurnStart{}, store.ErrConversationConflict
	}

	// 第 5 步：读取已保存的 Turn 和消息历史，计算下一组连续 sequence。
	// 类似前端 append 列表时使用服务端最后一项的 cursor，而不是让调用方随意传数组下标。
	turns, err := app.store.ListConversationTurns(ctx, current.ID)
	if err != nil {
		return ConversationTurnStart{}, err
	}
	messages, err := app.store.ListConversationMessages(ctx, current.ID, 0)
	if err != nil {
		return ConversationTurnStart{}, err
	}
	nextTurnSequence := int64(len(turns) + 1)
	nextMessageSequence := int64(len(messages) + 1)

	// 第 6 步：为这次提交里的 Turn、两条消息和用户文本片段分别生成 ID。
	// 每个对象独立生成，后续事件更新 assistant 消息时才能按 ID 定位，而不是按数组位置猜。
	turnID, err := app.newEntityID("turn-")
	if err != nil {
		return ConversationTurnStart{}, err
	}
	userMessageID, err := app.newEntityID("user-message-")
	if err != nil {
		return ConversationTurnStart{}, err
	}
	userSegmentID, err := app.newEntityID("text-segment-")
	if err != nil {
		return ConversationTurnStart{}, err
	}
	assistantMessageID, err := app.newEntityID("assistant-message-")
	if err != nil {
		return ConversationTurnStart{}, err
	}

	// 第 7 步：所有静态校验通过后只读取一次时钟。
	// Conversation、Turn 和两条消息共用 now，表示它们是同一个“开始 Turn”业务时刻。
	now := app.now()
	if now.IsZero() {
		return ConversationTurnStart{}, domain.ErrInvalidTime
	}
	now = now.UTC()

	// 第 8 步：让 23A 的领域状态机决定 Conversation 和 Turn 的新状态。
	// Application 只提供 ID、sequence、now，不手写 running/revision 等状态迁移规则。
	conversation, turn, err := current.BeginTurn(domain.BeginConversationTurnInput{
		ID:       turnID,
		Sequence: nextTurnSequence,
	}, now)
	if err != nil {
		return ConversationTurnStart{}, err
	}

	// 第 9 步：建立已完成的 user 消息。
	// 用户输入成为持久化事实；以后重启进程时可以用消息历史重新构造 Agent 上下文。
	userMessage := domain.ConversationMessage{
		ID:             userMessageID,
		ConversationID: conversation.ID,
		TurnID:         turn.ID,
		Sequence:       nextMessageSequence,
		Role:           domain.ConversationRoleUser,
		Phase:          turn.Phase,
		Status:         domain.ConversationMessageCompleted,
		Segments: []domain.ConversationSegment{{
			ID:      userSegmentID,
			Type:    domain.ConversationSegmentText,
			Content: input.UserContent,
		}},
		RecordedAt: now,
	}

	// 第 10 步：预建空的 streaming assistant 消息。
	// 前端类比：先在消息列表插入一个稳定占位项，后续 SSE 只更新这一个 entity 的 segments。
	assistantMessage := domain.ConversationMessage{
		ID:             assistantMessageID,
		ConversationID: conversation.ID,
		TurnID:         turn.ID,
		Sequence:       nextMessageSequence + 1,
		Role:           domain.ConversationRoleAssistant,
		Phase:          turn.Phase,
		Status:         domain.ConversationMessageStreaming,
		Segments:       []domain.ConversationSegment{},
		RecordedAt:     now,
	}

	// 第 11 步：把四份事实交给 23B 的原子 Store 操作。
	// Store 会重新检查 revision、sequence，并重跑 BeginTurn；并发请求只有一个能够成功提交。
	persistInput := store.BeginConversationTurnInput{
		Conversation:     conversation,
		ExpectedRevision: input.ExpectedRevision,
		Turn:             turn,
		UserMessage:      userMessage,
		AssistantMessage: assistantMessage,
	}
	if err := app.store.BeginConversationTurn(ctx, persistInput); err != nil {
		return ConversationTurnStart{}, err
	}

	// 第 12 步：事务成功后返回刚刚持久化的同一组值。
	// 这里不再次查询数据库；Store 契约保证成功提交的值就是 persistInput 中的值。
	return ConversationTurnStart{
		Conversation:     conversation,
		Turn:             turn,
		UserMessage:      userMessage,
		AssistantMessage: assistantMessage,
	}, nil
}

// ReplyConversation 保存用户回复、创建 assistant 占位消息，并立即运行同一条规划 Thread。
//
// 23F 只接回原 MVP 的 Grilling 回复入口。Spec/Tickets 的产物生成仍走各自已经存在的用例，
// 不能因为共用 Conversation 表就把一段聊天文本误当成正式版本。
func (app *Application) ReplyConversation(
	ctx context.Context,
	input ReplyConversationInput,
) (EndConversationTurnResult, error) {
	// 第 1 步：读取服务端快照并检查这是一个真正等待用户回答的 Grilling 对话。
	// 前端按钮是否 disabled 只改善体验；服务端仍必须重做这个门禁。
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	input.Content = strings.TrimSpace(input.Content)
	if input.RequirementID == "" || input.ExpectedRevision < 1 || input.Content == "" {
		return EndConversationTurnResult{}, domain.ErrInvalidInput
	}
	current, err := app.store.GetConversationByRequirement(ctx, input.RequirementID)
	if err != nil {
		return EndConversationTurnResult{}, err
	}
	if current.Revision != input.ExpectedRevision {
		return EndConversationTurnResult{}, store.ErrConversationConflict
	}
	if current.Phase != domain.ConversationPhaseGrilling ||
		current.Status != domain.ConversationStatusAwaitingUser {
		return EndConversationTurnResult{}, domain.ErrInvalidTransition
	}

	// 第 2 步：复用 23C 的原子开始用例。成功后 user + streaming assistant 已经一起落库。
	started, err := app.BeginConversationTurn(ctx, BeginConversationTurnInput{
		RequirementID: input.RequirementID, ExpectedRevision: input.ExpectedRevision,
		UserContent: input.Content,
	})
	if err != nil {
		return EndConversationTurnResult{}, err
	}

	// 第 3 步：Thread 已存在时，Prompt 只表达“本轮新增意图”；Codex 自己拥有此前上下文。
	// 若首次启动尚未来得及保存 Thread，则补入持久化 transcript，保证重试不会丢上下文。
	prompt, err := app.buildGrillingPrompt(ctx, current, input.Content, false)
	if err != nil {
		return app.failStartedConversationTurn(ctx, started, err)
	}
	return app.RunConversationTurn(ctx, RunConversationTurnInput{
		RequirementID: input.RequirementID, TurnID: started.Turn.ID,
		ExpectedRevision: started.Conversation.Revision, Prompt: prompt,
	})
}

// RetryConversation 创建新 Turn 后续跑失败/取消的 Grilling，不伪造第二条用户消息。
func (app *Application) RetryConversation(
	ctx context.Context,
	input RetryConversationInput,
) (EndConversationTurnResult, error) {
	// 第 1 步：只允许 failed/cancelled 重试。completed 和 awaiting-user 应走 Reply。
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	if input.RequirementID == "" || input.ExpectedRevision < 1 {
		return EndConversationTurnResult{}, domain.ErrInvalidInput
	}
	current, err := app.store.GetConversationByRequirement(ctx, input.RequirementID)
	if err != nil {
		return EndConversationTurnResult{}, err
	}
	if current.Revision != input.ExpectedRevision {
		return EndConversationTurnResult{}, store.ErrConversationConflict
	}
	if current.Phase != domain.ConversationPhaseGrilling ||
		(current.Status != domain.ConversationStatusFailed &&
			current.Status != domain.ConversationStatusCancelled) {
		return EndConversationTurnResult{}, domain.ErrInvalidTransition
	}

	// 第 2 步：新建 Turn 与 assistant 占位消息。这里没有 UserMessage，这是重试与回复的核心区别。
	started, err := app.beginConversationRetry(ctx, current)
	if err != nil {
		return EndConversationTurnResult{}, err
	}

	// 第 3 步：有 Thread 时让 Codex 从旧上下文继续；没有 Thread 时用数据库历史恢复语境。
	prompt, err := app.buildGrillingPrompt(ctx, current, "", true)
	if err != nil {
		return app.failStartedConversationTurn(ctx, started, err)
	}
	return app.RunConversationTurn(ctx, RunConversationTurnInput{
		RequirementID: input.RequirementID, TurnID: started.Turn.ID,
		ExpectedRevision: started.Conversation.Revision, Prompt: prompt,
	})
}

// CancelActiveConversation 中止本进程拥有的 Codex Turn，并等待 RunConversationTurn 完成落库。
func (app *Application) CancelActiveConversation(
	ctx context.Context,
	input CancelActiveConversationInput,
) (EndConversationTurnResult, error) {
	// 第 1 步：先验证浏览器携带的 identity/revision，避免取消另一条对话。
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	input.TurnID = strings.TrimSpace(input.TurnID)
	if err := validateConversationEndInput(
		ctx, input.RequirementID, input.TurnID, input.ExpectedRevision,
	); err != nil {
		return EndConversationTurnResult{}, err
	}
	conversation, turn, _, err := app.loadRunningConversationTurn(
		ctx, input.RequirementID, input.TurnID, input.ExpectedRevision,
	)
	if err != nil {
		return EndConversationTurnResult{}, err
	}

	// 第 2 步：从运行期 registry 取得 cancel。锁只保护 map，不能在持锁时等待 done。
	app.activeConversationMu.Lock()
	active := app.activeConversationTurns[input.TurnID]
	app.activeConversationMu.Unlock()
	if active == nil {
		// 进程刚重启或 Runtime 尚未登记时没有可 interrupt 的句柄；仍把数据库运行态可靠收口。
		return app.CancelConversationTurn(
			ctx, input.RequirementID, input.TurnID, conversation.Revision,
		)
	}
	active.cancel()

	// 第 3 步：RunConversationTurn 收到 context 后会调用 Runtime interrupt，再写 cancelled 终态。
	// 等待 done，保证 HTTP 成功响应时页面重新读取到的已经不是 running。
	select {
	case <-ctx.Done():
		return EndConversationTurnResult{}, ctx.Err()
	case <-active.done:
	}
	return app.loadEndedConversationTurn(ctx, conversation.RequirementID, turn.ID)
}

// beginConversationRetry 是 Reply 以外的 assistant-only mutation。
func (app *Application) beginConversationRetry(
	ctx context.Context,
	current domain.Conversation,
) (ConversationTurnStart, error) {
	// 第 1 步：历史长度决定新 sequence，调用方不能从浏览器传数组位置。
	turns, err := app.store.ListConversationTurns(ctx, current.ID)
	if err != nil {
		return ConversationTurnStart{}, err
	}
	messages, err := app.store.ListConversationMessages(ctx, current.ID, 0)
	if err != nil {
		return ConversationTurnStart{}, err
	}

	// 第 2 步：服务端生成新 Turn 和 assistant 消息身份，并为同一次 mutation 读取统一时间。
	turnID, err := app.newEntityID("turn-")
	if err != nil {
		return ConversationTurnStart{}, err
	}
	assistantID, err := app.newEntityID("assistant-message-")
	if err != nil {
		return ConversationTurnStart{}, err
	}
	now, err := app.conversationNow()
	if err != nil {
		return ConversationTurnStart{}, err
	}

	// 第 3 步：状态机清除上次 error/cancelled，并产生 revision+1 的 running 快照。
	conversation, turn, err := current.BeginTurn(domain.BeginConversationTurnInput{
		ID: turnID, Sequence: int64(len(turns) + 1),
	}, now)
	if err != nil {
		return ConversationTurnStart{}, err
	}
	assistant := domain.ConversationMessage{
		ID: assistantID, ConversationID: current.ID, TurnID: turn.ID,
		Sequence: int64(len(messages) + 1), Role: domain.ConversationRoleAssistant,
		Phase: turn.Phase, Status: domain.ConversationMessageStreaming,
		Segments: []domain.ConversationSegment{}, RecordedAt: now,
	}

	// 第 4 步：Store 事务同时写三份事实；重试历史保留，旧 Turn 不被覆盖。
	if err := app.store.RetryConversationTurn(ctx, store.RetryConversationTurnInput{
		Conversation: conversation, ExpectedRevision: current.Revision,
		Turn: turn, AssistantMessage: assistant,
	}); err != nil {
		return ConversationTurnStart{}, err
	}
	return ConversationTurnStart{
		Conversation: conversation, Turn: turn, AssistantMessage: assistant,
	}, nil
}

// buildGrillingPrompt 把“同一 Thread 继续”和“无 Thread 恢复”收敛在一个地方。
func (app *Application) buildGrillingPrompt(
	ctx context.Context,
	conversation domain.Conversation,
	userContent string,
	retry bool,
) (string, error) {
	if conversation.PlanningThreadID != "" {
		if retry {
			return "继续上一轮尚未完成的需求澄清。不要重复已经回答的问题；信息充分时在末尾输出 " + grillingReadyMarker, nil
		}
		return "用户对上一轮问题的回答如下：\n\n" + userContent +
			"\n\n继续澄清最关键的不确定点；信息充分时在末尾输出 " + grillingReadyMarker, nil
	}

	// Thread 尚未建立时，数据库消息就是恢复来源，类似前端刷新后从 query cache 重新渲染列表。
	requirement, err := app.store.GetRequirement(ctx, conversation.RequirementID)
	if err != nil {
		return "", err
	}
	messages, err := app.store.ListConversationMessages(ctx, conversation.ID, 0)
	if err != nil {
		return "", err
	}
	var transcript strings.Builder
	for _, message := range messages {
		if message.Status == domain.ConversationMessageStreaming {
			continue
		}
		for _, segment := range message.Segments {
			if segment.Type == domain.ConversationSegmentText && strings.TrimSpace(segment.Content) != "" {
				transcript.WriteString(string(message.Role))
				transcript.WriteString(": ")
				transcript.WriteString(segment.Content)
				transcript.WriteString("\n")
			}
		}
	}
	return "你正在以只读方式澄清一个产品需求。一次只追问最关键的不确定点，不修改仓库。\n" +
		"需求标题：" + requirement.Title + "\n\n已有对话：\n" + transcript.String() +
		"\n信息充分时，在回复末尾输出 " + grillingReadyMarker, nil
}

// failStartedConversationTurn 保证 Prompt 构造失败时不会遗留永远 running 的占位消息。
func (app *Application) failStartedConversationTurn(
	ctx context.Context,
	started ConversationTurnStart,
	cause error,
) (EndConversationTurnResult, error) {
	terminalCtx := ctx
	if ctx.Err() != nil {
		terminalCtx = context.WithoutCancel(ctx)
	}
	ended, endErr := app.FailConversationTurn(terminalCtx, FailConversationTurnInput{
		RequirementID: started.Conversation.RequirementID, TurnID: started.Turn.ID,
		ExpectedRevision: started.Conversation.Revision, Reason: cause.Error(),
	})
	return ended, errors.Join(cause, endErr)
}

// ApplyConversationEvent 把一个稳定 Codex 事件折叠进当前 streaming assistant 消息。
//
// 前端视角：它像一个只更新单条 message entity 的 reducer。text delta 按 item ID 追加，
// item 生命周期按 item ID upsert，因此重绘页面不需要理解 Codex JSON-RPC 协议。
func (app *Application) ApplyConversationEvent(
	ctx context.Context,
	input ApplyConversationEventInput,
) (domain.ConversationMessage, error) {
	// 第 1 步：取消请求不读取 Store，也不解析可能很大的 JSON。
	if err := ctx.Err(); err != nil {
		return domain.ConversationMessage{}, err
	}

	// 第 2 步：规范化业务身份和事件类型，并限制 payload 大小。
	// 大小门禁类似前端限制上传体积，避免异常 Adapter 让应用层分配无限内存。
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	input.TurnID = strings.TrimSpace(input.TurnID)
	input.EventType = strings.TrimSpace(input.EventType)
	validType := input.EventType == ConversationEventTextDelta ||
		input.EventType == ConversationEventItemStarted ||
		input.EventType == ConversationEventItemCompleted
	if input.RequirementID == "" || input.TurnID == "" || input.ExpectedRevision < 1 ||
		!validType || len(input.Payload) == 0 || len(input.Payload) > maxConversationEventPayloadBytes {
		return domain.ConversationMessage{}, domain.ErrInvalidInput
	}

	// 第 3 步：从 Store 读取可信 Conversation、Turn 和当前 assistant 消息。
	// 不让事件携带整条消息，是为了避免外部事件覆盖已经保存的 segments。
	_, _, assistant, err := app.loadRunningConversationTurn(
		ctx,
		input.RequirementID,
		input.TurnID,
		input.ExpectedRevision,
	)
	if err != nil {
		return domain.ConversationMessage{}, err
	}

	// 第 4 步：复制 segments 后执行纯内存折叠。
	// Store 虽然已经返回深拷贝，这里再显式复制，强调 reducer 不修改输入 slice 的约束。
	updated := assistant
	updated.Segments = append([]domain.ConversationSegment(nil), assistant.Segments...)
	if err := foldConversationEvent(&updated, input.EventType, input.Payload); err != nil {
		return domain.ConversationMessage{}, err
	}

	// 第 5 步：用 streaming 状态做 CAS 保存。
	// 完成/失败/取消一旦先发生，迟到事件会收到 MessageConflict，不能把终态改回 streaming。
	if err := app.store.SaveConversationMessage(
		ctx,
		updated,
		domain.ConversationMessageStreaming,
	); err != nil {
		return domain.ConversationMessage{}, err
	}

	// 第 6 步：返回与 Store 中一致的新消息，供运行时发布 SSE 或更新调用方快照。
	return updated, nil
}

// RunConversationTurn 把一个已持久化 running Turn 接到 23E 只读 Codex Runtime。
//
// 这是原 MVP 的执行闭环：恢复或创建 Thread → 保存 Thread ID → 串行折叠事件 → 根据 Codex
// 终态调用 23D 完成、失败或取消。它不创建后台恢复器，也不增加新的调度行为。
func (app *Application) RunConversationTurn(
	ctx context.Context,
	input RunConversationTurnInput,
) (EndConversationTurnResult, error) {
	// 第 1 步：校验调用身份和 prompt。运行时没有装配时明确失败，不允许 nil pointer panic。
	if err := ctx.Err(); err != nil {
		return EndConversationTurnResult{}, err
	}
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	input.TurnID = strings.TrimSpace(input.TurnID)
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.RequirementID == "" || input.TurnID == "" || input.ExpectedRevision < 1 || input.Prompt == "" {
		return EndConversationTurnResult{}, domain.ErrInvalidInput
	}
	if app.conversation == nil {
		return EndConversationTurnResult{}, ErrConversationRuntimeUnavailable
	}

	// 第 2 步：先建立可取消 context 并登记到进程内 registry。
	// Store 保存“正在运行”，registry 只保存“怎样中止这个进程资源”，两者职责不能混在一起。
	// 登记发生在读取运行态之前，可避免取消请求恰好插入“已读取、尚未登记”的缝隙。
	runtimeCtx, cancelRuntime := context.WithCancel(ctx)
	active := &activeConversationTurn{cancel: cancelRuntime, done: make(chan struct{})}
	if err := app.registerActiveConversationTurn(input.TurnID, active); err != nil {
		cancelRuntime()
		return EndConversationTurnResult{}, err
	}
	defer func() {
		cancelRuntime()
		app.unregisterActiveConversationTurn(input.TurnID, active)
	}()

	// 第 3 步：读取 23C 已保存的 running 三元组，并从 Conversation 选择要续接的 Thread。
	conversation, turn, _, err := app.loadRunningConversationTurn(
		runtimeCtx,
		input.RequirementID,
		input.TurnID,
		input.ExpectedRevision,
	)
	if err != nil {
		// 取消可能恰好发生在 registry 登记后、运行态读取完成前；此时仍要收口数据库状态。
		if runtimeCtx.Err() != nil {
			_, cancelErr := app.CancelConversationTurn(
				context.WithoutCancel(runtimeCtx), input.RequirementID, input.TurnID, input.ExpectedRevision,
			)
			return EndConversationTurnResult{}, errors.Join(err, cancelErr)
		}
		return EndConversationTurnResult{}, err
	}
	threadID := conversation.PlanningThreadID
	if conversation.Phase == domain.ConversationPhaseImplement {
		threadID = conversation.ImplementationThreadID
	}
	currentRevision := input.ExpectedRevision

	// 第 4 步：定义 Session callback。Runtime 会在 Codex turn/start 之前调用它。
	// 只有数据库同时保存 Conversation 和 Turn 的 Thread ID 后，Codex 才能真正开始输出。
	saveSession := func(sessionCtx context.Context, openedThreadID string) error {
		now, err := app.conversationNow()
		if err != nil {
			return err
		}
		updatedConversation, updatedTurn, err := conversation.AttachThread(turn, openedThreadID, now)
		if err != nil {
			return err
		}
		if err := app.store.AttachConversationThread(sessionCtx, store.AttachConversationThreadInput{
			Conversation:       updatedConversation,
			ExpectedRevision:   currentRevision,
			Turn:               updatedTurn,
			ExpectedTurnStatus: domain.ConversationTurnRunning,
		}); err != nil {
			return err
		}
		conversation = updatedConversation
		turn = updatedTurn
		currentRevision = updatedConversation.Revision
		return nil
	}

	// 第 5 步：定义 Event callback。turn/session/terminal 通知只控制 Runtime 生命周期；
	// 页面真正需要的 text/item 事件才交给 23D reducer，避免未知事件污染消息模型。
	applyEvent := func(eventCtx context.Context, eventType string, payload []byte) error {
		switch eventType {
		case ConversationEventTextDelta, ConversationEventItemStarted, ConversationEventItemCompleted:
			_, err := app.ApplyConversationEvent(eventCtx, ApplyConversationEventInput{
				RequirementID:    input.RequirementID,
				TurnID:           input.TurnID,
				ExpectedRevision: currentRevision,
				EventType:        eventType,
				Payload:          payload,
			})
			return err
		default:
			return nil
		}
	}

	// 第 6 步：运行 Codex。另一个 HTTP 请求调用 active.cancel 后，取消会沿 runtimeCtx
	// 传播到 Adapter，再由 23E 发送 turn/interrupt。
	result, runErr := app.conversation.Run(runtimeCtx, ConversationRuntimeRequest{
		Prompt: input.Prompt, ThreadID: threadID,
	}, applyEvent, saveSession)

	// 第 7 步：即使请求刚被取消，也必须把已开始的数据库 Turn 收口。
	// WithoutCancel 只用于本地 Store 写入，不会继续运行 Codex 或启动新的外部副作用。
	terminalCtx := runtimeCtx
	if runtimeCtx.Err() != nil {
		terminalCtx = context.WithoutCancel(runtimeCtx)
	}
	if runErr != nil {
		if result.Status == "interrupted" || runtimeCtx.Err() != nil {
			ended, endErr := app.CancelConversationTurn(
				terminalCtx,
				input.RequirementID,
				input.TurnID,
				currentRevision,
			)
			return ended, errors.Join(runErr, endErr)
		}
		reason := strings.TrimSpace(runErr.Error())
		ended, endErr := app.FailConversationTurn(terminalCtx, FailConversationTurnInput{
			RequirementID: input.RequirementID, TurnID: input.TurnID,
			ExpectedRevision: currentRevision, Reason: reason,
		})
		return ended, errors.Join(runErr, endErr)
	}

	// 第 8 步：原 MVP 用控制标记表示当前产物可确认；没有标记时继续等待用户回复。
	readyForConfirmation := strings.Contains(result.Summary, grillingReadyMarker)
	return app.CompleteConversationTurn(terminalCtx, FinishConversationTurnInput{
		RequirementID: input.RequirementID, TurnID: input.TurnID,
		ExpectedRevision: currentRevision, ReadyForConfirmation: readyForConfirmation,
	})
}

// CompleteConversationTurn 把成功的 Codex Turn、对话头和 assistant 消息一起完成。
func (app *Application) CompleteConversationTurn(
	ctx context.Context,
	input FinishConversationTurnInput,
) (EndConversationTurnResult, error) {
	// 第 1 步：规范化身份；readyForConfirmation 是服务端解析产物后得到的布尔事实。
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	input.TurnID = strings.TrimSpace(input.TurnID)
	if err := validateConversationEndInput(ctx, input.RequirementID, input.TurnID, input.ExpectedRevision); err != nil {
		return EndConversationTurnResult{}, err
	}

	// 第 2 步：读取同一 running Turn 的三份当前值，后续状态机只基于这些可信事实。
	conversation, turn, assistant, err := app.loadRunningConversationTurn(
		ctx,
		input.RequirementID,
		input.TurnID,
		input.ExpectedRevision,
	)
	if err != nil {
		return EndConversationTurnResult{}, err
	}
	if len(assistant.Segments) == 0 {
		return EndConversationTurnResult{}, domain.ErrInvalidInput
	}

	// 第 3 步：读取一次结束时间，并让 23A 决定 awaiting-user/awaiting-confirmation。
	finishedAt, err := app.conversationNow()
	if err != nil {
		return EndConversationTurnResult{}, err
	}
	finishedConversation, finishedTurn, err := conversation.CompleteTurn(
		turn,
		input.ReadyForConfirmation,
		finishedAt,
	)
	if err != nil {
		return EndConversationTurnResult{}, err
	}
	assistant.Status = domain.ConversationMessageCompleted

	// 第 4 步：使用 Store 事务一次提交三份终态，成功后才向调用方暴露完成结果。
	return app.persistConversationTurnEnd(ctx, conversation, finishedConversation, finishedTurn, assistant)
}

// FailConversationTurn 保存失败原因，并把 running/streaming 三元组原子收口为 failed。
func (app *Application) FailConversationTurn(
	ctx context.Context,
	input FailConversationTurnInput,
) (EndConversationTurnResult, error) {
	// 第 1 步：先清理失败原因。空原因无法支持页面解释和重试诊断，因此不能落库。
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	input.TurnID = strings.TrimSpace(input.TurnID)
	input.Reason = strings.TrimSpace(input.Reason)
	if err := validateConversationEndInput(ctx, input.RequirementID, input.TurnID, input.ExpectedRevision); err != nil {
		return EndConversationTurnResult{}, err
	}
	if input.Reason == "" {
		return EndConversationTurnResult{}, domain.ErrInvalidInput
	}

	// 第 2 步：读取当前运行态；若完成或取消已经抢先提交，这里会返回冲突而不是覆盖终态。
	conversation, turn, assistant, err := app.loadRunningConversationTurn(
		ctx,
		input.RequirementID,
		input.TurnID,
		input.ExpectedRevision,
	)
	if err != nil {
		return EndConversationTurnResult{}, err
	}

	// 第 3 步：通过领域状态机同步生成 failed Conversation 和 failed Turn。
	finishedAt, err := app.conversationNow()
	if err != nil {
		return EndConversationTurnResult{}, err
	}
	failedConversation, failedTurn, err := conversation.FailTurn(turn, input.Reason, finishedAt)
	if err != nil {
		return EndConversationTurnResult{}, err
	}
	assistant.Status = domain.ConversationMessageFailed

	// 第 4 步：原子保存；已有文本和活动 segments 会保留，页面还能展示失败前已经发生了什么。
	return app.persistConversationTurnEnd(ctx, conversation, failedConversation, failedTurn, assistant)
}

// CancelConversationTurn 把用户取消的运行态原子收口，并保留取消前的消息片段。
func (app *Application) CancelConversationTurn(
	ctx context.Context,
	requirementID string,
	turnID string,
	expectedRevision int64,
) (EndConversationTurnResult, error) {
	// 第 1 步：取消和失败使用同一套身份/revision 门禁，但取消不需要失败原因。
	requirementID = strings.TrimSpace(requirementID)
	turnID = strings.TrimSpace(turnID)
	if err := validateConversationEndInput(ctx, requirementID, turnID, expectedRevision); err != nil {
		return EndConversationTurnResult{}, err
	}

	// 第 2 步：先读取持久化运行态。真正向 Codex 发送 interrupt 属于后续运行时，不在这里做。
	conversation, turn, assistant, err := app.loadRunningConversationTurn(
		ctx,
		requirementID,
		turnID,
		expectedRevision,
	)
	if err != nil {
		return EndConversationTurnResult{}, err
	}

	// 第 3 步：领域状态机产生 cancelled Conversation/Turn，消息同步改成 cancelled。
	finishedAt, err := app.conversationNow()
	if err != nil {
		return EndConversationTurnResult{}, err
	}
	cancelledConversation, cancelledTurn, err := conversation.CancelTurn(turn, finishedAt)
	if err != nil {
		return EndConversationTurnResult{}, err
	}
	assistant.Status = domain.ConversationMessageCancelled

	// 第 4 步：一次事务提交三份取消事实，避免迟到的完成事件覆盖用户取消。
	return app.persistConversationTurnEnd(
		ctx,
		conversation,
		cancelledConversation,
		cancelledTurn,
		assistant,
	)
}

// loadRunningConversationTurn 从分表模型中重新组装当前运行三元组。
// 页面可能缓存旧快照，因此这里仍以 Store 为准，并用 revision 提前拒绝过期调用。
func (app *Application) loadRunningConversationTurn(
	ctx context.Context,
	requirementID string,
	turnID string,
	expectedRevision int64,
) (domain.Conversation, domain.ConversationTurn, domain.ConversationMessage, error) {
	conversation, err := app.store.GetConversationByRequirement(ctx, requirementID)
	if err != nil {
		return domain.Conversation{}, domain.ConversationTurn{}, domain.ConversationMessage{}, err
	}
	if conversation.Revision != expectedRevision {
		return domain.Conversation{}, domain.ConversationTurn{}, domain.ConversationMessage{},
			store.ErrConversationConflict
	}
	turn, err := app.store.GetConversationTurn(ctx, turnID)
	if err != nil {
		return domain.Conversation{}, domain.ConversationTurn{}, domain.ConversationMessage{}, err
	}
	if conversation.Status != domain.ConversationStatusRunning ||
		turn.ConversationID != conversation.ID || turn.Status != domain.ConversationTurnRunning {
		return domain.Conversation{}, domain.ConversationTurn{}, domain.ConversationMessage{},
			domain.ErrInvalidTransition
	}
	messages, err := app.store.ListConversationMessages(ctx, conversation.ID, 0)
	if err != nil {
		return domain.Conversation{}, domain.ConversationTurn{}, domain.ConversationMessage{}, err
	}
	for _, message := range messages {
		if message.TurnID == turn.ID && message.Role == domain.ConversationRoleAssistant &&
			message.Status == domain.ConversationMessageStreaming {
			return conversation, turn, message, nil
		}
	}
	return domain.Conversation{}, domain.ConversationTurn{}, domain.ConversationMessage{}, store.ErrNotFound
}

// persistConversationTurnEnd 把三个领域结果转换成 23D Store 事务输入。
func (app *Application) persistConversationTurnEnd(
	ctx context.Context,
	previous domain.Conversation,
	conversation domain.Conversation,
	turn domain.ConversationTurn,
	assistant domain.ConversationMessage,
) (EndConversationTurnResult, error) {
	err := app.store.FinishConversationTurn(ctx, store.FinishConversationTurnInput{
		Conversation:            conversation,
		ExpectedRevision:        previous.Revision,
		Turn:                    turn,
		ExpectedTurnStatus:      domain.ConversationTurnRunning,
		AssistantMessage:        assistant,
		ExpectedAssistantStatus: domain.ConversationMessageStreaming,
	})
	if err != nil {
		return EndConversationTurnResult{}, err
	}
	return EndConversationTurnResult{
		Conversation:     conversation,
		Turn:             turn,
		AssistantMessage: assistant,
	}, nil
}

// registerActiveConversationTurn 保证同一持久化 Turn 在一个进程里最多启动一个 Runtime。
func (app *Application) registerActiveConversationTurn(
	turnID string,
	active *activeConversationTurn,
) error {
	app.activeConversationMu.Lock()
	defer app.activeConversationMu.Unlock()
	if _, exists := app.activeConversationTurns[turnID]; exists {
		return store.ErrAlreadyExists
	}
	app.activeConversationTurns[turnID] = active
	return nil
}

// unregisterActiveConversationTurn 只删除自己登记的句柄，并在终态落库后唤醒取消请求。
func (app *Application) unregisterActiveConversationTurn(
	turnID string,
	active *activeConversationTurn,
) {
	app.activeConversationMu.Lock()
	if app.activeConversationTurns[turnID] == active {
		delete(app.activeConversationTurns, turnID)
	}
	app.activeConversationMu.Unlock()
	close(active.done)
}

// loadEndedConversationTurn 为等待 interrupt 的 HTTP 请求重新组装最终快照。
func (app *Application) loadEndedConversationTurn(
	ctx context.Context,
	requirementID string,
	turnID string,
) (EndConversationTurnResult, error) {
	conversation, err := app.store.GetConversationByRequirement(ctx, requirementID)
	if err != nil {
		return EndConversationTurnResult{}, err
	}
	turn, err := app.store.GetConversationTurn(ctx, turnID)
	if err != nil {
		return EndConversationTurnResult{}, err
	}
	if turn.ConversationID != conversation.ID || turn.Status == domain.ConversationTurnRunning {
		return EndConversationTurnResult{}, domain.ErrInvalidTransition
	}
	messages, err := app.store.ListConversationMessages(ctx, conversation.ID, 0)
	if err != nil {
		return EndConversationTurnResult{}, err
	}
	for _, message := range messages {
		if message.TurnID == turn.ID && message.Role == domain.ConversationRoleAssistant &&
			message.Status != domain.ConversationMessageStreaming {
			return EndConversationTurnResult{
				Conversation: conversation, Turn: turn, AssistantMessage: message,
			}, nil
		}
	}
	return EndConversationTurnResult{}, store.ErrNotFound
}

func (app *Application) conversationNow() (time.Time, error) {
	now := app.now()
	if now.IsZero() {
		return time.Time{}, domain.ErrInvalidTime
	}
	return now.UTC(), nil
}

func validateConversationEndInput(
	ctx context.Context,
	requirementID string,
	turnID string,
	expectedRevision int64,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if requirementID == "" || turnID == "" || expectedRevision < 1 {
		return domain.ErrInvalidInput
	}
	return nil
}

func foldConversationEvent(
	message *domain.ConversationMessage,
	eventType string,
	payload []byte,
) error {
	if eventType == ConversationEventTextDelta {
		var event struct {
			ItemID string `json:"itemId"`
			Delta  string `json:"delta"`
		}
		if err := json.Unmarshal(payload, &event); err != nil {
			return domain.ErrInvalidInput
		}
		event.ItemID = strings.TrimSpace(event.ItemID)
		if event.ItemID == "" || event.Delta == "" {
			return domain.ErrInvalidInput
		}
		return upsertConversationText(message, event.ItemID, event.Delta, true)
	}

	var event struct {
		ItemID   string `json:"itemId"`
		ItemType string `json:"itemType"`
		Status   string `json:"status"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return domain.ErrInvalidInput
	}
	event.ItemID = strings.TrimSpace(event.ItemID)
	event.ItemType = strings.TrimSpace(event.ItemType)
	event.Message = strings.TrimSpace(event.Message)
	if event.ItemID == "" || event.ItemType == "" {
		return domain.ErrInvalidInput
	}
	normalizedType := normalizeConversationItemType(event.ItemType)
	if strings.Contains(normalizedType, "message") {
		if event.Message == "" {
			return nil
		}
		return upsertConversationText(message, event.ItemID, event.Message, false)
	}
	status := domain.ConversationSegmentRunning
	if eventType == ConversationEventItemCompleted || strings.EqualFold(event.Status, "completed") {
		status = domain.ConversationSegmentCompleted
	}
	if strings.EqualFold(event.Status, "failed") || normalizedType == "error" {
		status = domain.ConversationSegmentFailed
	}
	label := "Agent 活动"
	if strings.Contains(normalizedType, "file") {
		label = "文件检查"
	}
	detail := event.Message
	if detail == "" {
		detail = event.ItemType
	}
	segment := domain.ConversationSegment{
		ID: event.ItemID, Type: domain.ConversationSegmentActivity,
		Label: label, Detail: detail, Status: status,
	}
	for index := range message.Segments {
		if message.Segments[index].ID == event.ItemID {
			if message.Segments[index].Type != domain.ConversationSegmentActivity {
				return domain.ErrInvalidInput
			}
			message.Segments[index] = segment
			return nil
		}
	}
	message.Segments = append(message.Segments, segment)
	return nil
}

func upsertConversationText(
	message *domain.ConversationMessage,
	itemID string,
	content string,
	appendDelta bool,
) error {
	for index := range message.Segments {
		if message.Segments[index].ID != itemID {
			continue
		}
		if message.Segments[index].Type != domain.ConversationSegmentText {
			return domain.ErrInvalidInput
		}
		if appendDelta {
			remaining := maxConversationTextBytes - len(message.Segments[index].Content)
			if remaining > 0 {
				message.Segments[index].Content += truncateConversationText(content, remaining)
			}
		} else {
			message.Segments[index].Content = truncateConversationText(content, maxConversationTextBytes)
		}
		return nil
	}
	// 某些流会先发换行再发正文。单独的空白不是合法 text segment，先忽略，后续正文仍可创建。
	if strings.TrimSpace(content) == "" {
		return nil
	}
	message.Segments = append(message.Segments, domain.ConversationSegment{
		ID: itemID, Type: domain.ConversationSegmentText,
		Content: truncateConversationText(content, maxConversationTextBytes),
	})
	return nil
}

func truncateConversationText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func normalizeConversationItemType(value string) string {
	replacer := strings.NewReplacer("_", "", "-", "", " ", "")
	return strings.ToLower(replacer.Replace(value))
}

// newEntityID 统一处理应用层生成的 ID。
//
// prefix 是服务端固定字符串，不来自浏览器。生成器只提供随机部分；TrimSpace 后为空说明
// 依赖实现损坏，应用必须在写 Store 之前失败。
func (app *Application) newEntityID(prefix string) (string, error) {
	value := strings.TrimSpace(app.newID())
	if value == "" {
		return "", domain.ErrInvalidInput
	}
	return prefix + value, nil
}

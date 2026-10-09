package store

import (
	"context"
	"slices"
	"strings"
	"time"

	"product-agent-workbench/internal/domain"
)

// CreateRequirementConversationInput 是新需求第一次落库的完整事务输入。
//
// 前端类比：它像一次 mutation 同时写入三个 normalized entities；任意一项失败时，
// Requirement、Conversation 和初始 system 消息都不能留下半成品。
type CreateRequirementConversationInput struct {
	Requirement    domain.Requirement
	Conversation   domain.Conversation
	InitialMessage domain.ConversationMessage
}

// Validate 检查三个实体能否组成同一个新需求聚合。
func (input CreateRequirementConversationInput) Validate() error {
	requirement := input.Requirement
	conversation := input.Conversation
	message := input.InitialMessage
	if strings.TrimSpace(requirement.ID) == "" || strings.TrimSpace(requirement.ProjectID) == "" ||
		strings.TrimSpace(requirement.Title) == "" || requirement.Stage != domain.RequirementStageGrilling ||
		requirement.Revision != 1 {
		return domain.ErrInvalidInput
	}
	if err := domain.ValidateConversation(conversation); err != nil {
		return err
	}
	if err := domain.ValidateConversationMessage(message); err != nil {
		return err
	}
	validIdentity := conversation.RequirementID == requirement.ID &&
		message.ConversationID == conversation.ID
	validInitialConversation := conversation.Phase == domain.ConversationPhaseGrilling &&
		conversation.Status == domain.ConversationStatusIdle && conversation.Revision == 1
	validInitialMessage := message.TurnID == "" && message.Sequence == 1 &&
		message.Role == domain.ConversationRoleSystem &&
		message.Phase == domain.ConversationPhaseGrilling &&
		message.Status == domain.ConversationMessageCompleted && len(message.Segments) > 0
	if !validIdentity || !validInitialConversation || !validInitialMessage ||
		message.RecordedAt.Before(conversation.CreatedAt) {
		return domain.ErrInvalidInput
	}
	return nil
}

// BeginConversationTurnInput 是开始一次 Turn 时必须同时提交的四份事实。
// Conversation 和 Turn 来自 domain.Conversation.BeginTurn，消息则形成页面立即可见的骨架。
type BeginConversationTurnInput struct {
	Conversation     domain.Conversation
	ExpectedRevision int64
	Turn             domain.ConversationTurn
	UserMessage      domain.ConversationMessage
	AssistantMessage domain.ConversationMessage
}

// RetryConversationTurnInput 是“失败/取消后重试”使用的原子事务输入。
//
// 前端类比：重试一次 failed mutation 会新建请求记录和新的 assistant 占位项，
// 但不会把上一次用户输入再 append 一遍。因此这里没有 UserMessage。
type RetryConversationTurnInput struct {
	Conversation     domain.Conversation
	ExpectedRevision int64
	Turn             domain.ConversationTurn
	AssistantMessage domain.ConversationMessage
}

// Validate 检查重试产生的运行态、Turn 和 assistant 占位消息是否属于同一次提交。
func (input RetryConversationTurnInput) Validate() error {
	conversation := input.Conversation
	turn := input.Turn
	message := input.AssistantMessage
	if input.ExpectedRevision < 1 || conversation.Revision != input.ExpectedRevision+1 {
		return domain.ErrInvalidInput
	}
	if err := domain.ValidateConversation(conversation); err != nil {
		return err
	}
	if err := domain.ValidateConversationTurn(turn); err != nil {
		return err
	}
	if err := domain.ValidateConversationMessage(message); err != nil {
		return err
	}
	validTurn := conversation.Status == domain.ConversationStatusRunning &&
		turn.Status == domain.ConversationTurnRunning && turn.ConversationID == conversation.ID &&
		turn.Phase == conversation.Phase && turn.StartedAt.Equal(conversation.UpdatedAt)
	validMessage := message.ConversationID == conversation.ID && message.TurnID == turn.ID &&
		message.Role == domain.ConversationRoleAssistant && message.Phase == turn.Phase &&
		message.Status == domain.ConversationMessageStreaming && len(message.Segments) == 0 &&
		!message.RecordedAt.Before(turn.StartedAt)
	if !validTurn || !validMessage {
		return domain.ErrInvalidInput
	}
	return nil
}

// FinishConversationTurnInput 是结束一次 Turn 时必须一起提交的三份终态。
//
// 前端类比：Conversation、Turn 和 assistant Message 是同一次 mutation 影响的三个 cache entity；
// 不能让其中一个先成功、另外两个仍停留在 running/streaming。
type FinishConversationTurnInput struct {
	Conversation            domain.Conversation
	ExpectedRevision        int64
	Turn                    domain.ConversationTurn
	ExpectedTurnStatus      domain.ConversationTurnStatus
	AssistantMessage        domain.ConversationMessage
	ExpectedAssistantStatus domain.ConversationMessageStatus
}

// AttachConversationThreadInput 是 Codex Thread 建立后、Turn 启动前的原子会话绑定。
type AttachConversationThreadInput struct {
	Conversation       domain.Conversation
	ExpectedRevision   int64
	Turn               domain.ConversationTurn
	ExpectedTurnStatus domain.ConversationTurnStatus
}

// ValidateTransition 使用当前持久化运行态重放领域方法，拒绝伪造 Thread 归属或 revision。
func (input AttachConversationThreadInput) ValidateTransition(
	current domain.Conversation,
	currentTurn domain.ConversationTurn,
) error {
	if input.ExpectedRevision < 1 || current.Revision != input.ExpectedRevision {
		return ErrConversationConflict
	}
	if input.ExpectedTurnStatus != domain.ConversationTurnRunning ||
		currentTurn.Status != input.ExpectedTurnStatus {
		return ErrConversationTurnConflict
	}
	threadID := input.Turn.CodexThreadID
	expectedConversation, expectedTurn, err := current.AttachThread(
		currentTurn,
		threadID,
		input.Conversation.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if expectedConversation != input.Conversation || !sameConversationTurn(expectedTurn, input.Turn) {
		return domain.ErrInvalidInput
	}
	return nil
}

// Validate 检查三个终态是否属于同一 Turn，并且成功、失败、取消状态彼此一致。
func (input FinishConversationTurnInput) Validate() error {
	conversation := input.Conversation
	turn := input.Turn
	message := input.AssistantMessage
	if input.ExpectedRevision < 1 || conversation.Revision != input.ExpectedRevision+1 ||
		input.ExpectedTurnStatus != domain.ConversationTurnRunning ||
		input.ExpectedAssistantStatus != domain.ConversationMessageStreaming {
		return domain.ErrInvalidInput
	}
	if err := domain.ValidateConversation(conversation); err != nil {
		return err
	}
	if err := domain.ValidateConversationTurn(turn); err != nil {
		return err
	}
	if err := domain.ValidateConversationMessage(message); err != nil {
		return err
	}
	validIdentity := turn.ConversationID == conversation.ID && message.ConversationID == conversation.ID &&
		message.TurnID == turn.ID && message.Role == domain.ConversationRoleAssistant &&
		message.Phase == turn.Phase && turn.Phase == conversation.Phase
	validTime := turn.FinishedAt != nil && turn.FinishedAt.Equal(conversation.UpdatedAt)
	validTerminal := false
	switch turn.Status {
	case domain.ConversationTurnCompleted:
		validTerminal = message.Status == domain.ConversationMessageCompleted && len(message.Segments) > 0 &&
			(conversation.Status == domain.ConversationStatusAwaitingUser ||
				conversation.Status == domain.ConversationStatusAwaitingConfirmation)
	case domain.ConversationTurnFailed:
		validTerminal = message.Status == domain.ConversationMessageFailed &&
			conversation.Status == domain.ConversationStatusFailed &&
			strings.TrimSpace(turn.FailureReason) != "" && turn.FailureReason == conversation.Error
	case domain.ConversationTurnCancelled:
		validTerminal = message.Status == domain.ConversationMessageCancelled &&
			conversation.Status == domain.ConversationStatusCancelled
	}
	if !validIdentity || !validTime || !validTerminal {
		return domain.ErrInvalidInput
	}
	return nil
}

// ValidateTransition 用当前持久化值重放 23A 状态机，防止调用方直接伪造终态快照。
func (input FinishConversationTurnInput) ValidateTransition(
	current domain.Conversation,
	currentTurn domain.ConversationTurn,
) error {
	if current.Revision != input.ExpectedRevision {
		return ErrConversationConflict
	}
	if currentTurn.Status != input.ExpectedTurnStatus {
		return ErrConversationTurnConflict
	}
	finishedAt := input.Turn.FinishedAt
	if finishedAt == nil {
		return domain.ErrInvalidTime
	}
	var expectedConversation domain.Conversation
	var expectedTurn domain.ConversationTurn
	var err error
	switch input.Turn.Status {
	case domain.ConversationTurnCompleted:
		expectedConversation, expectedTurn, err = current.CompleteTurn(
			currentTurn,
			input.Conversation.TurnReadyForConfirmation,
			*finishedAt,
		)
	case domain.ConversationTurnFailed:
		expectedConversation, expectedTurn, err = current.FailTurn(
			currentTurn,
			input.Turn.FailureReason,
			*finishedAt,
		)
	case domain.ConversationTurnCancelled:
		expectedConversation, expectedTurn, err = current.CancelTurn(currentTurn, *finishedAt)
	default:
		return domain.ErrInvalidTransition
	}
	if err != nil {
		return err
	}
	if expectedConversation != input.Conversation || !sameConversationTurn(expectedTurn, input.Turn) {
		return domain.ErrInvalidInput
	}
	return nil
}

// ValidateCurrentAssistant 防止终态提交覆盖刚刚写入的较新事件片段。
// expected status 只能保护 streaming 与终态竞争；segments 比较额外保护 streaming 与 streaming。
func (input FinishConversationTurnInput) ValidateCurrentAssistant(
	current domain.ConversationMessage,
) error {
	message := input.AssistantMessage
	if current.Status != input.ExpectedAssistantStatus {
		return ErrConversationMessageConflict
	}
	validIdentity := current.ID == message.ID && current.ConversationID == message.ConversationID &&
		current.TurnID == message.TurnID && current.Sequence == message.Sequence &&
		current.Role == message.Role && current.Phase == message.Phase &&
		current.RecordedAt.Equal(message.RecordedAt)
	if !validIdentity {
		return domain.ErrInvalidInput
	}
	if !slices.Equal(current.Segments, message.Segments) {
		return ErrConversationMessageConflict
	}
	return nil
}

// sameConversationTurn 按时间值比较 FinishedAt，而不是比较两个时间指针的内存地址。
func sameConversationTurn(left, right domain.ConversationTurn) bool {
	leftFinishedAt := left.FinishedAt
	rightFinishedAt := right.FinishedAt
	left.FinishedAt = nil
	right.FinishedAt = nil
	if left != right {
		return false
	}
	if leftFinishedAt == nil || rightFinishedAt == nil {
		return leftFinishedAt == nil && rightFinishedAt == nil
	}
	return leftFinishedAt.Equal(*rightFinishedAt)
}

// Validate 检查输入内部关系；与数据库当前快照的 CAS 比较由具体 Adapter 在事务内完成。
func (input BeginConversationTurnInput) Validate() error {
	conversation := input.Conversation
	turn := input.Turn
	userMessage := input.UserMessage
	assistantMessage := input.AssistantMessage
	if input.ExpectedRevision < 1 || conversation.Revision != input.ExpectedRevision+1 {
		return domain.ErrInvalidInput
	}
	if err := domain.ValidateConversation(conversation); err != nil {
		return err
	}
	if err := domain.ValidateConversationTurn(turn); err != nil {
		return err
	}
	if err := domain.ValidateConversationMessage(userMessage); err != nil {
		return err
	}
	if err := domain.ValidateConversationMessage(assistantMessage); err != nil {
		return err
	}
	validTurn := conversation.Status == domain.ConversationStatusRunning &&
		turn.Status == domain.ConversationTurnRunning && turn.ConversationID == conversation.ID &&
		turn.Phase == conversation.Phase && turn.StartedAt.Equal(conversation.UpdatedAt)
	validUserMessage := userMessage.ConversationID == conversation.ID && userMessage.TurnID == turn.ID &&
		userMessage.Role == domain.ConversationRoleUser && userMessage.Phase == turn.Phase &&
		userMessage.Status == domain.ConversationMessageCompleted && len(userMessage.Segments) > 0
	validAssistantMessage := assistantMessage.ConversationID == conversation.ID &&
		assistantMessage.TurnID == turn.ID && assistantMessage.Role == domain.ConversationRoleAssistant &&
		assistantMessage.Phase == turn.Phase &&
		assistantMessage.Status == domain.ConversationMessageStreaming &&
		assistantMessage.Sequence == userMessage.Sequence+1
	validTimes := !assistantMessage.RecordedAt.Before(turn.StartedAt) &&
		!assistantMessage.RecordedAt.Before(userMessage.RecordedAt)
	if !validTurn || !validUserMessage || !validAssistantMessage || !validTimes {
		return domain.ErrInvalidInput
	}
	return nil
}

// Store 描述工作流真正需要的持久化能力。
//
// 先在 internal/store/memory 中实现它并通过契约测试，再创建 SQLite Adapter。不要为了
// “以后可能替换数据库”继续拆出 RequirementRepository、TaskRepository 等浅接口。
type Store interface {
	// CreateRequirementConversation 原子创建 Requirement、对话头和初始 system 消息。
	CreateRequirementConversation(ctx context.Context, input CreateRequirementConversationInput) error

	// CreateRequirement 首次保存 Requirement。
	//
	// requirement.ID 已存在时返回 ErrAlreadyExists，不能覆盖旧值。
	CreateRequirement(ctx context.Context, requirement domain.Requirement) error

	// GetRequirement 按 ID 返回 Requirement 的值副本。
	GetRequirement(ctx context.Context, id string) (domain.Requirement, error)

	// SaveRequirement 使用乐观锁更新 Requirement。
	//
	// 只有已保存版本等于 expectedRevision 时才能更新；成功后 Store 把 Revision
	// 设置为 expectedRevision+1，并返回这个新版本。冲突时返回 ErrRevisionConflict，
	// 且不能修改已经保存的值。
	SaveRequirement(
		ctx context.Context,
		requirement domain.Requirement,
		expectedRevision int64,
	) (int64, error)

	// CreateConversation 为 Requirement 创建唯一逻辑对话。
	CreateConversation(ctx context.Context, conversation domain.Conversation) error

	// GetConversationByRequirement 读取 Requirement 对应的对话头；消息和 Turn 单独读取。
	GetConversationByRequirement(ctx context.Context, requirementID string) (domain.Conversation, error)

	// SaveConversation 使用 revision 乐观锁更新对话头。
	SaveConversation(
		ctx context.Context,
		conversation domain.Conversation,
		expectedRevision int64,
	) (int64, error)

	// BeginConversationTurn 原子更新对话头、创建 Turn，并追加 user/assistant 两条消息。
	BeginConversationTurn(ctx context.Context, input BeginConversationTurnInput) error

	// RetryConversationTurn 开始不新增 user 消息的重试 Turn。
	RetryConversationTurn(ctx context.Context, input RetryConversationTurnInput) error

	// AttachConversationThread 原子保存 Conversation 与当前 Turn 共用的 Codex Thread ID。
	AttachConversationThread(ctx context.Context, input AttachConversationThreadInput) error

	// FinishConversationTurn 原子结束 Conversation、Turn 和 streaming assistant 消息。
	FinishConversationTurn(ctx context.Context, input FinishConversationTurnInput) error

	// CreateConversationTurn 连续追加一条 running Turn；同一对话最多只能有一条 running Turn。
	CreateConversationTurn(ctx context.Context, turn domain.ConversationTurn) error

	GetConversationTurn(ctx context.Context, id string) (domain.ConversationTurn, error)
	ListConversationTurns(ctx context.Context, conversationID string) ([]domain.ConversationTurn, error)

	// SaveConversationTurn 用状态 CAS 结束 Turn，防止取消和完成互相覆盖。
	SaveConversationTurn(
		ctx context.Context,
		turn domain.ConversationTurn,
		expectedStatus domain.ConversationTurnStatus,
	) error

	// AppendConversationMessages 原子追加连续消息；任意一条无效时整批不写入。
	AppendConversationMessages(ctx context.Context, messages []domain.ConversationMessage) error

	// SaveConversationMessage 用状态 CAS 更新流式消息的稳定快照。
	SaveConversationMessage(
		ctx context.Context,
		message domain.ConversationMessage,
		expectedStatus domain.ConversationMessageStatus,
	) error

	ListConversationMessages(
		ctx context.Context,
		conversationID string,
		afterSequence int64,
	) ([]domain.ConversationMessage, error)

	// ConfirmGrilling 原子保存共同理解，并把 Requirement 从 grilling 推进到 spec。
	ConfirmGrilling(
		ctx context.Context,
		requirement domain.Requirement,
		expectedRevision int64,
		understanding domain.GrillingUnderstanding,
	) (int64, error)

	GetGrillingUnderstanding(ctx context.Context, requirementID string) (domain.GrillingUnderstanding, error)

	// CreateSpecVersion 原子保存新草稿、失效旧草稿并推进 Requirement revision。
	CreateSpecVersion(
		ctx context.Context,
		requirement domain.Requirement,
		expectedRevision int64,
		version domain.SpecVersion,
	) (int64, error)
	ListSpecVersions(ctx context.Context, requirementID string) ([]domain.SpecVersion, error)

	// ConfirmSpecVersion 原子冻结当前草稿，并把 Requirement 推进到 tickets。
	ConfirmSpecVersion(
		ctx context.Context,
		requirement domain.Requirement,
		expectedRevision int64,
		versionID string,
		confirmedAt time.Time,
	) (int64, error)

	// CreateTicketVersion 原子保存 Ticket DAG 草稿、失效旧草稿并推进 Requirement revision。
	CreateTicketVersion(
		ctx context.Context,
		requirement domain.Requirement,
		expectedRevision int64,
		version domain.TicketVersion,
	) (int64, error)
	ListTicketVersions(ctx context.Context, requirementID string) ([]domain.TicketVersion, error)

	// ConfirmTicketVersion 原子冻结当前 Ticket DAG，并把 Requirement 推进到 implementation。
	ConfirmTicketVersion(
		ctx context.Context,
		requirement domain.Requirement,
		expectedRevision int64,
		versionID string,
		confirmedAt time.Time,
	) (int64, error)

	// EnqueueTask 首次保存 queued TaskRun。
	//
	// 同一 Requirement 的不同 Ticket 可以同时活动；同一 Ticket 已有 queued、claimed 或
	// running attempt 时返回 ErrAlreadyExists，避免 Scheduler 重复入队。
	EnqueueTask(ctx context.Context, run domain.TaskRun) error

	// GetTask 按 ID 返回 TaskRun 的深拷贝。
	//
	// TaskRun 含有时间和结果指针，只复制结构体仍会泄漏 Store 内部可变数据。
	GetTask(ctx context.Context, id string) (domain.TaskRun, error)

	// ListTasksByTicketVersion 返回一个 Requirement 在指定 TicketVersion 下的全部执行历史。
	//
	// 结果按稳定入队顺序排列并使用深拷贝；Requirement 不存在时返回 ErrNotFound。
	ListTasksByTicketVersion(
		ctx context.Context,
		requirementID string,
		ticketVersionID string,
	) ([]domain.TaskRun, error)

	// ListInFlightTasks 返回已经被领取、但尚未进入终态的 TaskRun。
	// queued 仍可由新进程正常领取，不属于恢复对象；结果按稳定入队顺序排列。
	ListInFlightTasks(ctx context.Context) ([]domain.TaskRun, error)

	// ClaimNextTask 原子领取最早入队的 queued TaskRun。
	//
	// “查找候选、调用 TaskRun.Claim、写回结果”必须处于同一个写锁临界区。
	// now 由调用方传入，使行为可测试，也避免 Adapter 偷偷依赖系统时钟。
	// 没有可领取任务时返回 ErrNotFound。
	ClaimNextTask(ctx context.Context, now time.Time) (domain.TaskRun, error)

	// SaveTask 在当前状态仍等于 expectedStatus 时更新 TaskRun。
	//
	// “比较当前状态、写入新值”必须是一个原子操作。资源不存在返回 ErrNotFound；状态已经
	// 被其它请求改变返回 ErrTaskConflict，且不能覆盖较新的终态。它不允许借此创建新任务。
	SaveTask(ctx context.Context, run domain.TaskRun, expectedStatus domain.TaskStatus) error

	// AppendTaskEvents 原子追加同一 TaskRun 的一批连续事件。
	//
	// 第一条事件的 Sequence 必须是 1，后续必须严格递增；批次中任意事件无效时，
	// 整批都不能写入。返回值是本批最后一条事件的全局 ID。
	AppendTaskEvents(ctx context.Context, events []domain.TaskEvent) (int64, error)

	// ListTaskEvents 返回 sequence > afterSequence 的事件深拷贝，并按 Sequence 升序排列。
	ListTaskEvents(
		ctx context.Context,
		taskRunID string,
		afterSequence int64,
	) ([]domain.TaskEvent, error)

	// TaskEventIDBounds 返回当前仍可读取的最小、最大全局事件 ID。
	//
	// 空表返回 (0, 0, nil)。SSE 用它判断浏览器带来的 Last-Event-ID 是否已经落在
	// 可重放窗口之外；Sequence 是单个 TaskRun 的局部编号，不能用于这个判断。
	TaskEventIDBounds(ctx context.Context) (oldestID int64, latestID int64, err error)

	// ListTaskEventsAfterID 按全局 ID 读取一页事件，供 SSE 断线重连时重放。
	//
	// 返回 id > afterID 的前 limit 条事件，并严格按 ID 升序排列。afterID 必须非负，
	// limit 必须大于零。
	ListTaskEventsAfterID(
		ctx context.Context,
		afterID int64,
		limit int,
	) ([]domain.TaskEvent, error)
}

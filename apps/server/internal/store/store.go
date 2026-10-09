package store

import (
	"context"
	"time"

	"product-agent-workbench/internal/domain"
)

// Store 描述工作流真正需要的持久化能力。
//
// 先在 internal/store/memory 中实现它并通过契约测试，再创建 SQLite Adapter。不要为了
// “以后可能替换数据库”继续拆出 RequirementRepository、TaskRepository 等浅接口。
type Store interface {
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

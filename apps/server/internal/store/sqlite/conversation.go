package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"product-agent-workbench/internal/domain"
	storecontract "product-agent-workbench/internal/store"
)

const conversationColumns = `
	id, requirement_id, phase, status, planning_thread_id, implementation_thread_id,
	pending_understanding_revision, turn_ready_for_confirmation, error, revision,
	created_at, updated_at`

const conversationTurnColumns = `
	id, conversation_id, sequence, phase, status, codex_thread_id,
	started_at, finished_at, failure_reason`

const conversationMessageColumns = `
	id, conversation_id, turn_id, sequence, role, phase, status, segments_json, recorded_at`

// CreateRequirementConversation 在一个 SQLite 事务中建立新需求的最小页面快照。
func (s *Store) CreateRequirementConversation(
	ctx context.Context,
	input storecontract.CreateRequirementConversationInput,
) error {
	if err := input.Validate(); err != nil {
		return err
	}
	segmentsJSON, err := json.Marshal(input.InitialMessage.Segments)
	if err != nil {
		return fmt.Errorf("编码初始对话消息: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始创建 Requirement 对话: %w", err)
	}
	defer tx.Rollback()

	requirement := input.Requirement
	result, err := tx.ExecContext(ctx, `
		INSERT INTO requirements (id, project_id, title, stage, revision)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		requirement.ID,
		requirement.ProjectID,
		requirement.Title,
		requirement.Stage,
		requirement.Revision,
	)
	if err != nil {
		return fmt.Errorf("创建 Requirement 对话头: %w", err)
	}
	if err := requireOneInsertedRow(result, "读取 Requirement 对话头创建结果"); err != nil {
		return err
	}
	if err := insertConversation(ctx, tx, input.Conversation); err != nil {
		return err
	}
	if err := insertConversationMessage(ctx, tx, input.InitialMessage, segmentsJSON); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交 Requirement 对话: %w", err)
	}
	return nil
}

func (s *Store) CreateConversation(ctx context.Context, conversation domain.Conversation) error {
	if err := domain.ValidateConversation(conversation); err != nil {
		return err
	}
	exists, err := rowExists(ctx, s.db, "requirements", conversation.RequirementID)
	if err != nil {
		return fmt.Errorf("检查对话所属 Requirement: %w", err)
	}
	if !exists {
		return storecontract.ErrNotFound
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO conversations (
			id, requirement_id, phase, status, planning_thread_id, implementation_thread_id,
			pending_understanding_revision, turn_ready_for_confirmation, error, revision,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		conversation.ID,
		conversation.RequirementID,
		conversation.Phase,
		conversation.Status,
		nullableString(conversation.PlanningThreadID),
		nullableString(conversation.ImplementationThreadID),
		nullableString(conversation.PendingUnderstandingRevision),
		conversation.TurnReadyForConfirmation,
		nullableString(conversation.Error),
		conversation.Revision,
		conversation.CreatedAt.UTC().UnixNano(),
		conversation.UpdatedAt.UTC().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("创建对话: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取对话创建结果: %w", err)
	}
	if affected == 0 {
		return storecontract.ErrAlreadyExists
	}
	return nil
}

func (s *Store) GetConversationByRequirement(
	ctx context.Context,
	requirementID string,
) (domain.Conversation, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+conversationColumns+`
		FROM conversations
		WHERE requirement_id = ?`, requirementID)
	conversation, err := scanConversation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Conversation{}, storecontract.ErrNotFound
	}
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("读取 Requirement 对话: %w", err)
	}
	return conversation, nil
}

func (s *Store) SaveConversation(
	ctx context.Context,
	conversation domain.Conversation,
	expectedRevision int64,
) (int64, error) {
	if err := domain.ValidateConversation(conversation); err != nil {
		return 0, err
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE conversations
		SET phase = ?, status = ?, planning_thread_id = ?, implementation_thread_id = ?,
		    pending_understanding_revision = ?, turn_ready_for_confirmation = ?, error = ?,
		    revision = ?, updated_at = ?
		WHERE id = ? AND requirement_id = ? AND revision = ? AND created_at = ?`,
		conversation.Phase,
		conversation.Status,
		nullableString(conversation.PlanningThreadID),
		nullableString(conversation.ImplementationThreadID),
		nullableString(conversation.PendingUnderstandingRevision),
		conversation.TurnReadyForConfirmation,
		nullableString(conversation.Error),
		expectedRevision+1,
		conversation.UpdatedAt.UTC().UnixNano(),
		conversation.ID,
		conversation.RequirementID,
		expectedRevision,
		conversation.CreatedAt.UTC().UnixNano(),
	)
	if err != nil {
		return 0, fmt.Errorf("更新对话: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("读取对话更新结果: %w", err)
	}
	if affected == 1 {
		return expectedRevision + 1, nil
	}
	exists, err := rowExists(ctx, s.db, "conversations", conversation.ID)
	if err != nil {
		return 0, fmt.Errorf("检查对话更新冲突: %w", err)
	}
	if !exists {
		return 0, storecontract.ErrNotFound
	}
	return 0, storecontract.ErrConversationConflict
}

// BeginConversationTurn 把 23A 的状态迁移和页面消息骨架放进同一个写事务。
func (s *Store) BeginConversationTurn(
	ctx context.Context,
	input storecontract.BeginConversationTurnInput,
) error {
	if err := input.Validate(); err != nil {
		return err
	}
	userSegmentsJSON, err := json.Marshal(input.UserMessage.Segments)
	if err != nil {
		return fmt.Errorf("编码用户消息: %w", err)
	}
	assistantSegmentsJSON, err := json.Marshal(input.AssistantMessage.Segments)
	if err != nil {
		return fmt.Errorf("编码 assistant 消息: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始 Conversation Turn: %w", err)
	}
	defer tx.Rollback()

	current, err := scanConversation(tx.QueryRowContext(ctx, `
		SELECT `+conversationColumns+`
		FROM conversations
		WHERE id = ?`, input.Conversation.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return storecontract.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("读取 Turn 当前对话: %w", err)
	}
	if current.Revision != input.ExpectedRevision {
		return storecontract.ErrConversationConflict
	}
	expectedConversation, expectedTurn, err := current.BeginTurn(
		domain.BeginConversationTurnInput{
			ID:       input.Turn.ID,
			Sequence: input.Turn.Sequence,
		},
		input.Turn.StartedAt,
	)
	if err != nil {
		return err
	}
	if expectedConversation != input.Conversation || expectedTurn != input.Turn {
		return domain.ErrInvalidInput
	}

	var nextTurnSequence int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(sequence), 0) + 1
		FROM conversation_turns
		WHERE conversation_id = ?`, current.ID).Scan(&nextTurnSequence); err != nil {
		return fmt.Errorf("读取下一个 Turn 序号: %w", err)
	}
	if input.Turn.Sequence != nextTurnSequence {
		return storecontract.ErrInvalidTurnSequence
	}
	var nextMessageSequence int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(sequence), 0) + 1
		FROM conversation_messages
		WHERE conversation_id = ?`, current.ID).Scan(&nextMessageSequence); err != nil {
		return fmt.Errorf("读取下一个消息序号: %w", err)
	}
	if input.UserMessage.Sequence != nextMessageSequence {
		return storecontract.ErrInvalidMessageSequence
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE conversations
		SET phase = ?, status = ?, planning_thread_id = ?, implementation_thread_id = ?,
		    pending_understanding_revision = ?, turn_ready_for_confirmation = ?, error = ?,
		    revision = ?, updated_at = ?
		WHERE id = ? AND requirement_id = ? AND revision = ? AND created_at = ?`,
		input.Conversation.Phase,
		input.Conversation.Status,
		nullableString(input.Conversation.PlanningThreadID),
		nullableString(input.Conversation.ImplementationThreadID),
		nullableString(input.Conversation.PendingUnderstandingRevision),
		input.Conversation.TurnReadyForConfirmation,
		nullableString(input.Conversation.Error),
		input.Conversation.Revision,
		input.Conversation.UpdatedAt.UTC().UnixNano(),
		input.Conversation.ID,
		input.Conversation.RequirementID,
		input.ExpectedRevision,
		input.Conversation.CreatedAt.UTC().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("更新 Turn 对话头: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取 Turn 对话更新结果: %w", err)
	}
	if affected != 1 {
		return storecontract.ErrConversationConflict
	}
	if err := insertConversationTurn(ctx, tx, input.Turn); err != nil {
		return err
	}
	if err := insertConversationMessage(ctx, tx, input.UserMessage, userSegmentsJSON); err != nil {
		return err
	}
	if err := insertConversationMessage(
		ctx,
		tx,
		input.AssistantMessage,
		assistantSegmentsJSON,
	); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交 Conversation Turn: %w", err)
	}
	return nil
}

// RetryConversationTurn 在一个事务里开始重试；历史 user 消息保留，但不会被重复插入。
func (s *Store) RetryConversationTurn(
	ctx context.Context,
	input storecontract.RetryConversationTurnInput,
) error {
	// 第 1 步：先验证调用方组装的三份值，再编码唯一需要插入的 assistant 消息。
	if err := input.Validate(); err != nil {
		return err
	}
	segmentsJSON, err := json.Marshal(input.AssistantMessage.Segments)
	if err != nil {
		return fmt.Errorf("编码重试 assistant 消息: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始重试 Conversation Turn: %w", err)
	}
	defer tx.Rollback()

	// 第 2 步：事务内读取当前对话，并用领域方法重放 BeginTurn。
	current, err := scanConversation(tx.QueryRowContext(ctx, `
		SELECT `+conversationColumns+` FROM conversations WHERE id = ?`, input.Conversation.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return storecontract.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("读取重试当前对话: %w", err)
	}
	if current.Revision != input.ExpectedRevision {
		return storecontract.ErrConversationConflict
	}
	expectedConversation, expectedTurn, err := current.BeginTurn(domain.BeginConversationTurnInput{
		ID: input.Turn.ID, Sequence: input.Turn.Sequence,
	}, input.Turn.StartedAt)
	if err != nil {
		return err
	}
	if expectedConversation != input.Conversation || expectedTurn != input.Turn {
		return domain.ErrInvalidInput
	}

	// 第 3 步：从数据库尾部计算 sequence，重试只占用一个新的消息序号。
	var nextTurnSequence int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(sequence), 0) + 1 FROM conversation_turns
		WHERE conversation_id = ?`, current.ID).Scan(&nextTurnSequence); err != nil {
		return fmt.Errorf("读取重试 Turn 序号: %w", err)
	}
	if input.Turn.Sequence != nextTurnSequence {
		return storecontract.ErrInvalidTurnSequence
	}
	var nextMessageSequence int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(sequence), 0) + 1 FROM conversation_messages
		WHERE conversation_id = ?`, current.ID).Scan(&nextMessageSequence); err != nil {
		return fmt.Errorf("读取重试消息序号: %w", err)
	}
	if input.AssistantMessage.Sequence != nextMessageSequence {
		return storecontract.ErrInvalidMessageSequence
	}

	// 第 4 步：CAS 更新对话头，再插入新 Turn 和 assistant；任何一步失败都会整体回滚。
	result, err := tx.ExecContext(ctx, `
		UPDATE conversations
		SET phase = ?, status = ?, planning_thread_id = ?, implementation_thread_id = ?,
		    pending_understanding_revision = ?, turn_ready_for_confirmation = ?, error = ?,
		    revision = ?, updated_at = ?
		WHERE id = ? AND requirement_id = ? AND revision = ? AND created_at = ?`,
		input.Conversation.Phase, input.Conversation.Status,
		nullableString(input.Conversation.PlanningThreadID),
		nullableString(input.Conversation.ImplementationThreadID),
		nullableString(input.Conversation.PendingUnderstandingRevision),
		input.Conversation.TurnReadyForConfirmation,
		nullableString(input.Conversation.Error), input.Conversation.Revision,
		input.Conversation.UpdatedAt.UTC().UnixNano(), input.Conversation.ID,
		input.Conversation.RequirementID, input.ExpectedRevision,
		input.Conversation.CreatedAt.UTC().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("更新重试对话头: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取重试对话更新结果: %w", err)
	}
	if affected != 1 {
		return storecontract.ErrConversationConflict
	}
	if err := insertConversationTurn(ctx, tx, input.Turn); err != nil {
		return err
	}
	if err := insertConversationMessage(ctx, tx, input.AssistantMessage, segmentsJSON); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交重试 Conversation Turn: %w", err)
	}
	return nil
}

// AttachConversationThread 在 turn/start 之前原子保存可续接的 Codex Thread ID。
func (s *Store) AttachConversationThread(
	ctx context.Context,
	input storecontract.AttachConversationThreadInput,
) error {
	// 第 1 步：开启事务，并在同一数据库快照读取 Conversation 与当前 Turn。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始绑定 Conversation Thread: %w", err)
	}
	defer tx.Rollback()
	current, err := scanConversation(tx.QueryRowContext(ctx, `
		SELECT `+conversationColumns+` FROM conversations WHERE id = ?`, input.Conversation.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return storecontract.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("读取 Thread Conversation: %w", err)
	}
	currentTurn, err := scanConversationTurn(tx.QueryRowContext(ctx, `
		SELECT `+conversationTurnColumns+` FROM conversation_turns WHERE id = ?`, input.Turn.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return storecontract.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("读取 Thread Conversation Turn: %w", err)
	}

	// 第 2 步：重放领域迁移，确认新的 Thread ID 与当前运行态确实连续。
	if err := input.ValidateTransition(current, currentTurn); err != nil {
		return err
	}

	// 第 3 步：使用 revision CAS 更新对话头。
	result, err := tx.ExecContext(ctx, `
		UPDATE conversations
		SET planning_thread_id = ?, implementation_thread_id = ?, revision = ?, updated_at = ?
		WHERE id = ? AND requirement_id = ? AND revision = ? AND created_at = ?`,
		nullableString(input.Conversation.PlanningThreadID),
		nullableString(input.Conversation.ImplementationThreadID),
		input.Conversation.Revision,
		input.Conversation.UpdatedAt.UTC().UnixNano(),
		input.Conversation.ID,
		input.Conversation.RequirementID,
		input.ExpectedRevision,
		input.Conversation.CreatedAt.UTC().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("绑定 Conversation Thread: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("读取 Conversation Thread 更新结果: %w", err)
	} else if affected != 1 {
		return storecontract.ErrConversationConflict
	}

	// 第 4 步：使用 status CAS 更新当前 Turn；若失败，对话头更新也会回滚。
	result, err = tx.ExecContext(ctx, `
		UPDATE conversation_turns
		SET codex_thread_id = ?
		WHERE id = ? AND conversation_id = ? AND sequence = ? AND phase = ?
		  AND started_at = ? AND status = ?`,
		nullableString(input.Turn.CodexThreadID),
		input.Turn.ID,
		input.Turn.ConversationID,
		input.Turn.Sequence,
		input.Turn.Phase,
		input.Turn.StartedAt.UTC().UnixNano(),
		input.ExpectedTurnStatus,
	)
	if err != nil {
		return fmt.Errorf("绑定 Turn Thread: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("读取 Turn Thread 更新结果: %w", err)
	} else if affected != 1 {
		return storecontract.ErrConversationTurnConflict
	}

	// 第 5 步：两个 UPDATE 都成功后提交，之后才允许 Codex turn/start。
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交 Conversation Thread: %w", err)
	}
	return nil
}

// FinishConversationTurn 在一个 SQLite 事务中结束对话头、Turn 和 assistant 消息。
func (s *Store) FinishConversationTurn(
	ctx context.Context,
	input storecontract.FinishConversationTurnInput,
) error {
	// 第 1 步：先校验调用方提交的三个终态之间是否自洽，并准备消息 JSON。
	if err := input.Validate(); err != nil {
		return err
	}
	segmentsJSON, err := json.Marshal(input.AssistantMessage.Segments)
	if err != nil {
		return fmt.Errorf("编码终态 assistant 消息: %w", err)
	}

	// 第 2 步：开启事务。后面的任一检查或 UPDATE 失败，defer Rollback 都会撤销前序写入。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始结束 Conversation Turn: %w", err)
	}
	defer tx.Rollback()

	// 第 3 步：在同一事务快照里读取当前 Conversation、Turn 和 assistant 消息。
	current, err := scanConversation(tx.QueryRowContext(ctx, `
		SELECT `+conversationColumns+`
		FROM conversations
		WHERE id = ?`, input.Conversation.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return storecontract.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("读取终态 Conversation: %w", err)
	}
	currentTurn, err := scanConversationTurn(tx.QueryRowContext(ctx, `
		SELECT `+conversationTurnColumns+`
		FROM conversation_turns
		WHERE id = ?`, input.Turn.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return storecontract.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("读取终态 Conversation Turn: %w", err)
	}
	currentMessage, err := scanConversationMessage(tx.QueryRowContext(ctx, `
		SELECT `+conversationMessageColumns+`
		FROM conversation_messages
		WHERE id = ?`, input.AssistantMessage.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return storecontract.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("读取终态 assistant 消息: %w", err)
	}

	// 第 4 步：用数据库当前值重放 23A 状态机，并执行三种 CAS 前置检查。
	if err := input.ValidateTransition(current, currentTurn); err != nil {
		return err
	}
	if err := input.ValidateCurrentAssistant(currentMessage); err != nil {
		return err
	}
	message := input.AssistantMessage

	// 第 5 步：CAS 更新 Conversation。revision 保证旧浏览器或重复完成请求不能覆盖新状态。
	result, err := tx.ExecContext(ctx, `
		UPDATE conversations
		SET phase = ?, status = ?, planning_thread_id = ?, implementation_thread_id = ?,
		    pending_understanding_revision = ?, turn_ready_for_confirmation = ?, error = ?,
		    revision = ?, updated_at = ?
		WHERE id = ? AND requirement_id = ? AND revision = ? AND created_at = ?`,
		input.Conversation.Phase,
		input.Conversation.Status,
		nullableString(input.Conversation.PlanningThreadID),
		nullableString(input.Conversation.ImplementationThreadID),
		nullableString(input.Conversation.PendingUnderstandingRevision),
		input.Conversation.TurnReadyForConfirmation,
		nullableString(input.Conversation.Error),
		input.Conversation.Revision,
		input.Conversation.UpdatedAt.UTC().UnixNano(),
		input.Conversation.ID,
		input.Conversation.RequirementID,
		input.ExpectedRevision,
		input.Conversation.CreatedAt.UTC().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("结束 Turn 时更新 Conversation: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("读取终态 Conversation 更新结果: %w", err)
	} else if affected != 1 {
		return storecontract.ErrConversationConflict
	}

	// 第 6 步：CAS 更新 Turn。status 仍为 running 才能结束，避免完成覆盖已经发生的取消。
	result, err = tx.ExecContext(ctx, `
		UPDATE conversation_turns
		SET status = ?, codex_thread_id = ?, finished_at = ?, failure_reason = ?
		WHERE id = ? AND conversation_id = ? AND sequence = ? AND phase = ?
		  AND started_at = ? AND status = ?`,
		input.Turn.Status,
		nullableString(input.Turn.CodexThreadID),
		nullableUnixNano(input.Turn.FinishedAt),
		nullableString(input.Turn.FailureReason),
		input.Turn.ID,
		input.Turn.ConversationID,
		input.Turn.Sequence,
		input.Turn.Phase,
		input.Turn.StartedAt.UTC().UnixNano(),
		input.ExpectedTurnStatus,
	)
	if err != nil {
		return fmt.Errorf("结束 Conversation Turn: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("读取终态 Turn 更新结果: %w", err)
	} else if affected != 1 {
		return storecontract.ErrConversationTurnConflict
	}

	// 第 7 步：最后更新 assistant 消息；若这里失败，前两次 UPDATE 也会随事务回滚。
	result, err = tx.ExecContext(ctx, `
		UPDATE conversation_messages
		SET status = ?, segments_json = ?
		WHERE id = ? AND conversation_id = ? AND turn_id = ? AND sequence = ?
		  AND role = ? AND phase = ? AND recorded_at = ? AND status = ?`,
		message.Status,
		segmentsJSON,
		message.ID,
		message.ConversationID,
		message.TurnID,
		message.Sequence,
		message.Role,
		message.Phase,
		message.RecordedAt.UTC().UnixNano(),
		input.ExpectedAssistantStatus,
	)
	if err != nil {
		return fmt.Errorf("结束 streaming assistant 消息: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("读取终态 assistant 更新结果: %w", err)
	} else if affected != 1 {
		return storecontract.ErrConversationMessageConflict
	}

	// 第 8 步：只有三个 UPDATE 都成功才提交；Commit 返回后，读者才能看见完整终态。
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交 Conversation Turn 终态: %w", err)
	}
	return nil
}

func (s *Store) CreateConversationTurn(ctx context.Context, turn domain.ConversationTurn) error {
	if err := domain.ValidateConversationTurn(turn); err != nil {
		return err
	}
	if turn.Status != domain.ConversationTurnRunning {
		return domain.ErrInvalidTransition
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始创建对话 Turn: %w", err)
	}
	defer tx.Rollback()
	exists, err := rowExists(ctx, tx, "conversations", turn.ConversationID)
	if err != nil {
		return fmt.Errorf("检查 Turn 所属对话: %w", err)
	}
	if !exists {
		return storecontract.ErrNotFound
	}
	var nextSequence int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(sequence), 0) + 1
		FROM conversation_turns
		WHERE conversation_id = ?`, turn.ConversationID).Scan(&nextSequence); err != nil {
		return fmt.Errorf("读取下一个 Turn 序号: %w", err)
	}
	if turn.Sequence != nextSequence {
		return storecontract.ErrInvalidTurnSequence
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO conversation_turns (
			id, conversation_id, sequence, phase, status, codex_thread_id,
			started_at, finished_at, failure_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		turn.ID,
		turn.ConversationID,
		turn.Sequence,
		turn.Phase,
		turn.Status,
		nullableString(turn.CodexThreadID),
		turn.StartedAt.UTC().UnixNano(),
		nullableUnixNano(turn.FinishedAt),
		nullableString(turn.FailureReason),
	)
	if err != nil {
		return fmt.Errorf("创建对话 Turn: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取 Turn 创建结果: %w", err)
	}
	if affected == 0 {
		return storecontract.ErrAlreadyExists
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交对话 Turn: %w", err)
	}
	return nil
}

func (s *Store) GetConversationTurn(ctx context.Context, id string) (domain.ConversationTurn, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+conversationTurnColumns+`
		FROM conversation_turns
		WHERE id = ?`, id)
	turn, err := scanConversationTurn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ConversationTurn{}, storecontract.ErrNotFound
	}
	if err != nil {
		return domain.ConversationTurn{}, fmt.Errorf("读取对话 Turn: %w", err)
	}
	return turn, nil
}

func (s *Store) ListConversationTurns(
	ctx context.Context,
	conversationID string,
) ([]domain.ConversationTurn, error) {
	exists, err := rowExists(ctx, s.db, "conversations", conversationID)
	if err != nil {
		return nil, fmt.Errorf("检查对话: %w", err)
	}
	if !exists {
		return nil, storecontract.ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+conversationTurnColumns+`
		FROM conversation_turns
		WHERE conversation_id = ?
		ORDER BY sequence ASC`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("查询对话 Turn: %w", err)
	}
	defer rows.Close()
	turns := make([]domain.ConversationTurn, 0)
	for rows.Next() {
		turn, err := scanConversationTurn(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描对话 Turn: %w", err)
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历对话 Turn: %w", err)
	}
	return turns, nil
}

func (s *Store) SaveConversationTurn(
	ctx context.Context,
	turn domain.ConversationTurn,
	expectedStatus domain.ConversationTurnStatus,
) error {
	if err := domain.ValidateConversationTurn(turn); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE conversation_turns
		SET status = ?, codex_thread_id = ?, finished_at = ?, failure_reason = ?
		WHERE id = ? AND conversation_id = ? AND sequence = ? AND phase = ?
		  AND started_at = ? AND status = ?`,
		turn.Status,
		nullableString(turn.CodexThreadID),
		nullableUnixNano(turn.FinishedAt),
		nullableString(turn.FailureReason),
		turn.ID,
		turn.ConversationID,
		turn.Sequence,
		turn.Phase,
		turn.StartedAt.UTC().UnixNano(),
		expectedStatus,
	)
	if err != nil {
		return fmt.Errorf("更新对话 Turn: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取 Turn 更新结果: %w", err)
	}
	if affected == 1 {
		return nil
	}
	exists, err := rowExists(ctx, s.db, "conversation_turns", turn.ID)
	if err != nil {
		return fmt.Errorf("检查 Turn 更新冲突: %w", err)
	}
	if !exists {
		return storecontract.ErrNotFound
	}
	return storecontract.ErrConversationTurnConflict
}

func (s *Store) AppendConversationMessages(
	ctx context.Context,
	messages []domain.ConversationMessage,
) error {
	if len(messages) == 0 {
		return domain.ErrInvalidInput
	}
	for _, message := range messages {
		if err := domain.ValidateConversationMessage(message); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始追加对话消息: %w", err)
	}
	defer tx.Rollback()
	conversationID := messages[0].ConversationID
	exists, err := rowExists(ctx, tx, "conversations", conversationID)
	if err != nil {
		return fmt.Errorf("检查消息所属对话: %w", err)
	}
	if !exists {
		return storecontract.ErrNotFound
	}
	var nextSequence int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(sequence), 0) + 1
		FROM conversation_messages
		WHERE conversation_id = ?`, conversationID).Scan(&nextSequence); err != nil {
		return fmt.Errorf("读取下一个消息序号: %w", err)
	}
	seen := make(map[string]struct{}, len(messages))
	for index, message := range messages {
		if message.ConversationID != conversationID || message.Sequence != nextSequence+int64(index) {
			return storecontract.ErrInvalidMessageSequence
		}
		if _, duplicate := seen[message.ID]; duplicate {
			return storecontract.ErrAlreadyExists
		}
		seen[message.ID] = struct{}{}
		if message.TurnID != "" {
			belongs, err := turnBelongsToConversation(ctx, tx, message.TurnID, conversationID)
			if err != nil {
				return fmt.Errorf("检查消息所属 Turn: %w", err)
			}
			if !belongs {
				return storecontract.ErrNotFound
			}
		}
	}
	for _, message := range messages {
		segments := message.Segments
		if segments == nil {
			segments = []domain.ConversationSegment{}
		}
		segmentsJSON, err := json.Marshal(segments)
		if err != nil {
			return fmt.Errorf("编码对话消息片段: %w", err)
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO conversation_messages (
				id, conversation_id, turn_id, sequence, role, phase, status,
				segments_json, recorded_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT DO NOTHING`,
			message.ID,
			message.ConversationID,
			nullableString(message.TurnID),
			message.Sequence,
			message.Role,
			message.Phase,
			message.Status,
			segmentsJSON,
			message.RecordedAt.UTC().UnixNano(),
		)
		if err != nil {
			return fmt.Errorf("追加对话消息: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("读取消息追加结果: %w", err)
		}
		if affected == 0 {
			return storecontract.ErrAlreadyExists
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交对话消息: %w", err)
	}
	return nil
}

func (s *Store) SaveConversationMessage(
	ctx context.Context,
	message domain.ConversationMessage,
	expectedStatus domain.ConversationMessageStatus,
) error {
	if err := domain.ValidateConversationMessage(message); err != nil {
		return err
	}
	segments := message.Segments
	if segments == nil {
		segments = []domain.ConversationSegment{}
	}
	segmentsJSON, err := json.Marshal(segments)
	if err != nil {
		return fmt.Errorf("编码对话消息片段: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE conversation_messages
		SET status = ?, segments_json = ?
		WHERE id = ? AND conversation_id = ? AND sequence = ? AND role = ?
		  AND phase = ? AND recorded_at = ?
		  AND ((turn_id IS NULL AND ? IS NULL) OR turn_id = ?)
		  AND status = ?`,
		message.Status,
		segmentsJSON,
		message.ID,
		message.ConversationID,
		message.Sequence,
		message.Role,
		message.Phase,
		message.RecordedAt.UTC().UnixNano(),
		nullableString(message.TurnID),
		nullableString(message.TurnID),
		expectedStatus,
	)
	if err != nil {
		return fmt.Errorf("更新对话消息: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取消息更新结果: %w", err)
	}
	if affected == 1 {
		return nil
	}
	exists, err := rowExists(ctx, s.db, "conversation_messages", message.ID)
	if err != nil {
		return fmt.Errorf("检查消息更新冲突: %w", err)
	}
	if !exists {
		return storecontract.ErrNotFound
	}
	return storecontract.ErrConversationMessageConflict
}

func (s *Store) ListConversationMessages(
	ctx context.Context,
	conversationID string,
	afterSequence int64,
) ([]domain.ConversationMessage, error) {
	if afterSequence < 0 {
		return nil, domain.ErrInvalidInput
	}
	exists, err := rowExists(ctx, s.db, "conversations", conversationID)
	if err != nil {
		return nil, fmt.Errorf("检查对话: %w", err)
	}
	if !exists {
		return nil, storecontract.ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+conversationMessageColumns+`
		FROM conversation_messages
		WHERE conversation_id = ? AND sequence > ?
		ORDER BY sequence ASC`, conversationID, afterSequence)
	if err != nil {
		return nil, fmt.Errorf("查询对话消息: %w", err)
	}
	defer rows.Close()
	messages := make([]domain.ConversationMessage, 0)
	for rows.Next() {
		message, err := scanConversationMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描对话消息: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历对话消息: %w", err)
	}
	return messages, nil
}

func scanConversation(scanner rowScanner) (domain.Conversation, error) {
	var value domain.Conversation
	var planningThreadID, implementationThreadID, pendingRevision, conversationError sql.NullString
	var createdAt, updatedAt int64
	if err := scanner.Scan(
		&value.ID,
		&value.RequirementID,
		&value.Phase,
		&value.Status,
		&planningThreadID,
		&implementationThreadID,
		&pendingRevision,
		&value.TurnReadyForConfirmation,
		&conversationError,
		&value.Revision,
		&createdAt,
		&updatedAt,
	); err != nil {
		return domain.Conversation{}, err
	}
	value.PlanningThreadID = planningThreadID.String
	value.ImplementationThreadID = implementationThreadID.String
	value.PendingUnderstandingRevision = pendingRevision.String
	value.Error = conversationError.String
	value.CreatedAt = time.Unix(0, createdAt).UTC()
	value.UpdatedAt = time.Unix(0, updatedAt).UTC()
	if err := domain.ValidateConversation(value); err != nil {
		return domain.Conversation{}, fmt.Errorf("数据库中的 Conversation 无效: %w", err)
	}
	return value, nil
}

func scanConversationTurn(scanner rowScanner) (domain.ConversationTurn, error) {
	var value domain.ConversationTurn
	var codexThreadID, failureReason sql.NullString
	var startedAt int64
	var finishedAt sql.NullInt64
	if err := scanner.Scan(
		&value.ID,
		&value.ConversationID,
		&value.Sequence,
		&value.Phase,
		&value.Status,
		&codexThreadID,
		&startedAt,
		&finishedAt,
		&failureReason,
	); err != nil {
		return domain.ConversationTurn{}, err
	}
	value.CodexThreadID = codexThreadID.String
	value.FailureReason = failureReason.String
	value.StartedAt = time.Unix(0, startedAt).UTC()
	value.FinishedAt = timeFromNullInt64(finishedAt)
	if err := domain.ValidateConversationTurn(value); err != nil {
		return domain.ConversationTurn{}, fmt.Errorf("数据库中的 ConversationTurn 无效: %w", err)
	}
	return value, nil
}

func scanConversationMessage(scanner rowScanner) (domain.ConversationMessage, error) {
	var value domain.ConversationMessage
	var turnID sql.NullString
	var segmentsJSON []byte
	var recordedAt int64
	if err := scanner.Scan(
		&value.ID,
		&value.ConversationID,
		&turnID,
		&value.Sequence,
		&value.Role,
		&value.Phase,
		&value.Status,
		&segmentsJSON,
		&recordedAt,
	); err != nil {
		return domain.ConversationMessage{}, err
	}
	value.TurnID = turnID.String
	value.RecordedAt = time.Unix(0, recordedAt).UTC()
	value.Segments = []domain.ConversationSegment{}
	if err := json.Unmarshal(segmentsJSON, &value.Segments); err != nil {
		return domain.ConversationMessage{}, fmt.Errorf("解析消息片段: %w", err)
	}
	if err := domain.ValidateConversationMessage(value); err != nil {
		return domain.ConversationMessage{}, fmt.Errorf("数据库中的 ConversationMessage 无效: %w", err)
	}
	return value, nil
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertConversation(
	ctx context.Context,
	execer sqlExecer,
	conversation domain.Conversation,
) error {
	result, err := execer.ExecContext(ctx, `
		INSERT INTO conversations (
			id, requirement_id, phase, status, planning_thread_id, implementation_thread_id,
			pending_understanding_revision, turn_ready_for_confirmation, error, revision,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		conversation.ID,
		conversation.RequirementID,
		conversation.Phase,
		conversation.Status,
		nullableString(conversation.PlanningThreadID),
		nullableString(conversation.ImplementationThreadID),
		nullableString(conversation.PendingUnderstandingRevision),
		conversation.TurnReadyForConfirmation,
		nullableString(conversation.Error),
		conversation.Revision,
		conversation.CreatedAt.UTC().UnixNano(),
		conversation.UpdatedAt.UTC().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("创建对话: %w", err)
	}
	return requireOneInsertedRow(result, "读取对话创建结果")
}

func insertConversationTurn(
	ctx context.Context,
	execer sqlExecer,
	turn domain.ConversationTurn,
) error {
	result, err := execer.ExecContext(ctx, `
		INSERT INTO conversation_turns (
			id, conversation_id, sequence, phase, status, codex_thread_id,
			started_at, finished_at, failure_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		turn.ID,
		turn.ConversationID,
		turn.Sequence,
		turn.Phase,
		turn.Status,
		nullableString(turn.CodexThreadID),
		turn.StartedAt.UTC().UnixNano(),
		nullableUnixNano(turn.FinishedAt),
		nullableString(turn.FailureReason),
	)
	if err != nil {
		return fmt.Errorf("创建对话 Turn: %w", err)
	}
	return requireOneInsertedRow(result, "读取 Turn 创建结果")
}

func insertConversationMessage(
	ctx context.Context,
	execer sqlExecer,
	message domain.ConversationMessage,
	segmentsJSON []byte,
) error {
	result, err := execer.ExecContext(ctx, `
		INSERT INTO conversation_messages (
			id, conversation_id, turn_id, sequence, role, phase, status,
			segments_json, recorded_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		message.ID,
		message.ConversationID,
		nullableString(message.TurnID),
		message.Sequence,
		message.Role,
		message.Phase,
		message.Status,
		segmentsJSON,
		message.RecordedAt.UTC().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("创建对话消息: %w", err)
	}
	return requireOneInsertedRow(result, "读取对话消息创建结果")
}

func requireOneInsertedRow(result sql.Result, action string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	if affected != 1 {
		return storecontract.ErrAlreadyExists
	}
	return nil
}

func rowExists(ctx context.Context, queryer queryRower, table string, id string) (bool, error) {
	// SQL 参数只能替换值，不能替换表名。因此这里显式列出允许的固定查询，绝不能把浏览器
	// 输入拼进 SQL。前端类比：这类似只允许从 route map 选组件，而不是 eval 字符串。
	var query string
	switch table {
	case "requirements":
		query = "SELECT COUNT(*) FROM requirements WHERE id = ?"
	case "conversations":
		query = "SELECT COUNT(*) FROM conversations WHERE id = ?"
	case "conversation_turns":
		query = "SELECT COUNT(*) FROM conversation_turns WHERE id = ?"
	case "conversation_messages":
		query = "SELECT COUNT(*) FROM conversation_messages WHERE id = ?"
	default:
		return false, fmt.Errorf("不支持的表名 %q", table)
	}
	var count int
	if err := queryer.QueryRowContext(ctx, query, id).Scan(&count); err != nil {
		return false, err
	}
	return count == 1, nil
}

func turnBelongsToConversation(
	ctx context.Context,
	queryer queryRower,
	turnID string,
	conversationID string,
) (bool, error) {
	var count int
	if err := queryer.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM conversation_turns
		WHERE id = ? AND conversation_id = ?`, turnID, conversationID).Scan(&count); err != nil {
		return false, err
	}
	return count == 1, nil
}

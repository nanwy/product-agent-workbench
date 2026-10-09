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

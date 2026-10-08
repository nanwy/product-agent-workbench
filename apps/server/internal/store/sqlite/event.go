package sqlite

import (
	"context"
	"fmt"
	"strings"
	"time"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
)

// AppendTaskEvents 在单个事务中追加完整批次。
//
// 必须先验证 TaskRun、批内身份和连续 sequence，再写任何一行；失败批次不能部分落盘，
// 也不能消耗全局事件 ID。
//
// 先理解两个不同的编号：
//
//   - event.ID 是 task_events 的自增主键，所有 TaskRun 共用，用作以后 SSE 的全局游标；
//   - event.Sequence 只在同一个 TaskRun 内从 1 连续递增，用来检查是否丢事件或乱序。
//
// 建议按下面的顺序实现：
//  1. 先检查 ctx.Err()；取消的请求不能开始事务或写入事件；
//  2. 空批次不执行 INSERT，使用 QueryRowContext 查询当前全局最大 id，没有事件时返回 0；
//  3. s.db.BeginTx(ctx, nil) 开始事务，并立即 defer tx.Rollback() 覆盖所有错误出口；
//  4. 取 events[0].TaskRunID，通过 tx.QueryRowContext + SELECT EXISTS 确认 TaskRun 存在，
//     不存在返回 store.ErrNotFound；
//  5. 在同一事务内查询该 TaskRun 当前最大 sequence；没有历史时视为 0，因此下一条应为 1；
//  6. 在执行任何 INSERT 前遍历完整批次：TaskRunID 必须相同，Sequence 必须从“最大值+1”
//     连续递增，Type 不能空白，CreatedAt 不能为零值；失败返回稳定的领域/Store 错误；
//  7. 验证全部通过后，再逐条使用 tx.ExecContext 执行参数化 INSERT。不要插入 event.ID，
//     让 SQLite 的 AUTOINCREMENT 生成；CreatedAt 写成 UTC Unix 纳秒；
//  8. 每次 INSERT 返回的 sql.Result 可调用 LastInsertId()，保存最后一条事件的全局 ID；
//  9. 所有 INSERT 成功后 tx.Commit()，再返回最后的全局 ID。
//
// 为什么必须“先验证完整批次，再开始 INSERT”：虽然事务回滚可以撤销已写行，但先验证能让
// 错误语义更清晰，也避免把明显错误交给数据库执行。即使插入中途失败，defer Rollback 仍然
// 保证整批都不可见，AUTOINCREMENT 的已回滚编号也不会成为已提交的 SSE 游标。
func (s *Store) AppendTaskEvents(ctx context.Context, events []domain.TaskEvent) (int64, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if len(events) == 0 {
		var lastID int64
		err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM task_events`).Scan(&lastID)
		if err != nil {
			return 0, fmt.Errorf("读取 taskEvent 最大ID: %w", err)
		}
		return lastID, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开始事务， %w", err)
	}
	defer tx.Rollback()
	taskRunID := events[0].TaskRunID
	var exists bool

	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM task_runs WHERE id = ?)`, taskRunID).Scan(&exists)
	if err != nil {
		return 0, fmt.Errorf("检查taskrun 存在 %w", err)
	}
	if !exists {
		return 0, store.ErrNotFound
	}
	var maxSequence int64
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) FROM task_events WHERE task_run_id = ?`, taskRunID).Scan(&maxSequence)
	if err != nil {
		return 0, fmt.Errorf("最大sequence %w", err)
	}
	nextSequence := maxSequence + 1
	for i, event := range events {
		if event.TaskRunID != taskRunID {
			return 0, store.ErrInvalidEventSequence
		}
		// i 表示事件在本批次中的偏移量。例如数据库最后一条是 2，
		// 那么本批第 0、1 条应分别是 3、4。
		if event.Sequence != nextSequence+int64(i) {
			return 0, store.ErrInvalidEventSequence
		}
		if strings.TrimSpace(event.Type) == "" {
			return 0, domain.ErrInvalidInput
		}
		if event.CreatedAt.IsZero() {
			return 0, domain.ErrInvalidInput
		}
	}
	const insertQuery = `INSERT INTO task_events (task_run_id, sequence, type, payload, created_at) VALUES (?, ?, ?, ?, ?)`
	var lastID int64
	for _, event := range events {
		// SQLite 会把 nil []byte 当作 NULL；领域层允许“没有附加数据”的事件，
		// 因此统一保存为空 BLOB，避免违反 payload NOT NULL 约束。
		payload := event.Payload
		if payload == nil {
			payload = []byte{}
		}
		result, err := tx.ExecContext(ctx, insertQuery, event.TaskRunID, event.Sequence, event.Type, payload, event.CreatedAt.UTC().UnixNano())
		if err != nil {
			return 0, fmt.Errorf("写入 taskEvents %w", err)
		}
		lastID, err = result.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("获取最后插入ID %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交事务 %w", err)
	}
	return lastID, nil
}

// ListTaskEvents 返回 sequence > afterSequence 的事件，并按 sequence 升序排列。
//
// QueryContext 后立即 defer rows.Close()，循环后检查 rows.Err()；Payload 必须复制到新的
// byte slice，不能复用 driver 缓冲区。
//
// 这里第一次使用“多行查询”API，各对象的职责是：
//
//   - QueryContext：执行可能返回多行的 SELECT，得到 *sql.Rows；
//   - rows.Next()：把游标移动到下一行，有行时返回 true；
//   - rows.Scan(...)：按 SELECT 列顺序把当前行写入 Go 变量；
//   - rows.Close()：尽早归还数据库连接，所以 QueryContext 成功后立即 defer；
//   - rows.Err()：循环结束后检查遍历期间是否发生驱动、连接或 context 错误。
//
// 建议按下面的顺序实现：
//  1. 检查 ctx.Err()；
//  2. 先用 QueryRowContext + SELECT EXISTS 判断 TaskRun 是否存在。不能用“事件查询返回零行”
//     判断，因为“TaskRun 存在但还没有事件”也会返回零行；
//  3. 用 QueryContext 查询 id、task_run_id、sequence、type、payload、created_at，条件是
//     task_run_id = ? AND sequence > ?，并明确 ORDER BY sequence ASC；
//  4. QueryContext 成功后立刻 defer rows.Close()；创建非 nil 的空 []domain.TaskEvent；
//  5. for rows.Next() 中声明当前 event 和 createdAt int64，再按 SELECT 顺序 Scan；
//  6. 使用 time.Unix(0, createdAt).UTC() 还原时间，并复制 Payload 后 append 到结果切片；
//  7. 循环结束后检查 rows.Err()，不能把遍历中断误当成正常结束；
//  8. 返回事件切片。TaskRun 存在但没有新事件时，返回非 nil 空切片和 nil error。
//
// 所有动态值都通过 ? 参数传入；不要把 taskRunID 或 afterSequence 拼进 SQL 字符串。
func (s *Store) ListTaskEvents(
	ctx context.Context,
	taskRunID string,
	afterSequence int64,
) ([]domain.TaskEvent, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM task_runs WHERE id = ?)`, taskRunID).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("检查 TaskRun 是否存在 %w", err)
	}
	if !exists {
		return nil, store.ErrNotFound
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id, task_run_id, sequence, type, payload, created_at FROM task_events WHERE task_run_id = ? AND sequence > ? ORDER BY sequence ASC`, taskRunID, afterSequence)
	if err != nil {
		return nil, fmt.Errorf("查询 TaskEvent 列表 %w", err)
	}
	defer rows.Close()
	// 明确初始化为空切片，使“没有新事件”和“查询失败返回 nil”在调用方看来不同。
	events := make([]domain.TaskEvent, 0)
	for rows.Next() {
		var event domain.TaskEvent
		var createdAt int64
		if err := rows.Scan(&event.ID, &event.TaskRunID, &event.Sequence, &event.Type, &event.Payload, &createdAt); err != nil {
			return nil, fmt.Errorf("扫描 TaskEvent %w", err)
		}
		event.CreatedAt = time.Unix(0, createdAt).UTC()
		event.Payload = append([]byte(nil), event.Payload...)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 TaskEvent 列表 %w", err)
	}
	return events, nil
}

// TaskEventIDBounds 返回 SSE 当前可重放的全局 ID 范围。
//
// MIN/MAX 是聚合查询，只返回一行，所以使用 QueryRowContext；COALESCE 把空表产生的
// NULL 转成 0，调用方不必处理 sql.NullInt64。
func (s *Store) TaskEventIDBounds(ctx context.Context) (int64, int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	var oldestID, latestID int64
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT COALESCE(MIN(id), 0), COALESCE(MAX(id), 0) FROM task_events`,
	).Scan(&oldestID, &latestID); err != nil {
		return 0, 0, fmt.Errorf("读取 TaskEvent ID 范围: %w", err)
	}
	return oldestID, latestID, nil
}

// ListTaskEventsAfterID 按全局 ID 读取一页事件。
//
// 前端类比：afterID 就像分页请求的 cursor，limit 是 pageSize。这里不能按 Sequence
// 查询，因为不同 TaskRun 都会从 Sequence=1 开始，无法组成一条全局消息流。
func (s *Store) ListTaskEventsAfterID(
	ctx context.Context,
	afterID int64,
	limit int,
) ([]domain.TaskEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if afterID < 0 || limit < 1 {
		return nil, domain.ErrInvalidInput
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_run_id, sequence, type, payload, created_at
		FROM task_events
		WHERE id > ?
		ORDER BY id ASC
		LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("按全局 ID 查询 TaskEvent: %w", err)
	}
	defer rows.Close()

	events := make([]domain.TaskEvent, 0, limit)
	for rows.Next() {
		var event domain.TaskEvent
		var createdAt int64
		if err := rows.Scan(
			&event.ID,
			&event.TaskRunID,
			&event.Sequence,
			&event.Type,
			&event.Payload,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("扫描全局 TaskEvent: %w", err)
		}
		event.CreatedAt = time.Unix(0, createdAt).UTC()
		event.Payload = append([]byte(nil), event.Payload...)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历全局 TaskEvent: %w", err)
	}
	return events, nil
}

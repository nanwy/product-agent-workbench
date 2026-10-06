package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"product-agent-workbench/internal/domain"
	storecontract "product-agent-workbench/internal/store"
)

// EnqueueTask 保存 queued TaskRun 和稳定入队顺序。
//
// 如果还不知道 ExecContext/QueryRowContext/Scan 各自做什么，先阅读同目录
// README.md；当前方法只需要 ExecContext 和 RowsAffected。
//
// migration 必须提供持久化顺序事实；不能只按 CreatedAt 排序，因为两个任务可以同一时刻
// 创建。只接受 queued，重复 ID 映射 ErrAlreadyExists。
//
// 实现顺序：
//  1. 先检查 run.Status 是否为 queued，否则返回 domain.ErrInvalidTransition；
//  2. INSERT 时不传 enqueue_seq，让 SQLite 的 INTEGER PRIMARY KEY 生成稳定顺序；
//  3. CreatedAt 使用 run.CreatedAt.UTC().UnixNano()，未发生的时间和结果传 nil；
//  4. 使用 ON CONFLICT DO NOTHING + RowsAffected，把重复 ID、重复 attempt 或同一 Ticket
//     的第二条活动记录映射为 ErrAlreadyExists；
//  5. 外键和 CHECK 错误仍由 SQLite 返回，不能伪装成 ErrAlreadyExists。
func (s *Store) EnqueueTask(ctx context.Context, run domain.TaskRun) error {
	if run.Status != domain.TaskStatusQueued {
		return domain.ErrInvalidTransition
	}
	if strings.TrimSpace(run.TicketVersionID) == "" {
		return domain.ErrInvalidInput
	}
	const query = `
		INSERT INTO task_runs (
			id, requirement_id, ticket_version_id, ticket_id, attempt,
			status, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING
	`
	result, err := s.db.ExecContext(
		ctx,
		query,
		run.ID,
		run.RequirementID,
		run.TicketVersionID,
		run.TicketID,
		run.Attempt,
		string(run.Status),
		run.CreatedAt.UTC().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("TaskRun 入队: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取 TaskRun 入队结果: %w", err)
	}
	if rowsAffected == 0 {
		return storecontract.ErrAlreadyExists
	}
	return nil
}

// GetTask 查询并完整还原 TaskRun。
//
// 先阅读同目录 README.md 中的“QueryRowContext 和 Scan 分别做什么”。
// QueryRowContext 负责得到一行结果，Scan 负责把列值写入 Go 变量。
//
// 所有 nullable 时间和 Result 字段集中经 scan.go 处理，避免每条查询各自猜测 nil 语义。
//
// 本方法只负责 QueryRowContext：SELECT 列顺序要与 scanTaskRun 完全一致，
// sql.ErrNoRows 通过 errors.Is 映射为 store.ErrNotFound，其他错误使用 %w 包装。
func (s *Store) GetTask(ctx context.Context, id string) (domain.TaskRun, error) {
	const query = `
		SELECT
			id,
			requirement_id,
			ticket_version_id,
			ticket_id,
			attempt,
			status,
			created_at,
			claimed_at,
			started_at,
			finished_at,
			result_summary,
			codex_thread_id,
			worktree_path,
			result_revision,
			failure_reason
		FROM task_runs
		WHERE id = ?
	`
	row := s.db.QueryRowContext(ctx, query, id)
	run, err := scanTaskRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.TaskRun{}, storecontract.ErrNotFound
		}
		return domain.TaskRun{}, fmt.Errorf("查询 TaskRun: %w", err)
	}
	return run, nil
}

// ListTasksByTicketVersion 返回 Scheduler 计算 ready 集合所需的执行历史。
//
// 先区分“Requirement 不存在”和“这个版本还没有 TaskRun”：前者返回 ErrNotFound，
// 后者返回非 nil 空切片。结果按 enqueue_seq 排序，使同一 Ticket 后出现的记录就是较新的
// attempt；所有动态值仍通过参数传入。
func (s *Store) ListTasksByTicketVersion(
	ctx context.Context,
	requirementID string,
	ticketVersionID string,
) ([]domain.TaskRun, error) {
	var exists bool
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM requirements WHERE id = ?)`,
		requirementID,
	).Scan(&exists); err != nil {
		return nil, fmt.Errorf("检查 Requirement 是否存在: %w", err)
	}
	if !exists {
		return nil, storecontract.ErrNotFound
	}

	const query = `
		SELECT
			id,
			requirement_id,
			ticket_version_id,
			ticket_id,
			attempt,
			status,
			created_at,
			claimed_at,
			started_at,
			finished_at,
			result_summary,
			codex_thread_id,
			worktree_path,
			result_revision,
			failure_reason
		FROM task_runs
		WHERE requirement_id = ? AND ticket_version_id = ?
		ORDER BY enqueue_seq ASC
	`
	rows, err := s.db.QueryContext(ctx, query, requirementID, ticketVersionID)
	if err != nil {
		return nil, fmt.Errorf("查询 TicketVersion TaskRun: %w", err)
	}
	defer rows.Close()

	runs := make([]domain.TaskRun, 0)
	for rows.Next() {
		run, err := scanTaskRun(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描 TicketVersion TaskRun: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 TicketVersion TaskRun: %w", err)
	}
	return runs, nil
}

// ClaimNextTask 原子领取最早 queued TaskRun。
//
// 先阅读同目录 README.md 的“事务 API 与原子 Claim”；里面解释了 BeginTx、Rollback、
// Commit，以及为什么事务开始后不能调用使用 s.db 的 SaveTask。
//
// SQLite 没有 SELECT FOR UPDATE。本项目通过 `_txlock=immediate` 写事务完成下面的原子步骤：
//  1. s.db.BeginTx 开始事务，并立即 defer tx.Rollback 覆盖所有错误出口；
//  2. tx.QueryRowContext 按 enqueue_seq 选择最早 queued 记录，SELECT 列顺序复用 scanTaskRun；
//  3. sql.ErrNoRows 映射为 store.ErrNotFound，其余查询错误使用 %w 包装；
//  4. 调用 queued.Claim(now)，不要手写 claimed 状态或时间；
//  5. tx.ExecContext 用 `WHERE id = ? AND status = ?` 写回领域方法返回的新状态和 claimed_at；
//  6. RowsAffected 必须为 1，最后 Commit，成功后返回 claimed TaskRun。
//
// 当前测试会用两个 goroutine 和多个物理连接竞争同一任务，不能通过限制连接池为 1 绕过。
func (s *Store) ClaimNextTask(ctx context.Context, now time.Time) (domain.TaskRun, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TaskRun{}, fmt.Errorf("开始事务: %w", err)
	}
	defer tx.Rollback()
	const query = `
		SELECT
			id,
			requirement_id,
			ticket_version_id,
			ticket_id,
			attempt,
			status,
			created_at,
			claimed_at,
			started_at,
			finished_at,
			result_summary,
			codex_thread_id,
			worktree_path,
			result_revision,
			failure_reason
		FROM task_runs
		WHERE status = 'queued'
		ORDER BY enqueue_seq ASC
		LIMIT 1
	`
	result := tx.QueryRowContext(ctx, query)
	run, err := scanTaskRun(result)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.TaskRun{}, storecontract.ErrNotFound
		}
		return domain.TaskRun{}, fmt.Errorf("查询最早 queued TaskRun: %w", err)
	}
	claimed, err := run.Claim(now)
	if err != nil {
		return domain.TaskRun{}, err
	}
	const update = `
		UPDATE task_runs
		SET status = ?, claimed_at = ?
		WHERE id = ? AND status = ?
	`
	resultExec, err := tx.ExecContext(
		ctx,
		update,
		string(claimed.Status),
		claimed.ClaimedAt.UTC().UnixNano(),
		claimed.ID,
		string(run.Status),
	)
	if err != nil {
		return domain.TaskRun{}, fmt.Errorf("更新 TaskRun 为 claimed: %w", err)
	}
	rowsAffected, err := resultExec.RowsAffected()
	if err != nil {
		return domain.TaskRun{}, fmt.Errorf("读取 TaskRun 更新结果: %w", err)
	}
	if rowsAffected != 1 {
		return domain.TaskRun{}, storecontract.ErrTaskConflict
	}
	if err := tx.Commit(); err != nil {
		return domain.TaskRun{}, fmt.Errorf("提交事务: %w", err)
	}
	return claimed, nil
}

// SaveTask 使用 expectedStatus 条件更新已存在的 TaskRun，不允许 UPSERT 创建记录。
//
// 最终 SQL 必须包含 `WHERE id = ? AND status = ?`。RowsAffected 为 0 时再区分 ErrNotFound
// 与 ErrTaskConflict；不能先在事务外读取状态再无条件 UPDATE。
//
// UPDATE 需要写回状态、三个可空时间、TaskResult 四列和 failure_reason。
// timeToUnixNano 和 resultColumns 的转换集中放在 scan.go，避免 nil 语义散落在 SQL 参数中。
func (s *Store) SaveTask(ctx context.Context, run domain.TaskRun, expectedStatus domain.TaskStatus) error {
	const query = `
		UPDATE task_runs
		SET
			status = ?,
			claimed_at = ?,
			started_at = ?,
			finished_at = ?,
			result_summary = ?,
			codex_thread_id = ?,
			worktree_path = ?,
			result_revision = ?,
			failure_reason = ?
		WHERE id = ? AND status = ?
	`

	resultSummary, codexThreadID, worktreePath, resultRevision := resultColumns(run.Result)
	result, err := s.db.ExecContext(
		ctx,
		query,
		string(run.Status),
		nullableUnixNano(run.ClaimedAt),
		nullableUnixNano(run.StartedAt),
		nullableUnixNano(run.FinishedAt),
		resultSummary,
		codexThreadID,
		worktreePath,
		resultRevision,
		nullableString(run.FailureReason),
		run.ID,
		string(expectedStatus),
	)
	if err != nil {
		return fmt.Errorf("保存 TaskRun: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取 TaskRun 保存结果: %w", err)
	}
	if rowsAffected > 0 {
		return nil
	}

	var exists bool

	err = s.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM task_runs WHERE id = ?)`,
		run.ID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("检查 TaskRun 是否存在: %w", err)
	}
	if !exists {
		return storecontract.ErrNotFound
	}
	return storecontract.ErrTaskConflict
}

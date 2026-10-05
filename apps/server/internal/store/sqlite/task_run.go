package sqlite

import (
	"context"
	"time"

	"product-agent-workbench/internal/domain"
)

// EnqueueTask 保存 queued TaskRun 和稳定入队顺序。
//
// migration 必须提供持久化顺序事实；不能只按 CreatedAt 排序，因为两个任务可以同一时刻
// 创建。只接受 queued，重复 ID 映射 ErrAlreadyExists。
func (s *Store) EnqueueTask(ctx context.Context, run domain.TaskRun) error {
	return errNotImplemented
}

// GetTask 查询并完整还原 TaskRun。
//
// 所有 nullable 时间和 Result 字段集中经 scan.go 处理，避免每条查询各自猜测 nil 语义。
func (s *Store) GetTask(ctx context.Context, id string) (domain.TaskRun, error) {
	return domain.TaskRun{}, errNotImplemented
}

// ClaimNextTask 原子领取最早 queued TaskRun。
//
// SQLite 没有 SELECT FOR UPDATE。实现前先在 migration/README.md 记录具体事务与条件更新
// 方案，并用两个独立连接的并发测试证明同一任务最多被领取一次。
func (s *Store) ClaimNextTask(ctx context.Context, now time.Time) (domain.TaskRun, error) {
	return domain.TaskRun{}, errNotImplemented
}

// SaveTask 使用 expectedStatus 条件更新已存在的 TaskRun，不允许 UPSERT 创建记录。
//
// 最终 SQL 必须包含 `WHERE id = ? AND status = ?`。RowsAffected 为 0 时再区分 ErrNotFound
// 与 ErrTaskConflict；不能先在事务外读取状态再无条件 UPDATE。
func (s *Store) SaveTask(ctx context.Context, run domain.TaskRun, expectedStatus domain.TaskStatus) error {
	return errNotImplemented
}

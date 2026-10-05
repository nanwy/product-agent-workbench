package sqlite

import (
	"context"

	"product-agent-workbench/internal/domain"
)

// AppendTaskEvents 在单个事务中追加完整批次。
//
// 必须先验证 TaskRun、批内身份和连续 sequence，再写任何一行；失败批次不能部分落盘，
// 也不能消耗全局事件 ID。
func (s *Store) AppendTaskEvents(ctx context.Context, events []domain.TaskEvent) (int64, error) {
	return 0, errNotImplemented
}

// ListTaskEvents 返回 sequence > afterSequence 的事件，并按 sequence 升序排列。
//
// QueryContext 后立即 defer rows.Close()，循环后检查 rows.Err()；Payload 必须复制到新的
// byte slice，不能复用 driver 缓冲区。
func (s *Store) ListTaskEvents(
	ctx context.Context,
	taskRunID string,
	afterSequence int64,
) ([]domain.TaskEvent, error) {
	return nil, errNotImplemented
}

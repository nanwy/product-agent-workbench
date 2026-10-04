package store

import (
	"context"

	"product-agent-workbench/internal/domain"
)

// Store 描述工作流真正需要的持久化能力。
//
// 先在 internal/store/memory 中实现它并通过契约测试，再创建 SQLite Adapter。不要为了
// “以后可能替换数据库”继续拆出 RequirementRepository、TaskRepository 等浅接口。
type Store interface {
	CreateRequirement(ctx context.Context, requirement domain.Requirement) error
	GetRequirement(ctx context.Context, id string) (domain.Requirement, error)
	SaveRequirement(
		ctx context.Context,
		requirement domain.Requirement,
		expectedRevision int64,
	) (int64, error)

	EnqueueTask(ctx context.Context, run domain.TaskRun) error
	ClaimNextTask(ctx context.Context) (*domain.TaskRun, error)
	SaveTask(ctx context.Context, run domain.TaskRun) error
	AppendTaskEvents(ctx context.Context, events []domain.TaskEvent) (int64, error)
}

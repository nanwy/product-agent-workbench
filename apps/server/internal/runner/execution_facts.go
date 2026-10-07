package runner

import (
	"context"
	"fmt"

	"product-agent-workbench/internal/domain"
)

// TaskHistoryReader 提供恢复 Codex Thread 所需的最小 TaskRun 历史查询。
type TaskHistoryReader interface {
	ListTasksByTicketVersion(
		ctx context.Context,
		requirementID string,
		ticketVersionID string,
	) ([]domain.TaskRun, error)
}

// resumableExecutionFactsReader 在 artifact 执行事实之上补充可信的历史 Thread ID。
//
// artifact 描述“要做什么”，SQLite 历史描述“上次做到哪一个会话”。两者来源不同，不能
// 把运行时 Thread ID 反写进不可变 artifact。
type resumableExecutionFactsReader struct {
	artifacts ExecutionFactsReader
	history   TaskHistoryReader
}

// NewResumableExecutionFactsReader 组合静态 artifact 与动态执行历史。
func NewResumableExecutionFactsReader(
	artifacts ExecutionFactsReader,
	history TaskHistoryReader,
) (ExecutionFactsReader, error) {
	if artifacts == nil || history == nil {
		return nil, ErrInvalidDependencies
	}
	return &resumableExecutionFactsReader{artifacts: artifacts, history: history}, nil
}

func (r *resumableExecutionFactsReader) ReadExecutionFacts(
	ctx context.Context,
	run domain.TaskRun,
) (ExecutionFacts, error) {
	facts, err := r.artifacts.ReadExecutionFacts(ctx, run)
	if err != nil {
		return ExecutionFacts{}, err
	}
	if facts.CodexThreadID != "" {
		return facts, nil
	}

	history, err := r.history.ListTasksByTicketVersion(ctx, run.RequirementID, run.TicketVersionID)
	if err != nil {
		return ExecutionFacts{}, fmt.Errorf("读取 Codex Thread 历史: %w", err)
	}
	latestAttempt := 0
	for _, previous := range history {
		if previous.TicketID != run.TicketID || previous.Attempt >= run.Attempt ||
			previous.CodexThreadID == "" || previous.Attempt <= latestAttempt {
			continue
		}
		latestAttempt = previous.Attempt
		facts.CodexThreadID = previous.CodexThreadID
	}
	return facts, nil
}

var _ ExecutionFactsReader = (*resumableExecutionFactsReader)(nil)

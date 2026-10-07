package runner

import (
	"context"
	"fmt"

	"product-agent-workbench/internal/domain"
)

// TaskEventAppender 是 EventSink 真正需要的最小持久化能力。
//
// 前端类比：组件通常只要求一个 onChange(value) prop，而不会接收整个 store。这里也不把
// 完整 store.Store 交给适配器，只声明“追加事件”这一项能力。SQLite Store 和 Memory Store
// 已经拥有相同签名，因此都能满足这个接口。
type TaskEventAppender interface {
	AppendTaskEvents(ctx context.Context, events []domain.TaskEvent) (int64, error)
}

// NewStoreEventSink 把 Store 的“批量追加事件”接口适配成 Executor 需要的“单条事件回调”。
//
// 前端可以把目标形状理解成：
//
//	const sink: EventSink = async (event) => {
//	  await store.appendTaskEvents([event])
//	}
//
// Go 实现步骤：
//
//  1. appender 为 nil 时返回 ErrInvalidDependencies；
//  2. 返回一个符合 EventSink 签名的闭包；
//  3. 闭包先检查 ctx.Err()，再创建只包含 event 的非 nil 切片；
//  4. 调用 appender.AppendTaskEvents；全局 lastID 暂时不用，但错误必须用 %w 包装并返回；
//  5. 保存成功返回 nil。
//
// 不要在这里分配 Sequence：eventEmitter 是唯一分配者；Store 只验证它是否连续。
func NewStoreEventSink(appender TaskEventAppender) (EventSink, error) {
	if appender == nil {
		return nil, ErrInvalidDependencies
	}
	return func(ctx context.Context, event domain.TaskEvent) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		events := []domain.TaskEvent{event}
		if _, err := appender.AppendTaskEvents(ctx, events); err != nil {
			return fmt.Errorf("追加 TaskRun %q 的事件 %d: %w", event.TaskRunID, event.Sequence, err)
		}
		return nil
	}, nil
}

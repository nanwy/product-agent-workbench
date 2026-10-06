package runner

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
)

// ErrInvalidSupervisorDependencies 表示并发上限或 Worker 工厂配置无效。
var ErrInvalidSupervisorDependencies = errors.New("Supervisor 依赖无效")

// TaskWorker 是 Supervisor 真正需要的最小 Worker seam。
type TaskWorker interface {
	RunNext(ctx context.Context) (domain.TaskRun, error)
}

// WorkerFactory 为每个并发槽位创建独立 Worker。
//
// Store、Application 可以安全共享；真实 Executor 若持有子进程或 session 状态，应由工厂为
// 每个槽位创建独立实例，不能让多个 goroutine 非受控地共享可变状态。
type WorkerFactory func(slot int) (TaskWorker, error)

// SupervisorDependencies 配置固定并发上限。
type SupervisorDependencies struct {
	Concurrency int
	NewWorker   WorkerFactory
}

// Supervisor 使用固定数量的 Worker 并行排空当前队列。
//
// 它不拥有无限轮询和进程生命周期；Drain 的调用方提供 context 并同步等待所有槽位退出。
// 这样每个 goroutine 都有明确 owner、退出条件和等待点。
type Supervisor struct {
	workers []TaskWorker
}

// DrainResult 返回本轮实际处理过的 TaskRun。
//
// Runs 按 Worker 槽位和槽位内执行顺序排列，不表示全局完成时间顺序；持久化顺序仍以 Store
// 和 TaskEvent 为准。
type DrainResult struct {
	Runs []domain.TaskRun
}

type supervisorSlotResult struct {
	runs []domain.TaskRun
	err  error
}

// NewSupervisor 在启动 goroutine 前创建全部 Worker，避免运行到一半才发现槽位配置错误。
func NewSupervisor(dependencies SupervisorDependencies) (*Supervisor, error) {
	if dependencies.Concurrency < 1 || dependencies.NewWorker == nil {
		return nil, ErrInvalidSupervisorDependencies
	}

	workers := make([]TaskWorker, dependencies.Concurrency)
	for slot := range workers {
		worker, err := dependencies.NewWorker(slot)
		if err != nil {
			return nil, fmt.Errorf("创建 Worker 槽位 %d: %w", slot, err)
		}
		if worker == nil {
			return nil, fmt.Errorf("%w: Worker 槽位 %d 为空", ErrInvalidSupervisorDependencies, slot)
		}
		workers[slot] = worker
	}
	return &Supervisor{workers: workers}, nil
}

// Drain 并行消费 queued TaskRun，直到每个槽位遇到队列为空、context 取消或执行错误。
func (s *Supervisor) Drain(ctx context.Context) (DrainResult, error) {
	// 每个 goroutine 只写自己的下标，不共享 append，因此不需要额外互斥锁。
	slots := make([]supervisorSlotResult, len(s.workers))
	var waitGroup sync.WaitGroup
	waitGroup.Add(len(s.workers))
	for slot, worker := range s.workers {
		go func() {
			defer waitGroup.Done()
			slots[slot] = drainWorker(ctx, worker)
		}()
	}

	// Drain 是结构化并发入口：返回前一定等待本轮创建的全部 goroutine 退出。
	waitGroup.Wait()

	result := DrainResult{Runs: make([]domain.TaskRun, 0)}
	errs := make([]error, 0, len(slots))
	for slot, slotResult := range slots {
		result.Runs = append(result.Runs, slotResult.runs...)
		if slotResult.err != nil {
			errs = append(errs, fmt.Errorf("Worker 槽位 %d: %w", slot, slotResult.err))
		}
	}
	return result, errors.Join(errs...)
}

func drainWorker(ctx context.Context, worker TaskWorker) supervisorSlotResult {
	result := supervisorSlotResult{runs: make([]domain.TaskRun, 0)}
	for {
		if err := ctx.Err(); err != nil {
			result.err = err
			return result
		}

		run, err := worker.RunNext(ctx)
		if run.ID != "" {
			result.runs = append(result.runs, run)
		}
		if errors.Is(err, store.ErrNotFound) {
			// 队列为空是 Drain 的正常结束条件，不是执行失败。
			return result
		}
		if err != nil {
			result.err = err
			return result
		}
	}
}

package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
)

// ErrInvalidDependencies 表示 Worker 缺少应用用例或执行 Adapter。
var ErrInvalidDependencies = errors.New("Worker 依赖无效")

// TaskRunApplication 是 Worker 消费的 TaskRun 生命周期 seam。
type TaskRunApplication interface {
	ClaimNextTaskRun(ctx context.Context) (domain.TaskRun, error)
	StartTaskRun(ctx context.Context, id string) (domain.TaskRun, error)
	CompleteTaskRun(ctx context.Context, input app.CompleteTaskRunInput) (domain.TaskRun, error)
	FailTaskRun(ctx context.Context, input app.FailTaskRunInput) (domain.TaskRun, error)
}

// RequestBuilder 把已开始的 TaskRun 与已确认配置组合成稳定 RunRequest。
//
// RepositoryRoot 和 Prompt 不能由浏览器随意传入；以后从受信任项目配置、Spec 和 Ticket
// artifact 构造。把它注入可让当前阶段使用 fake，而不提前实现 Git/Codex。
type RequestBuilder func(ctx context.Context, run domain.TaskRun) (RunRequest, error)

// WorkerDependencies 是执行一条队列记录所需的最小依赖。
type WorkerDependencies struct {
	Application  TaskRunApplication
	Executor     Runner
	BuildRequest RequestBuilder
	EventSink    EventSink
}

// Worker 串联应用状态机与真正的执行 Adapter。
//
// 第一版故意没有 goroutine 和无限循环。调用方显式调用 RunNext；等单次执行契约稳定后，
// 再由进程级 Supervisor 决定轮询、并发上限和优雅退出。
type Worker struct {
	application  TaskRunApplication
	executor     Runner
	buildRequest RequestBuilder
	eventSink    EventSink
}

// NewWorker 校验并保存依赖，不领取任务、不启动 goroutine。
func NewWorker(dependencies WorkerDependencies) (*Worker, error) {
	if dependencies.Application == nil || dependencies.Executor == nil ||
		dependencies.BuildRequest == nil || dependencies.EventSink == nil {
		return nil, ErrInvalidDependencies
	}
	return &Worker{
		application:  dependencies.Application,
		executor:     dependencies.Executor,
		buildRequest: dependencies.BuildRequest,
		eventSink:    dependencies.EventSink,
	}, nil
}

// RunNext 同步执行队列中的下一条 TaskRun。
//
// 对应原 TypeScript `prepareExecution + executeQueue` 的最小纵向切片：
//
//  1. ClaimNextTaskRun；没有任务时原样返回 ErrNotFound。
//  2. StartTaskRun，确认控制面进入 running。
//  3. BuildRequest，把受信任配置与 Ticket 转为 RunRequest。
//  4. Executor.Run(ctx, request, EventSink)。不要在这里另起 goroutine。
//  5. 成功时 CompleteTaskRun；失败时 FailTaskRun，并保留原执行错误供日志判断。
//
// context 取消时不要用 context.Background() 强行写失败；任务保留为 claimed/running，后续
// 重启恢复策略根据持久事实决定重试或失败。
func (w *Worker) RunNext(ctx context.Context) (domain.TaskRun, error) {
	// 第一步：从 Store 原子领取最早的 queued 任务。
	// ClaimNextTaskRun 已经把“查找 + queued → claimed”封装成一个原子操作，Worker
	// 不能先查询再领取，否则多个 Worker 可能拿到同一条任务。
	claimed, err := w.application.ClaimNextTaskRun(ctx)
	if err != nil {
		return domain.TaskRun{}, err
	}

	// 第二步：在调用外部执行器之前，先把控制面事实推进为 running。
	// 如果这里失败，任务仍停留在 claimed；恢复策略以后可以根据这个状态决定如何处理。
	running, err := w.application.StartTaskRun(ctx, claimed.ID)
	if err != nil {
		return claimed, fmt.Errorf("启动 TaskRun %q: %w", claimed.ID, err)
	}

	// 第三步：只从服务端可信配置和已确认 Ticket artifact 构造执行请求。
	// BuildRequest 失败说明当前任务无法执行，也应把已经 running 的任务保存为 failed。
	request, err := w.buildRequest(ctx, running)
	if err != nil {
		return w.failRunningTask(ctx, running, fmt.Errorf("构造 TaskRun %q 的执行请求: %w", running.ID, err))
	}

	// RequestBuilder 只负责补充仓库路径和 Prompt。TaskRun 必须使用刚刚持久化的 running
	// 快照，避免错误的 Builder 返回另一条任务或旧状态并传给执行器。
	request.TaskRun = running

	// 第四步：同步执行一次任务。这里故意不创建 goroutine；调用方拥有 ctx，也负责决定
	// RunNext 何时返回。事件接收器由 Executor 在执行过程中按顺序调用。
	runResult, err := w.executor.Run(ctx, request, w.eventSink)
	if err != nil {
		return w.failRunningTask(ctx, running, err)
	}

	// 第五步：只有执行器成功返回后，才允许保存 completed 和执行结果。
	// 如果落盘失败，不能再把任务标成 failed，因为外部执行实际上已经成功；调用方需要
	// 保留这个错误，后续恢复流程再核对 running 记录与外部结果。
	completed, err := w.application.CompleteTaskRun(ctx, app.CompleteTaskRunInput{
		ID:     running.ID,
		Result: runResult.Result,
	})
	if err != nil {
		return running, fmt.Errorf("完成 TaskRun %q: %w", running.ID, err)
	}
	return completed, nil
}

// failRunningTask 尝试把已经进入 running 的任务保存为 failed，同时保留最初执行错误。
//
// context 已取消时不能改用 context.Background() 绕过调用方：此时保持 running 事实，交给
// 后续重启恢复逻辑判断。若保存 failed 也失败，则用 errors.Join 同时返回两个原因。
func (w *Worker) failRunningTask(
	ctx context.Context,
	running domain.TaskRun,
	cause error,
) (domain.TaskRun, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return running, ctxErr
	}

	reason := strings.TrimSpace(cause.Error())
	if reason == "" {
		// 领域层不接受空失败原因；这里提供稳定描述，但仍通过返回值保留原始 error。
		reason = "执行器返回了空失败原因"
	}
	failed, err := w.application.FailTaskRun(ctx, app.FailTaskRunInput{
		ID:     running.ID,
		Reason: reason,
	})
	if err != nil {
		return running, errors.Join(cause, fmt.Errorf("保存 TaskRun %q 失败状态: %w", running.ID, err))
	}
	return failed, cause
}

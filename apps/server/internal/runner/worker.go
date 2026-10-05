package runner

import (
	"context"
	"errors"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
)

// ErrInvalidDependencies 表示 Worker 缺少应用用例或执行 Adapter。
var ErrInvalidDependencies = errors.New("Worker 依赖无效")

// errRunNextNotImplemented 只用于最小 Worker 学习脚手架。
var errRunNextNotImplemented = errors.New("Worker.RunNext 尚未实现")

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
	return domain.TaskRun{}, errRunNextNotImplemented
}

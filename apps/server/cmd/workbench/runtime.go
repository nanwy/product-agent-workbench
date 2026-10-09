package main

import (
	"fmt"
	"net/http"
	"time"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/httpapi"
	"product-agent-workbench/internal/runner"
	"product-agent-workbench/internal/scheduler"
	"product-agent-workbench/internal/store"
	"product-agent-workbench/internal/webui"
	"product-agent-workbench/internal/workspace"
)

// runtimeDependencies 是 buildRuntime 需要的、已经创建好的外部依赖。
//
// 前端类比：这些对象类似 App 根节点收到的 queryClient、router、authClient。buildRuntime
// 只负责把它们传给各层构造函数，不负责打开数据库文件或读取环境变量。
//
// 为什么不使用全局变量：测试可以传 fake Store/ArtifactReader/Executor；每个依赖从哪里来
// 一目了然，也不会因为 import 某个包就偷偷启动进程或连接数据库。
type runtimeDependencies struct {
	Store           store.Store
	Artifacts       scheduler.ArtifactReader
	ExecutionFacts  runner.ExecutionFactsReader
	Workspaces      workspace.Manager
	Executor        runner.Runner
	Verifier        app.DeliveryVerifier
	SpecGenerator   app.SpecGenerator
	TicketGenerator app.TicketGenerator
	TicketPublisher app.TicketPublisher
	TargetRuntime   httpapi.TargetRuntime
	NewID           func() string
	Now             func() time.Time
	Concurrency     int
}

// runtimeComponents 是装配完成后，进程生命周期真正需要持有的两个入口。
//
// Handler 接收浏览器请求；Supervisor 消费已经入队的 TaskRun。Application、Scheduler、
// EventSink 等对象会被这两个入口通过字段或闭包继续引用，所以不必全部重复放进这个结构体。
type runtimeComponents struct {
	Application *app.Application
	Handler     http.Handler
	Supervisor  *runner.Supervisor
}

// buildRuntime 是项目的 composition root（依赖装配根）。
//
// 前端可以把它理解成：
//
//	const app = (
//	  <QueryClientProvider client={queryClient}>
//	    <RouterProvider router={router} />
//	  </QueryClientProvider>
//	)
//
// 区别是 Go 不使用 JSX，而是把上一步构造出的对象显式传给下一步构造函数。
//
// 实现前先画出依赖图：
//
//	Store ───────────────→ Application ← Verification
//	  │                         │               │
//	  ├─→ Scheduler ← Artifacts │               ├─→ HTTP Server
//	  │        ↑                │               │
//	  │        └─ CompletionHook┘               │
//	  └─→ EventSink                             │
//	                                             │
//	ExecutionFacts → RequestBuilder              │
//	Workspaces + Executor → ManagedRunner         │
//	以上四项 + Application → WorkerFactory(闭包) ┘
//	WorkerFactory + Concurrency → Supervisor
//
// 按下面顺序实现，每完成一步先编译：
//
//  1. 校验所有接口/函数不是 nil，Concurrency >= 1；错误返回 runner.ErrInvalidDependencies；
//  2. app.New：注入同一个 Store、Ticket artifact reader、Verification、NewID、Now；
//  3. scheduler.New：注入 Artifacts、Store、NewID、Now；
//  4. runner.NewStoreEventSink：把同一个 Store 适配为 EventSink；
//  5. runner.NewTrustedRequestBuilder：把 ExecutionFacts reader 适配为 RequestBuilder；
//  6. runner.NewManagedRunner：组合 Workspaces 与 Executor；
//  7. 创建 runner.WorkerFactory 闭包。闭包会捕获前面创建的 Application、ManagedRunner、
//     RequestBuilder、EventSink 和 Scheduler；每次 Supervisor 调用它时返回一个新 Worker；
//  8. runner.NewSupervisor：传入 Concurrency 和 WorkerFactory；
//  9. httpapi.NewServer：Application 同时提供 Requirement、TaskRun、Verification 用例，
//     Scheduler 作为 TaskScheduler；
//
// 10. 返回 runtimeComponents{Handler: server, Supervisor: supervisor}, nil。
//
// 任何一步失败都要立即用 fmt.Errorf("创建 XXX: %w", err) 返回，不要继续构造后续对象。
// 本函数不启动 goroutine、不监听端口、不关闭资源，只负责连接对象。
func buildRuntime(dependencies runtimeDependencies) (runtimeComponents, error) {
	if err := validateRuntimeDependencies(dependencies); err != nil {
		return runtimeComponents{}, err
	}

	// 前端类比：先创建共享的 queryClient。后面的 HTTP Server 和每个 Worker 都引用同一个
	// Application，因此状态变更会经过同一套用例规则和同一个 Store。
	application, err := app.New(app.Dependencies{
		Store:           dependencies.Store,
		Tickets:         dependencies.Artifacts,
		Verification:    dependencies.Verifier,
		SpecGenerator:   dependencies.SpecGenerator,
		TicketGenerator: dependencies.TicketGenerator,
		TicketPublisher: dependencies.TicketPublisher,
		NewID:           dependencies.NewID,
		Now:             dependencies.Now,
	})
	if err != nil {
		return runtimeComponents{}, fmt.Errorf("创建 Application: %w", err)
	}

	// Scheduler 只判断 Ticket 依赖是否已经满足并创建 queued TaskRun，不直接执行任务。
	reconciler, err := scheduler.New(scheduler.Dependencies{
		Artifacts: dependencies.Artifacts,
		Tasks:     dependencies.Store,
		NewID:     dependencies.NewID,
		Now:       dependencies.Now,
	})
	if err != nil {
		return runtimeComponents{}, fmt.Errorf("创建 Scheduler: %w", err)
	}

	// 下面两个构造函数返回闭包：eventSink 记住 Store，buildRequest 记住可信事实读取器。
	// Worker 调用它们时只需要传本次变化的事件或 TaskRun，不必重复传固定依赖。
	eventSink, err := runner.NewStoreEventSink(dependencies.Store)
	if err != nil {
		return runtimeComponents{}, fmt.Errorf("创建 EventSink: %w", err)
	}
	resumableFacts, err := runner.NewResumableExecutionFactsReader(
		dependencies.ExecutionFacts,
		dependencies.Store,
	)
	if err != nil {
		return runtimeComponents{}, fmt.Errorf("创建可恢复执行事实 Reader: %w", err)
	}
	buildRequest, err := runner.NewTrustedRequestBuilder(resumableFacts)
	if err != nil {
		return runtimeComponents{}, fmt.Errorf("创建 RequestBuilder: %w", err)
	}

	managedRunner, err := runner.NewManagedRunner(runner.ManagedRunnerDependencies{
		Workspaces: dependencies.Workspaces,
		Executor:   dependencies.Executor,
	})
	if err != nil {
		return runtimeComponents{}, fmt.Errorf("创建 ManagedRunner: %w", err)
	}

	// 前端类比：newWorker 像一个已经绑定 Provider 值的组件工厂。它捕获上面创建的五个
	// 稳定依赖；Supervisor 只负责调用工厂，不需要知道这些对象是怎样构造出来的。
	newWorker := func(_ int) (runner.TaskWorker, error) {
		return runner.NewWorker(runner.WorkerDependencies{
			Application:  application,
			Executor:     managedRunner,
			BuildRequest: buildRequest,
			EventSink:    eventSink,
			// 方法值也是闭包：它记住 reconciler，Worker 只需要传 ctx 和 completed。
			OnCompleted: reconciler.OnTaskCompleted,
		})
	}

	supervisor, err := runner.NewSupervisor(runner.SupervisorDependencies{
		Concurrency: dependencies.Concurrency,
		NewWorker:   newWorker,
	})
	if err != nil {
		return runtimeComponents{}, fmt.Errorf("创建 Supervisor: %w", err)
	}

	server, err := httpapi.NewServer(httpapi.Dependencies{
		Application:   application,
		Planning:      application,
		TaskRuns:      application,
		Scheduler:     reconciler,
		Events:        application,
		Verification:  application,
		TargetRuntime: dependencies.TargetRuntime,
		Preview:       application,
	})
	if err != nil {
		return runtimeComponents{}, fmt.Errorf("创建 HTTP Server: %w", err)
	}
	// 练习 20：API 与嵌入式 SPA 在这里合成一个 Handler。前端类比是生产反向代理：
	// /api 仍进入 Go handler，其它 GET/HEAD 请求由静态资源与 SPA fallback 处理。
	handler, err := webui.New(server)
	if err != nil {
		return runtimeComponents{}, fmt.Errorf("创建嵌入式 Web UI: %w", err)
	}

	return runtimeComponents{
		Application: application,
		Handler:     handler,
		Supervisor:  supervisor,
	}, nil
}

// validateRuntimeDependencies 在创建任何组件前检查启动配置。
//
// 这类似 React 根组件先确认必需 Provider 都存在：缺一个就直接拒绝启动，避免得到一个
// 只有部分路由或部分后台任务可用的运行时。这里不调用依赖，只检查“有没有传进来”。
func validateRuntimeDependencies(dependencies runtimeDependencies) error {
	missingDependency := dependencies.Store == nil ||
		dependencies.Artifacts == nil ||
		dependencies.ExecutionFacts == nil ||
		dependencies.Workspaces == nil ||
		dependencies.Executor == nil ||
		dependencies.Verifier == nil ||
		dependencies.SpecGenerator == nil ||
		dependencies.TicketGenerator == nil ||
		dependencies.TicketPublisher == nil ||
		dependencies.TargetRuntime == nil ||
		dependencies.NewID == nil ||
		dependencies.Now == nil
	if missingDependency || dependencies.Concurrency < 1 {
		return runner.ErrInvalidDependencies
	}
	return nil
}

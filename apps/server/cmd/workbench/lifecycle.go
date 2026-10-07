package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"

	"product-agent-workbench/internal/artifact"
	"product-agent-workbench/internal/codex"
	"product-agent-workbench/internal/runner"
	storeadapter "product-agent-workbench/internal/store/sqlite"
	"product-agent-workbench/internal/workspace"
)

// run 拥有进程级资源的完整生命周期。
//
// 前端类比：buildRuntime 类似 render(<App />)，run 更像浏览器加载入口到页面卸载的全过程：
// 它先创建长期资源，启动 HTTP 与后台 Worker，收到退出信号后再按相反方向释放资源。
func run(ctx context.Context, config processConfig) error {
	if ctx == nil {
		return fmt.Errorf("启动 Workbench: context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateProcessConfig(config); err != nil {
		return err
	}

	// database/sql 返回连接池。谁打开它，谁负责 Close；Store 只借用连接池，不拥有它。
	db, err := openDatabase(ctx, config.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()

	persistence, err := storeadapter.New(db)
	if err != nil {
		return fmt.Errorf("创建 SQLite Store: %w", err)
	}
	artifacts, err := artifact.NewStore(config.ArtifactRoot)
	if err != nil {
		return fmt.Errorf("创建 Artifact Store: %w", err)
	}
	workspaces, err := workspace.NewGitManager(workspace.GitManagerDependencies{
		Root:      config.WorktreeRoot,
		GitBinary: config.GitExecutable,
	})
	if err != nil {
		return fmt.Errorf("创建 Git Worktree Manager: %w", err)
	}
	// Recover 只核对工作台自己的 ownership 记录，不会接管未知目录或删除用户文件。
	recovered, err := workspaces.Recover(ctx)
	if err != nil {
		return fmt.Errorf("恢复受管 Worktree: %w", err)
	}
	if len(recovered) > 0 {
		log.Printf("已恢复 %d 个受管 Worktree", len(recovered))
	}

	executor, err := codex.NewExecutor(codex.ExecutorConfig{
		Executable: config.CodexExecutable,
	})
	if err != nil {
		return fmt.Errorf("创建 Codex Executor: %w", err)
	}
	components, err := buildRuntime(runtimeDependencies{
		Store:          persistence,
		Artifacts:      artifacts,
		ExecutionFacts: artifacts,
		Workspaces:     workspaces,
		Executor:       executor,
		NewID:          uuid.NewString,
		Now:            time.Now,
		Concurrency:    config.Concurrency,
	})
	if err != nil {
		return fmt.Errorf("装配 Workbench: %w", err)
	}
	// HTTP 和 Worker 启动前先收敛上次进程留下的 claimed/running 记录。不能自动重跑，
	// 因为旧进程可能已经在 Worktree 或外部系统产生了部分副作用。
	recoveredRuns, err := components.Application.RecoverInterruptedTaskRuns(ctx)
	if err != nil {
		return fmt.Errorf("恢复中断的 TaskRun: %w", err)
	}
	if len(recoveredRuns) > 0 {
		log.Printf("已把 %d 条中断的 TaskRun 标记为 failed", len(recoveredRuns))
	}

	return serveRuntime(ctx, config, components)
}

// serveRuntime 同时拥有 HTTP Server 和 Supervisor 循环。
//
// 两个 goroutine 都从同一个 lifecycleContext 接收取消信号；函数返回前会等待它们退出，
// 避免 main 已结束但后台仍在访问数据库或遗留子进程。
func serveRuntime(
	ctx context.Context,
	config processConfig,
	components runtimeComponents,
) error {
	listener, err := net.Listen("tcp", config.Address)
	if err != nil {
		return fmt.Errorf("监听 %s: %w", config.Address, err)
	}
	return serveRuntimeOnListener(ctx, config, components, listener)
}

// serveRuntimeOnListener 把“监听端口”与“管理已打开监听器的生命周期”分开，测试可以提供
// 不占用真实端口的 listener；生产路径仍只通过 serveRuntime 创建 TCP listener。
func serveRuntimeOnListener(
	ctx context.Context,
	config processConfig,
	components runtimeComponents,
	listener net.Listener,
) error {
	defer listener.Close()

	lifecycleContext, cancel := context.WithCancel(ctx)
	defer cancel()

	server := &http.Server{
		Handler:           components.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		// 每个 HTTP request 都成为进程 context 的子节点，关闭服务时取消会继续传到 Store。
		BaseContext: func(net.Listener) context.Context { return lifecycleContext },
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.Serve(listener)
	}()

	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		drainTasks(lifecycleContext, components.Supervisor, config.DrainInterval)
	}()

	log.Printf("Workbench 正在监听 http://%s", listener.Addr())
	select {
	case <-ctx.Done():
		// 先取消 Worker/Codex，再停止接收 HTTP；这样不会在关闭期间领取新的任务。
		cancel()
		shutdownContext, cancelShutdown := context.WithTimeout(
			context.WithoutCancel(ctx),
			config.ShutdownTimeout,
		)
		shutdownErr := server.Shutdown(shutdownContext)
		cancelShutdown()
		if shutdownErr != nil {
			// Shutdown 超时后强制关闭监听与现存连接，不能让进程无限挂住。
			shutdownErr = errors.Join(shutdownErr, server.Close())
		}
		serveErr := <-serverErrors
		<-workerDone
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return errors.Join(shutdownErr, fmt.Errorf("HTTP Server: %w", serveErr))
		}
		return shutdownErr
	case serveErr := <-serverErrors:
		cancel()
		<-workerDone
		if errors.Is(serveErr, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("HTTP Server: %w", serveErr)
	}
}

// drainTasks 周期性排空当前队列。单张 Ticket 失败已经由 Worker 持久化为 failed，不能让
// 一次业务失败带崩整个控制面；记录错误后，下一轮仍可处理其它 Requirement 的队列。
func drainTasks(ctx context.Context, supervisor *runner.Supervisor, interval time.Duration) {
	for {
		if _, err := supervisor.Drain(ctx); err != nil && ctx.Err() == nil {
			log.Printf("排空 TaskRun 队列失败: %v", err)
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/scheduler"
	"product-agent-workbench/internal/targetruntime"
	"product-agent-workbench/internal/verification"
)

// ErrInvalidDependencies 表示 HTTP Server 缺少必需的应用用例依赖。
// 这是启动期错误，main 应停止启动，而不是运行一个部分可用的 Server。
var ErrInvalidDependencies = errors.New("HTTP Server 依赖无效")

// RequirementApplication 是 HTTP 模块真正消费的应用用例 seam。
//
// 接口定义在消费方 httpapi，而不是实现方 app。HTTP 测试可以传入 fake，生产装配则传入
// *app.Application。后续新增 TaskRun endpoint 时，再加入它实际需要的方法。
type RequirementApplication interface {
	CreateRequirement(
		ctx context.Context,
		input app.CreateRequirementInput,
	) (domain.Requirement, error)
	GetRequirement(ctx context.Context, id string) (domain.Requirement, error)
}

// PlanningApplication 是步骤 21A/21B 的 Grilling 与 Spec 用例入口。
type PlanningApplication interface {
	ConfirmGrilling(ctx context.Context, input app.ConfirmGrillingInput) (app.PlanningSnapshot, error)
	GenerateSpec(ctx context.Context, input app.GenerateSpecInput) (app.PlanningSnapshot, error)
	ConfirmSpec(ctx context.Context, input app.ConfirmSpecInput) (app.PlanningSnapshot, error)
	GenerateTickets(ctx context.Context, input app.GenerateTicketsInput) (app.PlanningSnapshot, error)
	ConfirmTickets(ctx context.Context, input app.ConfirmTicketsInput) (app.PlanningSnapshot, error)
	GetPlanning(ctx context.Context, requirementID string) (app.PlanningSnapshot, error)
}

// TaskRunApplication 是浏览器查询和取消 TaskRun 使用的用例 seam。
//
// claim/start/complete/fail 和原始 EnqueueTaskRun 不暴露给浏览器，避免客户端伪造
// Ticket、依赖状态或 Runner 生命周期。
type TaskRunApplication interface {
	GetTaskRun(ctx context.Context, id string) (domain.TaskRun, error)
	CancelTaskRun(ctx context.Context, id string) (domain.TaskRun, error)
}

// TaskScheduler 是浏览器启动一个已确认 TicketVersion 时调用的 seam。
//
// 浏览器只提供 Requirement 和版本身份；具体 TicketID、Attempt 及 ready 判断全部来自
// Reconciler 读取的确认 artifact 和持久化执行历史。
type TaskScheduler interface {
	Reconcile(
		ctx context.Context,
		requirementID string,
		ticketVersionID string,
	) (scheduler.ReconcileResult, error)
}

// TaskEventApplication 是 SSE endpoint 消费的持久化重放用例。
//
// HTTP 只认识全局游标，不直接访问 Store；游标是否失效由应用层统一判断。
type TaskEventApplication interface {
	ReplayTaskEvents(
		ctx context.Context,
		input app.ReplayTaskEventsInput,
	) (app.TaskEventReplay, error)
}

// VerificationApplication 是浏览器触发真实验证时使用的用例 seam。
type VerificationApplication interface {
	VerifyRequirement(
		ctx context.Context,
		input app.VerifyRequirementInput,
	) (app.VerifyRequirementResult, error)
	GetLatestVerification(
		ctx context.Context,
		requirementID string,
		ticketVersionID string,
	) (verification.Report, error)
}

// TargetRuntime 是 21D 的进程生命周期 seam。HTTP 层只表达动作，不接收命令参数。
type TargetRuntime interface {
	Target() targetruntime.Target
	Snapshot() targetruntime.Snapshot
	Start(ctx context.Context, workspacePath string) (targetruntime.Snapshot, error)
	Stop(ctx context.Context) (targetruntime.Snapshot, error)
}

// PreviewApplication 把浏览器提交的领域 ID 解析成服务端可信的候选 Worktree。
type PreviewApplication interface {
	ResolvePreviewWorkspace(ctx context.Context, requirementID string, ticketVersionID string) (string, error)
}

const (
	defaultEventPollInterval      = 250 * time.Millisecond
	defaultEventHeartbeatInterval = 15 * time.Second
	defaultEventReplayLimit       = 100
)

// Dependencies 是 HTTP Server 启动时必须具备的依赖。
type Dependencies struct {
	Application   RequirementApplication
	Planning      PlanningApplication
	TaskRuns      TaskRunApplication
	Scheduler     TaskScheduler
	Events        TaskEventApplication
	Verification  VerificationApplication
	TargetRuntime TargetRuntime
	Preview       PreviewApplication

	// 下面三个值主要用于测试注入短周期；生产装配传零值即可使用安全默认值。
	EventPollInterval      time.Duration
	EventHeartbeatInterval time.Duration
	EventReplayLimit       int
}

// Server 把 HTTP 协议翻译为应用用例调用。
//
// Server 只负责路由、JSON、状态码和 header；输入规则、状态迁移与持久化都不属于这里。
// mux 和 application 在构造后只读，因此 Server 可以被 net/http 并发调用。
type Server struct {
	mux           *http.ServeMux
	application   RequirementApplication
	planning      PlanningApplication
	taskRuns      TaskRunApplication
	scheduler     TaskScheduler
	events        TaskEventApplication
	verification  VerificationApplication
	targetRuntime TargetRuntime
	preview       PreviewApplication

	eventPollInterval      time.Duration
	eventHeartbeatInterval time.Duration
	eventReplayLimit       int
}

// NewServer 校验依赖、注册路由并返回 HTTP handler。
//
// 实现要点：
//  1. dependencies.Application 为 nil 时返回 nil, ErrInvalidDependencies。
//  2. 创建 http.NewServeMux 和 Server；不要启动监听端口或 goroutine。
//  3. 使用 Go 1.22 路由模式 `GET `+PathHealth 注册 handleHealth。
//  4. 返回构造完成的 Server。
func NewServer(dependencies Dependencies) (*Server, error) {
	if dependencies.Application == nil {
		return nil, ErrInvalidDependencies
	}
	if dependencies.EventPollInterval < 0 ||
		dependencies.EventHeartbeatInterval < 0 ||
		dependencies.EventReplayLimit < 0 {
		return nil, ErrInvalidDependencies
	}
	pollInterval := dependencies.EventPollInterval
	if pollInterval == 0 {
		pollInterval = defaultEventPollInterval
	}
	heartbeatInterval := dependencies.EventHeartbeatInterval
	if heartbeatInterval == 0 {
		heartbeatInterval = defaultEventHeartbeatInterval
	}
	replayLimit := dependencies.EventReplayLimit
	if replayLimit == 0 {
		replayLimit = defaultEventReplayLimit
	}

	mux := http.NewServeMux()
	server := &Server{
		mux:           mux,
		application:   dependencies.Application,
		planning:      dependencies.Planning,
		taskRuns:      dependencies.TaskRuns,
		scheduler:     dependencies.Scheduler,
		events:        dependencies.Events,
		verification:  dependencies.Verification,
		targetRuntime: dependencies.TargetRuntime,
		preview:       dependencies.Preview,

		eventPollInterval:      pollInterval,
		eventHeartbeatInterval: heartbeatInterval,
		eventReplayLimit:       replayLimit,
	}

	mux.HandleFunc(http.MethodGet+" "+PathHealth, server.handleHealth)
	mux.HandleFunc(http.MethodPost+" "+PathRequirements, server.handleCreateRequirement)
	mux.HandleFunc(http.MethodGet+" "+PathRequirements+"/{id}", server.handleGetRequirement)
	if server.planning != nil {
		mux.HandleFunc(http.MethodGet+" "+PathRequirements+"/{id}/planning", server.handleGetPlanning)
		mux.HandleFunc(http.MethodPost+" "+PathRequirements+"/{id}/grilling/confirm", server.handleConfirmGrilling)
		mux.HandleFunc(http.MethodPost+" "+PathRequirements+"/{id}/specs/generate", server.handleGenerateSpec)
		mux.HandleFunc(http.MethodPost+" "+PathRequirements+"/{id}/specs/{versionId}/confirm", server.handleConfirmSpec)
		mux.HandleFunc(http.MethodPost+" "+PathRequirements+"/{id}/tickets/generate", server.handleGenerateTickets)
		mux.HandleFunc(http.MethodPost+" "+PathRequirements+"/{id}/tickets/{versionId}/confirm", server.handleConfirmTickets)
	}
	// TaskRuns 与 Scheduler 在渐进迁移期间允许为空，现有 Requirement HTTP 仍可独立测试。
	if server.taskRuns != nil {
		mux.HandleFunc(http.MethodGet+" "+PathTaskRuns+"/{id}", server.handleGetTaskRun)
		mux.HandleFunc(http.MethodPost+" "+PathTaskRuns+"/{id}/cancel", server.handleCancelTaskRun)
	}
	if server.scheduler != nil {
		mux.HandleFunc(http.MethodPost+" "+PathTaskRuns, server.handleReconcileTaskRuns)
	}
	if server.events != nil {
		mux.HandleFunc(http.MethodGet+" "+PathEvents, server.handleEvents)
	}
	if server.verification != nil {
		mux.HandleFunc(
			http.MethodPost+" "+PathRequirements+"/{id}/verify",
			server.handleVerifyRequirement,
		)
		mux.HandleFunc(
			http.MethodGet+" "+PathRequirements+"/{id}/verification",
			server.handleGetLatestVerification,
		)
	}
	if server.targetRuntime != nil {
		mux.HandleFunc(http.MethodGet+" "+PathTargetRuntime, server.handleGetTargetRuntime)
		mux.HandleFunc(http.MethodPost+" "+PathTargetRuntime+"/start", server.handleStartTargetRuntime)
		mux.HandleFunc(http.MethodPost+" "+PathTargetRuntime+"/stop", server.handleStopTargetRuntime)
	}

	return server, nil
}

// ServeHTTP 让 Server 满足 http.Handler。
//
// 实现要点：只把请求转交给 s.mux.ServeHTTP；不要在这里编写具体 endpoint 逻辑。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// handleHealth 返回轻量进程存活信息。
//
// 实现要点：
//  1. 设置 Content-Type 为 `application/json; charset=utf-8`，必须在 WriteHeader 前完成。
//  2. 返回 200 OK。
//  3. 使用 json.NewEncoder(w).Encode 写入 HealthResponse{Status: "ok"}。
//  4. 健康检查不能调用 Application、Store 或其它外部资源。
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	health := HealthResponse{Status: "ok"}
	if err := json.NewEncoder(w).Encode(health); err != nil {
		// 这里不返回错误给调用方，因为健康检查本身就是为了判断服务是否可用。
		// 但可以在日志中记录编码失败的情况。
		log.Printf("failed to encode health response: %v", err)
	}
}

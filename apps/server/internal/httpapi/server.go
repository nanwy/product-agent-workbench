package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
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

// Dependencies 是 HTTP Server 启动时必须具备的依赖。
type Dependencies struct {
	Application RequirementApplication
}

// Server 把 HTTP 协议翻译为应用用例调用。
//
// Server 只负责路由、JSON、状态码和 header；输入规则、状态迁移与持久化都不属于这里。
// mux 和 application 在构造后只读，因此 Server 可以被 net/http 并发调用。
type Server struct {
	mux         *http.ServeMux
	application RequirementApplication
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

	mux := http.NewServeMux()
	server := &Server{
		mux:         mux,
		application: dependencies.Application,
	}

	mux.HandleFunc(http.MethodGet+" "+PathHealth, server.handleHealth)
	mux.HandleFunc(http.MethodPost+" "+PathRequirements, server.handleCreateRequirement)
	mux.HandleFunc(http.MethodGet+" "+PathRequirements+"/{id}", server.handleGetRequirement)

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

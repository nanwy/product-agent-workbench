package httpapi

import "product-agent-workbench/internal/domain"

// 路径常量先固定浏览器与 Go 之间的语言，具体 handler 等领域用例稳定后再实现。
const (
	PathHealth       = "/api/v1/health"
	PathRequirements = "/api/v1/requirements"
	PathTaskRuns     = "/api/v1/task-runs"
	PathEvents       = "/api/v1/events"
)

// HealthResponse 是健康检查成功时返回的稳定 JSON 结构。
//
// 第一版只表达进程已经启动并能处理 HTTP。数据库连通性、Codex 可用性等属于 readiness
// 或环境预检，不要把昂贵检查塞进每一次轻量健康探测。
type HealthResponse struct {
	Status string `json:"status"`
}

// CreateRequirementRequest 是 POST /requirements 接受的 JSON。
//
// 不包含 ID、Stage 和 Revision：这些字段由 Application 创建，不能由浏览器伪造。
type CreateRequirementRequest struct {
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
}

// RequirementResponse 是 Requirement endpoint 返回的稳定 JSON 视图。
//
// 不直接把领域结构体当作 HTTP 合约，避免以后领域字段变化意外破坏前端协议。
type RequirementResponse struct {
	ID        string                  `json:"id"`
	ProjectID string                  `json:"projectId"`
	Title     string                  `json:"title"`
	Stage     domain.RequirementStage `json:"stage"`
	Revision  int64                   `json:"revision"`
}

// ErrorResponse 是所有 HTTP 错误使用的稳定 JSON 结构。
//
// Code 给前端程序判断，Message 给本地用户阅读；不要把原始 SQL、文件路径或秘密直接放入
// Message。RequestID 留给后续日志关联，第一阶段可以为空。
type ErrorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

// MutationMeta 是所有写请求共有的并发与幂等信息。
type MutationMeta struct {
	IdempotencyKey   string `json:"idempotencyKey"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

// newRequirementResponse 把领域事实显式映射为传输对象。
//
// 实现要点：逐字段复制 ID、ProjectID、Title、Stage 和 Revision。不要返回领域对象本身，
// 也不要在映射函数中修改任何业务值。
func newRequirementResponse(requirement domain.Requirement) RequirementResponse {
	return RequirementResponse{}
}

// 当前学习顺序：先完成 server.go 的 GET /health，再实现 Requirement 创建和读取；
// 不要提前写 SSE。

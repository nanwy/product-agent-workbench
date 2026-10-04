package httpapi

// 路径常量先固定浏览器与 Go 之间的语言，具体 handler 等领域用例稳定后再实现。
const (
	PathHealth       = "/api/v1/health"
	PathRequirements = "/api/v1/requirements"
	PathTaskRuns     = "/api/v1/task-runs"
	PathEvents       = "/api/v1/events"
)

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

// TODO(学习者):领域状态机和内存 Store 完成后，新增 server.go。
// 先用 httptest 实现 GET /health，再实现 Requirement 的创建和读取；不要先写 SSE。

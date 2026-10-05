package httpapi

import (
	"time"

	"product-agent-workbench/internal/domain"
)

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

// EnqueueTaskRunRequest 是浏览器请求执行一张 Ticket 的稳定 JSON 合约。
type EnqueueTaskRunRequest struct {
	RequirementID string `json:"requirementId"`
	TicketID      string `json:"ticketId"`
	Attempt       int    `json:"attempt"`
}

// TaskResultResponse 是成功 TaskRun 暴露给浏览器的结果视图。
type TaskResultResponse struct {
	Summary       string `json:"summary"`
	CodexThreadID string `json:"codexThreadId,omitempty"`
	WorktreePath  string `json:"worktreePath,omitempty"`
}

// TaskRunResponse 是 TaskRun endpoint 的稳定 JSON 视图。
type TaskRunResponse struct {
	ID            string              `json:"id"`
	RequirementID string              `json:"requirementId"`
	TicketID      string              `json:"ticketId"`
	Attempt       int                 `json:"attempt"`
	Status        domain.TaskStatus   `json:"status"`
	CreatedAt     time.Time           `json:"createdAt"`
	ClaimedAt     *time.Time          `json:"claimedAt,omitempty"`
	StartedAt     *time.Time          `json:"startedAt,omitempty"`
	FinishedAt    *time.Time          `json:"finishedAt,omitempty"`
	Result        *TaskResultResponse `json:"result,omitempty"`
	FailureReason string              `json:"failureReason,omitempty"`
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
	return RequirementResponse{
		ID:        requirement.ID,
		ProjectID: requirement.ProjectID,
		Title:     requirement.Title,
		Stage:     requirement.Stage,
		Revision:  requirement.Revision,
	}
}

// newTaskRunResponse 显式把领域实体映射为 HTTP DTO。
//
// 实现时逐字段复制；Result 非 nil 时创建新的 TaskResultResponse，不能把领域结构体直接
// 当作 JSON 合约。时间指针只读使用，不在映射函数中改写。
func newTaskRunResponse(run domain.TaskRun) TaskRunResponse {
	response := TaskRunResponse{
		ID:            run.ID,
		RequirementID: run.RequirementID,
		TicketID:      run.TicketID,
		Attempt:       run.Attempt,
		Status:        run.Status,
		CreatedAt:     run.CreatedAt,
		ClaimedAt:     run.ClaimedAt,
		StartedAt:     run.StartedAt,
		FinishedAt:    run.FinishedAt,
		FailureReason: run.FailureReason,
	}
	if run.Result != nil {
		response.Result = &TaskResultResponse{
			Summary:       run.Result.Summary,
			CodexThreadID: run.Result.CodexThreadID,
			WorktreePath:  run.Result.WorktreePath,
		}
	}
	return response
}

// Requirement 与 TaskRun 普通 JSON endpoint 已完成；SSE 留到持久化 Adapter 稳定之后。

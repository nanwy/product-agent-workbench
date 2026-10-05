package httpapi

import (
	"errors"
	"net/http"
)

// errTaskRunHTTPNotImplemented 只用于 TaskRun HTTP 学习脚手架。
// 三组 handler 测试通过后删除。
var errTaskRunHTTPNotImplemented = errors.New("TaskRun HTTP 尚未实现")

// handleEnqueueTaskRun 处理 POST /api/v1/task-runs。
//
// 对应原 TypeScript 浏览器启动执行时创建后台执行事实的部分。实现顺序：严格解码请求、
// 映射 EnqueueTaskRunInput、传递 r.Context()、统一处理错误、返回 201 和 TaskRunResponse。
func (s *Server) handleEnqueueTaskRun(w http.ResponseWriter, r *http.Request) {
	writeError(w, errTaskRunHTTPNotImplemented)
}

// handleGetTaskRun 处理 GET /api/v1/task-runs/{id}。
//
// 从 r.PathValue("id") 取值并交给 Application；handler 不直接读取 Store。
func (s *Server) handleGetTaskRun(w http.ResponseWriter, r *http.Request) {
	writeError(w, errTaskRunHTTPNotImplemented)
}

// handleCancelTaskRun 处理 POST /api/v1/task-runs/{id}/cancel。
//
// 对应原 `cancelRequirementExecution` 的浏览器动作入口，但这里只请求保存取消事实；真正
// interrupt Codex 由 Worker 观察到取消后通过 context 完成。
func (s *Server) handleCancelTaskRun(w http.ResponseWriter, r *http.Request) {
	writeError(w, errTaskRunHTTPNotImplemented)
}

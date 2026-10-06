package httpapi

import (
	"net/http"

	"product-agent-workbench/internal/app"
)

// handleReconcileTaskRuns 处理 POST /api/v1/task-runs。
//
// 浏览器只提交已确认版本的身份；具体执行票和 attempt 由 Scheduler 决定，不能从请求体
// 直接构造 TaskRun。
func (s *Server) handleReconcileTaskRuns(w http.ResponseWriter, r *http.Request) {
	var request EnqueueTaskRunRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, err)
		return
	}

	result, err := s.scheduler.Reconcile(r.Context(), request.RequirementID, request.TicketVersionID)
	if err != nil {
		writeError(w, err)
		return
	}

	response := ReconcileTaskRunsResponse{
		TicketVersionID: result.TicketVersionID,
		Enqueued:        make([]TaskRunResponse, 0, len(result.Enqueued)),
	}
	for _, run := range result.Enqueued {
		response.Enqueued = append(response.Enqueued, newTaskRunResponse(run))
	}
	logWriteError(writeJSON(w, http.StatusCreated, response))
}

// handleGetTaskRun 处理 GET /api/v1/task-runs/{id}。
//
// 从 r.PathValue("id") 取值并交给 Application；handler 不直接读取 Store。
func (s *Server) handleGetTaskRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := s.taskRuns.GetTaskRun(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	response := newTaskRunResponse(run)
	logWriteError(writeJSON(w, http.StatusOK, response))
}

// handleCancelTaskRun 处理 POST /api/v1/task-runs/{id}/cancel。
//
// 对应原 `cancelRequirementExecution` 的浏览器动作入口，但这里只请求保存取消事实；真正
// interrupt Codex 由 Worker 观察到取消后通过 context 完成。
func (s *Server) handleCancelTaskRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := s.taskRuns.CancelTaskRun(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	response := newTaskRunResponse(run)
	logWriteError(writeJSON(w, http.StatusOK, response))
}

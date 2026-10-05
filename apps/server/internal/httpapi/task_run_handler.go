package httpapi

import (
	"net/http"

	"product-agent-workbench/internal/app"
)

// handleEnqueueTaskRun 处理 POST /api/v1/task-runs。
//
// 对应原 TypeScript 浏览器启动执行时创建后台执行事实的部分。实现顺序：严格解码请求、
// 映射 EnqueueTaskRunInput、传递 r.Context()、统一处理错误、返回 201 和 TaskRunResponse。
func (s *Server) handleEnqueueTaskRun(w http.ResponseWriter, r *http.Request) {
	var request EnqueueTaskRunRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, err)
		return
	}

	// HTTP DTO 只负责描述线上协议；这里显式转换，Application 不依赖 httpapi 包。
	input := app.EnqueueTaskRunInput{
		RequirementID: request.RequirementID,
		TicketID:      request.TicketID,
		Attempt:       request.Attempt,
	}
	run, err := s.taskRuns.EnqueueTaskRun(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}

	response := newTaskRunResponse(run)
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

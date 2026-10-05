package httpapi

import (
	"log"
	"net/http"

	"product-agent-workbench/internal/app"
)

// handleCreateRequirement 处理 POST /api/v1/requirements。
//
// 实现要点：
//  1. 用 decodeJSON 解析 CreateRequirementRequest；失败时 writeError 后立即 return。
//  2. 只把 ProjectID、Title 转成 app.CreateRequirementInput。
//  3. 把 r.Context() 原样传给 Application.CreateRequirement，不能换成 Background。
//  4. Application 失败时统一 writeError。
//  5. 成功时把领域对象转成 RequirementResponse，并以 201 Created 写回。
func (s *Server) handleCreateRequirement(w http.ResponseWriter, r *http.Request) {
	var request CreateRequirementRequest
	err := decodeJSON(w, r, &request)
	if err != nil {
		writeError(w, err)
		return
	}

	input := app.CreateRequirementInput{
		ProjectID: request.ProjectID,
		Title:     request.Title,
	}

	requirement, err := s.application.CreateRequirement(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}

	response := newRequirementResponse(requirement)

	logWriteError(writeJSON(w, http.StatusCreated, response))
}

// handleGetRequirement 处理 GET /api/v1/requirements/{id}。
//
// 实现要点：
//  1. 使用 r.PathValue("id") 读取路径参数，不要自己切割 URL 字符串。
//  2. 把 r.Context() 和 ID 原样交给 Application.GetRequirement。
//  3. Application 错误统一交给 writeError。
//  4. 成功时返回 200 OK 和 RequirementResponse。
func (s *Server) handleGetRequirement(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	requirement, err := s.application.GetRequirement(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	response := newRequirementResponse(requirement)

	logWriteError(writeJSON(w, http.StatusOK, response))
}

// logWriteError 记录响应写入失败。
//
// 第一版使用标准日志；后续加入结构化 logger 依赖后再替换。这里不能再次写响应，因为 header
// 可能已经提交，二次写入只会产生损坏的 body。
func logWriteError(err error) {
	if err != nil {
		log.Printf("failed to write JSON response: %v", err)
	}
}

package httpapi

import (
	"net/http"

	"product-agent-workbench/internal/app"
)

// handleVerifyRequirement 只接收 TicketVersion 身份，不接收命令、路径或“通过”结论。
//
// 前端只能表达“请验证这个已确认版本”。真正的命令来自服务端配置，Worktree 和 revision
// 来自完成的 TaskRun；这和前端不能自己提交 `isAdmin: true` 是同一种信任边界。
func (s *Server) handleVerifyRequirement(w http.ResponseWriter, r *http.Request) {
	var request VerifyRequirementRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, err)
		return
	}

	result, err := s.verification.VerifyRequirement(r.Context(), app.VerifyRequirementInput{
		RequirementID:   r.PathValue("id"),
		TicketVersionID: request.TicketVersionID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response := VerifyRequirementResponse{
		Requirement: newRequirementResponse(result.Requirement),
		Report:      newVerificationReportResponse(result.Report),
		Deliverable: result.Deliverable,
	}
	logWriteError(writeJSON(w, http.StatusOK, response))
}

func (s *Server) handleGetLatestVerification(w http.ResponseWriter, r *http.Request) {
	report, err := s.verification.GetLatestVerification(
		r.Context(),
		r.PathValue("id"),
		r.URL.Query().Get("ticketVersionId"),
	)
	if err != nil {
		writeError(w, err)
		return
	}
	logWriteError(writeJSON(w, http.StatusOK, newVerificationReportResponse(report)))
}

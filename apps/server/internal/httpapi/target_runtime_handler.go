package httpapi

import "net/http"

func (server *Server) handleGetTargetRuntime(w http.ResponseWriter, _ *http.Request) {
	logWriteError(writeJSON(w, http.StatusOK, TargetRuntimeResponse{
		Target:  server.targetRuntime.Target(),
		Process: server.targetRuntime.Snapshot(),
	}))
}

// start/stop 请求没有 body：浏览器只能表达意图，实际命令始终来自服务端受信任配置。
func (server *Server) handleStartTargetRuntime(w http.ResponseWriter, request *http.Request) {
	var input StartTargetRuntimeRequest
	if err := decodeJSON(w, request, &input); err != nil {
		writeError(w, err)
		return
	}
	workspacePath := ""
	if input.RequirementID != "" || input.TicketVersionID != "" {
		if server.preview == nil {
			writeError(w, ErrInvalidDependencies)
			return
		}
		var err error
		workspacePath, err = server.preview.ResolvePreviewWorkspace(
			request.Context(), input.RequirementID, input.TicketVersionID,
		)
		if err != nil {
			writeError(w, err)
			return
		}
	}
	snapshot, err := server.targetRuntime.Start(request.Context(), workspacePath)
	if err != nil {
		writeError(w, err)
		return
	}
	logWriteError(writeJSON(w, http.StatusOK, TargetRuntimeResponse{
		Target:  server.targetRuntime.Target(),
		Process: snapshot,
	}))
}

func (server *Server) handleStopTargetRuntime(w http.ResponseWriter, request *http.Request) {
	snapshot, err := server.targetRuntime.Stop(request.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	logWriteError(writeJSON(w, http.StatusOK, TargetRuntimeResponse{
		Target:  server.targetRuntime.Target(),
		Process: snapshot,
	}))
}

package httpapi

import (
	"net/http"

	"product-agent-workbench/internal/app"
)

func (s *Server) handleGetPlanning(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.planning.GetPlanning(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	logWriteError(writeJSON(w, http.StatusOK, newPlanningResponse(snapshot)))
}

func (s *Server) handleConfirmGrilling(w http.ResponseWriter, r *http.Request) {
	var request ConfirmGrillingRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, err)
		return
	}
	snapshot, err := s.planning.ConfirmGrilling(r.Context(), app.ConfirmGrillingInput{
		RequirementID:    r.PathValue("id"),
		ExpectedRevision: request.ExpectedRevision,
		Summary:          request.Summary,
		DesiredOutcome:   request.DesiredOutcome,
		ChangeBoundary:   request.ChangeBoundary,
		AcceptanceSignal: request.AcceptanceSignal,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	logWriteError(writeJSON(w, http.StatusOK, newPlanningResponse(snapshot)))
}

func (s *Server) handleGenerateSpec(w http.ResponseWriter, r *http.Request) {
	var request GenerateSpecRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, err)
		return
	}
	snapshot, err := s.planning.GenerateSpec(r.Context(), app.GenerateSpecInput{
		RequirementID:    r.PathValue("id"),
		ExpectedRevision: request.ExpectedRevision,
		Instruction:      request.Instruction,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	logWriteError(writeJSON(w, http.StatusCreated, newPlanningResponse(snapshot)))
}

func (s *Server) handleConfirmSpec(w http.ResponseWriter, r *http.Request) {
	var request ConfirmSpecRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, err)
		return
	}
	snapshot, err := s.planning.ConfirmSpec(r.Context(), app.ConfirmSpecInput{
		RequirementID:    r.PathValue("id"),
		VersionID:        r.PathValue("versionId"),
		ExpectedRevision: request.ExpectedRevision,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	logWriteError(writeJSON(w, http.StatusOK, newPlanningResponse(snapshot)))
}

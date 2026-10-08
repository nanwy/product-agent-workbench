package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/httpapi"
	"product-agent-workbench/internal/verification"
)

func TestServer_VerifyRequirementOnlyAcceptsTicketVersionIdentity(t *testing.T) {
	application := &recordingVerificationApplication{}
	server, err := httpapi.NewServer(httpapi.Dependencies{
		Application:  stubApplication{},
		Verification: application,
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	body := bytes.NewBufferString(`{"ticketVersionId":"tickets-v1"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/requirements/req-1/verify", body)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	if application.input.RequirementID != "req-1" || application.input.TicketVersionID != "tickets-v1" {
		t.Fatalf("VerifyRequirement() input = %#v", application.input)
	}
	var got httpapi.VerifyRequirementResponse
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !got.Deliverable || got.Requirement.Stage != domain.RequirementStageAcceptance ||
		got.Report.Status != verification.ReportStatusPassed || len(got.Report.Records) != 1 {
		t.Fatalf("response = %#v", got)
	}

	// 浏览器不能把可执行文件塞进请求；未知字段必须在到达 Application 前被拒绝。
	malicious := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/requirements/req-1/verify",
		bytes.NewBufferString(`{"ticketVersionId":"tickets-v1","command":"rm"}`),
	)
	maliciousResponse := httptest.NewRecorder()
	server.ServeHTTP(maliciousResponse, malicious)
	if maliciousResponse.Code != http.StatusBadRequest {
		t.Fatalf("unknown command field status = %d, want 400", maliciousResponse.Code)
	}

	latestRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/requirements/req-1/verification?ticketVersionId=tickets-v1",
		nil,
	)
	latestResponse := httptest.NewRecorder()
	server.ServeHTTP(latestResponse, latestRequest)
	if latestResponse.Code != http.StatusOK {
		t.Fatalf("latest status = %d, body = %q", latestResponse.Code, latestResponse.Body.String())
	}
}

type recordingVerificationApplication struct {
	input app.VerifyRequirementInput
}

func (a *recordingVerificationApplication) VerifyRequirement(
	_ context.Context,
	input app.VerifyRequirementInput,
) (app.VerifyRequirementResult, error) {
	a.input = input
	now := time.Unix(1, 0).UTC()
	return app.VerifyRequirementResult{
		Requirement: domain.Requirement{
			ID: "req-1", ProjectID: "project-1", Title: "验证",
			Stage: domain.RequirementStageAcceptance, Revision: 2,
		},
		Report: verification.Report{
			ID: "verification-1", RequirementID: "req-1", TicketVersionID: "tickets-v1",
			CandidateRevision: "revision-1", Status: verification.ReportStatusPassed,
			Matrix: verification.Matrix{Records: []verification.Evidence{{
				ID: "typecheck", Label: "Typecheck", Status: verification.StatusVerified,
				Required: true, Source: verification.SourceCommand, StartedAt: now, CompletedAt: now,
			}}},
			StartedAt: now, CompletedAt: now,
		},
		Deliverable: true,
	}, nil
}

func (a *recordingVerificationApplication) GetLatestVerification(
	_ context.Context,
	requirementID string,
	ticketVersionID string,
) (verification.Report, error) {
	result, err := a.VerifyRequirement(context.Background(), app.VerifyRequirementInput{
		RequirementID: requirementID, TicketVersionID: ticketVersionID,
	})
	return result.Report, err
}

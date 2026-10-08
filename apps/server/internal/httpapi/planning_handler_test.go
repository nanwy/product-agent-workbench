package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/httpapi"
)

type planningApplicationStub struct {
	confirmGrilling func(context.Context, app.ConfirmGrillingInput) (app.PlanningSnapshot, error)
	generateSpec    func(context.Context, app.GenerateSpecInput) (app.PlanningSnapshot, error)
	confirmSpec     func(context.Context, app.ConfirmSpecInput) (app.PlanningSnapshot, error)
	getPlanning     func(context.Context, string) (app.PlanningSnapshot, error)
}

func (stub planningApplicationStub) ConfirmGrilling(ctx context.Context, input app.ConfirmGrillingInput) (app.PlanningSnapshot, error) {
	return stub.confirmGrilling(ctx, input)
}
func (stub planningApplicationStub) GenerateSpec(ctx context.Context, input app.GenerateSpecInput) (app.PlanningSnapshot, error) {
	return stub.generateSpec(ctx, input)
}
func (stub planningApplicationStub) ConfirmSpec(ctx context.Context, input app.ConfirmSpecInput) (app.PlanningSnapshot, error) {
	return stub.confirmSpec(ctx, input)
}
func (stub planningApplicationStub) GetPlanning(ctx context.Context, id string) (app.PlanningSnapshot, error) {
	return stub.getPlanning(ctx, id)
}

func TestServer_PlanningRoutesPassIdentityAndRevision(t *testing.T) {
	var got app.ConfirmGrillingInput
	planning := planningApplicationStub{
		confirmGrilling: func(_ context.Context, input app.ConfirmGrillingInput) (app.PlanningSnapshot, error) {
			got = input
			return app.PlanningSnapshot{Requirement: domain.Requirement{
				ID: input.RequirementID, ProjectID: "project-1", Title: "需求",
				Stage: domain.RequirementStageSpec, Revision: input.ExpectedRevision + 1,
			}, SpecVersions: []domain.SpecVersion{}}, nil
		},
	}
	server, err := httpapi.NewServer(httpapi.Dependencies{
		Application: fakeRequirementApplication{},
		Planning:    planning,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		httpapi.PathRequirements+"/req-1/grilling/confirm",
		strings.NewReader(`{"expectedRevision":1,"summary":"摘要","desiredOutcome":"结果","changeBoundary":"apps/web","acceptanceSignal":"页面可见"}`),
	)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if got.RequirementID != "req-1" || got.ExpectedRevision != 1 || got.ChangeBoundary != "apps/web" {
		t.Fatalf("ConfirmGrilling input=%#v", got)
	}
}

package app_test

import (
	"context"
	"testing"
	"time"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store/memory"
)

type specGeneratorStub struct{}

func (specGeneratorStub) Generate(context.Context, app.SpecGenerationInput) (domain.SpecDocument, error) {
	return domain.SpecDocument{
		Problem: "需求尚未形成规格", Solution: "生成版本化 Spec", UserStories: []string{"用户可以确认"},
		ImplementationDecisions: []string{"使用 Go"}, TestingDecisions: []string{"检查阶段"},
		OutOfScope: []string{"Ticket"}, FurtherNotes: []string{"21B"},
	}, nil
}

func TestPlanningFlowMovesRequirementFromGrillingToTickets(t *testing.T) {
	ids := []string{"req-1", "version-1"}
	application, err := app.New(app.Dependencies{
		Store: memory.New(),
		NewID: func() string {
			id := ids[0]
			ids = ids[1:]
			return id
		},
		Now:           func() time.Time { return time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC) },
		SpecGenerator: specGeneratorStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	requirement, err := application.CreateRequirement(ctx, app.CreateRequirementInput{ProjectID: "project-1", Title: "迁移规划"})
	if err != nil {
		t.Fatal(err)
	}
	planning, err := application.ConfirmGrilling(ctx, app.ConfirmGrillingInput{
		RequirementID: requirement.ID, ExpectedRevision: requirement.Revision,
		Summary: "共同理解", DesiredOutcome: "完成 Go 迁移", ChangeBoundary: "apps/server",
		AcceptanceSignal: "Spec 可确认",
	})
	if err != nil || planning.Requirement.Stage != domain.RequirementStageSpec || planning.Requirement.Revision != 2 {
		t.Fatalf("ConfirmGrilling() = %#v, error=%v", planning, err)
	}
	planning, err = application.GenerateSpec(ctx, app.GenerateSpecInput{
		RequirementID: requirement.ID, ExpectedRevision: 2,
	})
	if err != nil || len(planning.SpecVersions) != 1 || planning.Requirement.Revision != 3 {
		t.Fatalf("GenerateSpec() = %#v, error=%v", planning, err)
	}
	planning, err = application.ConfirmSpec(ctx, app.ConfirmSpecInput{
		RequirementID: requirement.ID, VersionID: planning.SpecVersions[0].ID, ExpectedRevision: 3,
	})
	if err != nil || planning.Requirement.Stage != domain.RequirementStageTickets || planning.Requirement.Revision != 4 {
		t.Fatalf("ConfirmSpec() = %#v, error=%v", planning, err)
	}
}

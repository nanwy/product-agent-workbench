package store_test

import (
	"context"
	"testing"
	"time"

	"product-agent-workbench/internal/domain"
)

func runPlanningStoreContract(t *testing.T, factory storeFactory) {
	t.Helper()
	t.Run("共同理解与 Spec 状态原子持久化", func(t *testing.T) {
		ctx := context.Background()
		subject := factory(t)
		requirement := requirementFixture()
		if err := subject.CreateRequirement(ctx, requirement); err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
		understanding := domain.GrillingUnderstanding{
			RequirementID: requirement.ID, Summary: "统一目标", DesiredOutcome: "页面可操作",
			ChangeBoundary: "apps/web", AcceptanceSignal: "用户可以确认 Spec", ConfirmedAt: now,
		}
		requirement.Stage = domain.RequirementStageSpec
		revision, err := subject.ConfirmGrilling(ctx, requirement, 1, understanding)
		if err != nil || revision != 2 {
			t.Fatalf("ConfirmGrilling() = (%d, %v), want (2, nil)", revision, err)
		}
		gotUnderstanding, err := subject.GetGrillingUnderstanding(ctx, requirement.ID)
		if err != nil || gotUnderstanding.Summary != understanding.Summary {
			t.Fatalf("understanding = %#v, error=%v", gotUnderstanding, err)
		}

		requirement.Revision = revision
		version := domain.SpecVersion{
			ID: "spec-1", RequirementID: requirement.ID, Version: 1,
			Status: domain.SpecVersionDraft, CreatedAt: now,
			Document: planningSpecFixture(),
		}
		revision, err = subject.CreateSpecVersion(ctx, requirement, revision, version)
		if err != nil || revision != 3 {
			t.Fatalf("CreateSpecVersion() = (%d, %v), want (3, nil)", revision, err)
		}
		requirement.Revision = revision
		requirement.Stage = domain.RequirementStageTickets
		revision, err = subject.ConfirmSpecVersion(ctx, requirement, revision, version.ID, now.Add(time.Minute))
		if err != nil || revision != 4 {
			t.Fatalf("ConfirmSpecVersion() = (%d, %v), want (4, nil)", revision, err)
		}
		versions, err := subject.ListSpecVersions(ctx, requirement.ID)
		if err != nil || len(versions) != 1 || versions[0].Status != domain.SpecVersionConfirmed || versions[0].ConfirmedAt == nil {
			t.Fatalf("versions = %#v, error=%v", versions, err)
		}
		stored, err := subject.GetRequirement(ctx, requirement.ID)
		if err != nil || stored.Stage != domain.RequirementStageTickets || stored.Revision != 4 {
			t.Fatalf("requirement = %#v, error=%v", stored, err)
		}
	})
}

func planningSpecFixture() domain.SpecDocument {
	return domain.SpecDocument{
		Problem: "问题", Solution: "方案", UserStories: []string{"故事"},
		ImplementationDecisions: []string{"实现"}, TestingDecisions: []string{"测试"},
		OutOfScope: []string{"范围外"}, FurtherNotes: []string{"备注"},
	}
}

package app_test

import (
	"context"
	"testing"
	"time"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/scheduler"
	"product-agent-workbench/internal/store/memory"
)

func TestApplicationResolvePreviewWorkspaceUsesCompletedCandidateFacts(t *testing.T) {
	storeAdapter := memory.New()
	if err := storeAdapter.CreateRequirement(context.Background(), domain.Requirement{
		ID: "req-1", ProjectID: "project-1", Title: "候选预览",
		Stage: domain.RequirementStageImplementation, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	completeVerificationTask(t, storeAdapter, "run-a", "ticket-a", "revision-a")
	completeVerificationTask(t, storeAdapter, "run-b", "ticket-b", "revision-b")
	application, err := app.New(app.Dependencies{
		Store: storeAdapter,
		Tickets: verificationTicketReaderStub{version: scheduler.TicketVersion{
			ID: "tickets-v1", RequirementID: "req-1",
			Tickets: []scheduler.Ticket{{ID: "ticket-a"}, {ID: "ticket-b"}},
		}},
		NewID: func() string { return "unused" },
		Now:   func() time.Time { return time.Unix(10, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	workspacePath, err := application.ResolvePreviewWorkspace(context.Background(), "req-1", "tickets-v1")
	if err != nil {
		t.Fatalf("ResolvePreviewWorkspace() error = %v", err)
	}
	if workspacePath != "/tmp/requirement-worktree" {
		t.Fatalf("workspacePath = %q", workspacePath)
	}
}

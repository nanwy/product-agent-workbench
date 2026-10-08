package app_test

import (
	"context"
	"testing"
	"time"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/scheduler"
	"product-agent-workbench/internal/store/memory"
	"product-agent-workbench/internal/verification"
)

func TestApplication_VerifyRequirementUsesMatrixAsAcceptanceGate(t *testing.T) {
	tests := []struct {
		name            string
		status          verification.Status
		wantStage       domain.RequirementStage
		wantRevision    int64
		wantDeliverable bool
	}{
		{
			name: "全部必要证据通过后进入 acceptance", status: verification.StatusVerified,
			wantStage: domain.RequirementStageAcceptance, wantRevision: 2, wantDeliverable: true,
		},
		{
			name: "验证失败时保留 completed Ticket 和 implementation 阶段", status: verification.StatusFailed,
			wantStage: domain.RequirementStageImplementation, wantRevision: 1, wantDeliverable: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			storeAdapter := memory.New()
			requirement := domain.Requirement{
				ID: "req-1", ProjectID: "project-1", Title: "验证候选",
				Stage: domain.RequirementStageImplementation, Revision: 1,
			}
			if err := storeAdapter.CreateRequirement(ctx, requirement); err != nil {
				t.Fatalf("CreateRequirement() error = %v", err)
			}
			completeVerificationTask(t, storeAdapter, "run-a", "ticket-a", "revision-a")
			completeVerificationTask(t, storeAdapter, "run-b", "ticket-b", "revision-b")

			verifier := &recordingDeliveryVerifier{report: verification.Report{
				ID: "verification-1", RequirementID: "req-1", TicketVersionID: "tickets-v1",
				Status: verification.ReportStatusBlocked,
				Matrix: verification.Matrix{Records: []verification.Evidence{{
					ID: "typecheck", Label: "Typecheck", Status: test.status, Required: true,
				}}},
			}}
			if test.status == verification.StatusVerified {
				verifier.report.Status = verification.ReportStatusPassed
			}
			application, err := app.New(app.Dependencies{
				Store: storeAdapter,
				Tickets: verificationTicketReaderStub{version: scheduler.TicketVersion{
					ID: "tickets-v1", RequirementID: "req-1",
					Tickets: []scheduler.Ticket{{ID: "ticket-a"}, {ID: "ticket-b"}},
				}},
				Verification: verifier,
				NewID:        func() string { return "unused" },
				Now:          func() time.Time { return time.Unix(10, 0).UTC() },
			})
			if err != nil {
				t.Fatalf("app.New() error = %v", err)
			}

			result, err := application.VerifyRequirement(ctx, app.VerifyRequirementInput{
				RequirementID: "req-1", TicketVersionID: "tickets-v1",
			})
			if err != nil {
				t.Fatalf("VerifyRequirement() error = %v", err)
			}
			if result.Deliverable != test.wantDeliverable || result.Requirement.Stage != test.wantStage ||
				result.Requirement.Revision != test.wantRevision {
				t.Fatalf("VerifyRequirement() = %#v", result)
			}
			if verifier.request.WorkspacePath != "/tmp/requirement-worktree" ||
				len(verifier.request.ExpectedRevisions) != 2 {
				t.Fatalf("Verify() request = %#v", verifier.request)
			}
			for _, id := range []string{"run-a", "run-b"} {
				run, getErr := storeAdapter.GetTask(ctx, id)
				if getErr != nil || run.Status != domain.TaskStatusCompleted {
					t.Fatalf("验证改变了 TaskRun %q: %#v, %v", id, run, getErr)
				}
			}
		})
	}
}

func completeVerificationTask(
	t *testing.T,
	storeAdapter *memory.Store,
	runID string,
	ticketID string,
	revision string,
) {
	t.Helper()
	ctx := context.Background()
	createdAt := time.Unix(1, 0).UTC()
	run, err := domain.NewTaskRun(domain.NewTaskRunInput{
		ID: runID, RequirementID: "req-1", TicketVersionID: "tickets-v1",
		TicketID: ticketID, Attempt: 1,
	}, createdAt)
	if err != nil {
		t.Fatalf("NewTaskRun() error = %v", err)
	}
	if err := storeAdapter.EnqueueTask(ctx, run); err != nil {
		t.Fatalf("EnqueueTask() error = %v", err)
	}
	claimed, err := storeAdapter.ClaimNextTask(ctx, createdAt.Add(time.Second))
	if err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	running, err := claimed.Start(createdAt.Add(2 * time.Second))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := storeAdapter.SaveTask(ctx, running, domain.TaskStatusClaimed); err != nil {
		t.Fatalf("SaveTask(running) error = %v", err)
	}
	completed, err := running.Complete(domain.TaskResult{
		Summary: "完成", WorktreePath: "/tmp/requirement-worktree", Revision: revision,
	}, createdAt.Add(3*time.Second))
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err := storeAdapter.SaveTask(ctx, completed, domain.TaskStatusRunning); err != nil {
		t.Fatalf("SaveTask(completed) error = %v", err)
	}
}

type verificationTicketReaderStub struct {
	version scheduler.TicketVersion
}

func (s verificationTicketReaderStub) ReadConfirmedTicketVersion(
	context.Context,
	string,
	string,
) (scheduler.TicketVersion, error) {
	return s.version, nil
}

type recordingDeliveryVerifier struct {
	request verification.VerifyRequest
	report  verification.Report
}

func (v *recordingDeliveryVerifier) Verify(
	_ context.Context,
	request verification.VerifyRequest,
) (verification.Report, error) {
	v.request = request
	return v.report, nil
}

func (v *recordingDeliveryVerifier) LatestReport(
	context.Context,
	string,
	string,
) (verification.Report, error) {
	return v.report, nil
}

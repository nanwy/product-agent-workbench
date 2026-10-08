package verification

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestServiceVerifyBuildsAndPersistsPassedReport(t *testing.T) {
	clock := sequenceClock(
		time.Unix(1, 0).UTC(),
		time.Unix(2, 0).UTC(),
		time.Unix(3, 0).UTC(),
	)
	repository := &recordingReportRepository{}
	executor := &recordingCommandExecutor{results: map[string]CommandResult{
		"typecheck": {
			ExitCode: 0, Termination: TerminationExit,
			StartedAt: time.Unix(1, 0).UTC(), CompletedAt: time.Unix(2, 0).UTC(),
		},
	}}
	service := newVerificationService(t, executor, candidateInspectorStub{
		candidate: Candidate{Revision: "revision-2"},
	}, repository, clock)

	report, err := service.Verify(context.Background(), VerifyRequest{
		RequirementID:     "req-1",
		TicketVersionID:   "tickets-v1",
		WorkspacePath:     "/tmp/managed-requirement",
		ExpectedRevisions: []string{"revision-1", "revision-2"},
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if report.Status != ReportStatusPassed || report.Matrix.BlocksDelivery() {
		t.Fatalf("Verify() report = %#v, want passed", report)
	}
	if len(report.Matrix.Records) != 2 || report.Matrix.Records[0].Source != SourceCandidate ||
		report.Matrix.Records[1].Status != StatusVerified {
		t.Fatalf("Verify() records = %#v", report.Matrix.Records)
	}
	if !reflect.DeepEqual(repository.saved, []Report{report}) {
		t.Fatalf("SaveReport() = %#v, want report", repository.saved)
	}
	if !reflect.DeepEqual(executor.commands, []string{"typecheck"}) {
		t.Fatalf("Execute() order = %#v", executor.commands)
	}
}

func TestServiceVerifyKeepsFailedAndNotRunSeparate(t *testing.T) {
	repository := &recordingReportRepository{}
	executor := &recordingCommandExecutor{results: map[string]CommandResult{
		"typecheck": {
			ExitCode: 1, Output: "type error", Termination: TerminationExit,
			StartedAt: time.Unix(1, 0).UTC(), CompletedAt: time.Unix(2, 0).UTC(),
		},
	}}
	service := newVerificationService(t, executor, candidateInspectorStub{
		err: errors.New("git unavailable"),
	}, repository, sequenceClock(time.Unix(1, 0).UTC(), time.Unix(2, 0).UTC(), time.Unix(3, 0).UTC()))

	report, err := service.Verify(context.Background(), VerifyRequest{
		RequirementID:     "req-1",
		TicketVersionID:   "tickets-v1",
		WorkspacePath:     "/tmp/managed-requirement",
		ExpectedRevisions: []string{"revision-1"},
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if report.Status != ReportStatusBlocked || !report.Matrix.BlocksDelivery() {
		t.Fatalf("Verify() report status = %q, want blocked", report.Status)
	}
	if report.Matrix.Records[0].Status != StatusNotRun || report.Matrix.Records[0].Reason == "" {
		t.Fatalf("candidate evidence = %#v, want not-run with reason", report.Matrix.Records[0])
	}
	if report.Matrix.Records[1].Status != StatusFailed || report.Matrix.Records[1].Reason != "" {
		t.Fatalf("command evidence = %#v, want failed", report.Matrix.Records[1])
	}
}

func TestServiceVerifyRunsBuildBeforeRuntimeChecksButKeepsDisplayOrder(t *testing.T) {
	now := time.Unix(1, 0).UTC()
	executor := &recordingCommandExecutor{results: map[string]CommandResult{
		"runtime-test": {ExitCode: 0, Termination: TerminationExit, StartedAt: now, CompletedAt: now},
		"build":        {ExitCode: 0, Termination: TerminationExit, StartedAt: now, CompletedAt: now},
	}}
	service, err := NewService(ServiceDependencies{
		Commands: []Command{
			{
				ID: "runtime-test", Label: "Runtime", Executable: "test", Timeout: time.Second,
				Environment: EnvironmentTest, ProxyPolicy: ProxyPolicyOmit,
				Required: true, RequiresRuntime: true,
			},
			{
				ID: "build", Label: "Build", Executable: "build", Timeout: time.Second,
				Environment: EnvironmentProduction, ProxyPolicy: ProxyPolicyOmit, Required: true,
			},
		},
		Executor: executor, Candidate: candidateInspectorStub{candidate: Candidate{Revision: "revision-1"}},
		Runtime: runtimeProbeStub{}, Reports: &recordingReportRepository{},
		NewID: func() string { return "verification-1" }, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	report, err := service.Verify(context.Background(), VerifyRequest{
		RequirementID: "req-1", TicketVersionID: "tickets-v1",
		WorkspacePath: "/tmp/worktree", ExpectedRevisions: []string{"revision-1"},
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !reflect.DeepEqual(executor.commands, []string{"build", "runtime-test"}) {
		t.Fatalf("execution order = %#v, want build before runtime", executor.commands)
	}
	if report.Matrix.Records[1].ID != "runtime-test" || report.Matrix.Records[2].ID != "build" {
		t.Fatalf("display order = %#v", report.Matrix.Records)
	}
}

func TestHostCommandExecutorClassifiesExitAndTimeout(t *testing.T) {
	executor := NewHostCommandExecutor()
	success := executor.Execute(context.Background(), t.TempDir(), Command{
		Executable:  os.Args[0],
		Args:        []string{"-test.run=^TestVerificationHelperProcess$"},
		Timeout:     10 * time.Second,
		Environment: Environment("verification-helper-success"),
		ProxyPolicy: ProxyPolicyOmit,
	})
	if success.Termination != TerminationExit || success.ExitCode != 0 || !strings.Contains(success.Output, "helper ok") {
		t.Fatalf("success result = %#v", success)
	}

	timedOut := executor.Execute(context.Background(), t.TempDir(), Command{
		Executable:  os.Args[0],
		Args:        []string{"-test.run=^TestVerificationHelperProcess$"},
		Timeout:     20 * time.Millisecond,
		Environment: Environment("verification-helper-timeout"),
		ProxyPolicy: ProxyPolicyOmit,
	})
	if timedOut.Termination != TerminationTimeout || timedOut.ExitCode != 124 {
		t.Fatalf("timeout result = %#v", timedOut)
	}
}

func TestVerificationHelperProcess(t *testing.T) {
	switch os.Getenv("NODE_ENV") {
	case "verification-helper-success":
		_, _ = os.Stdout.WriteString("helper ok")
	case "verification-helper-timeout":
		time.Sleep(time.Minute)
	}
}

func newVerificationService(
	t *testing.T,
	executor CommandExecutor,
	candidate CandidateInspector,
	repository ReportRepository,
	clock Clock,
) *Service {
	t.Helper()
	service, err := NewService(ServiceDependencies{
		Commands: []Command{{
			ID: "typecheck", Label: "Typecheck", Executable: "tool", Timeout: time.Second,
			Environment: EnvironmentTest, ProxyPolicy: ProxyPolicyOmit, Required: true,
		}},
		Executor: executor, Candidate: candidate, Reports: repository,
		NewID: func() string { return "verification-1" }, Now: clock,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

type recordingCommandExecutor struct {
	results  map[string]CommandResult
	commands []string
}

func (e *recordingCommandExecutor) Execute(
	_ context.Context,
	_ string,
	command Command,
) CommandResult {
	e.commands = append(e.commands, command.ID)
	return e.results[command.ID]
}

type candidateInspectorStub struct {
	candidate Candidate
	err       error
}

type runtimeProbeStub struct{ err error }

func (s runtimeProbeStub) Check(context.Context) error { return s.err }

func (s candidateInspectorStub) Inspect(context.Context, string, []string) (Candidate, error) {
	return s.candidate, s.err
}

type recordingReportRepository struct {
	saved []Report
}

func (r *recordingReportRepository) SaveReport(_ context.Context, report Report) error {
	r.saved = append(r.saved, report)
	return nil
}

func (r *recordingReportRepository) LatestReport(context.Context, string, string) (Report, error) {
	if len(r.saved) == 0 {
		return Report{}, ErrReportNotFound
	}
	return r.saved[len(r.saved)-1], nil
}

func sequenceClock(values ...time.Time) Clock {
	index := 0
	return func() time.Time {
		value := values[index]
		index++
		return value
	}
}

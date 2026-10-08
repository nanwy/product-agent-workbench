package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/scheduler"
)

func TestStoreReadsSchedulerAndRunnerProjections(t *testing.T) {
	root := t.TempDir()
	requirementID := "req-1"
	versionID := "tickets-v1"
	document := TicketVersionDocument{
		ID:                versionID,
		RequirementID:     requirementID,
		RepositoryRoot:    filepath.Join(t.TempDir(), "repository"),
		BaseRevision:      "0123456789012345678901234567890123456789",
		RequirementBranch: "workbench/req-1",
		Tickets: []TicketDocument{
			{
				ID:                 "ticket-a",
				BlockedBy:          []string{},
				Prompt:             "实现 A",
				ModificationScopes: []string{"apps/web"},
			},
			{
				ID:                 "ticket-b",
				BlockedBy:          []string{"ticket-a"},
				Prompt:             "实现 B",
				ModificationScopes: []string{"apps/server"},
			},
		},
	}
	writeDocument(t, root, document)

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	version, err := store.ReadConfirmedTicketVersion(context.Background(), requirementID, versionID)
	if err != nil {
		t.Fatalf("ReadConfirmedTicketVersion() error = %v", err)
	}
	if len(version.Tickets) != 2 || version.Tickets[1].BlockedBy[0] != "ticket-a" {
		t.Fatalf("TicketVersion = %#v", version)
	}

	facts, err := store.ReadExecutionFacts(context.Background(), domain.TaskRun{
		RequirementID:   requirementID,
		TicketVersionID: versionID,
		TicketID:        "ticket-b",
		Attempt:         2,
		CodexThreadID:   "thread-2",
	})
	if err != nil {
		t.Fatalf("ReadExecutionFacts() error = %v", err)
	}
	if facts.Prompt != "实现 B" ||
		facts.CodexThreadID != "thread-2" ||
		facts.Workspace.TicketBranch != "workbench/req-1/ticket-b/attempt-2" ||
		facts.Workspace.ModificationScopes[0] != "apps/server" {
		t.Fatalf("ExecutionFacts = %#v", facts)
	}
}

func TestStoreReportsMissingTicketVersion(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	_, err = store.ReadConfirmedTicketVersion(context.Background(), "req-1", "tickets-v1")
	if !errors.Is(err, scheduler.ErrTicketVersionNotFound) {
		t.Fatalf("missing TicketVersion error = %v, want ErrTicketVersionNotFound", err)
	}
}

func TestStorePublishesConfirmedTicketVersionIdempotently(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	confirmedAt := time.Now().UTC()
	version := domain.TicketVersion{
		ID: "tickets-v1", RequirementID: "req-1", SpecVersionID: "spec-v1",
		Version: 1, Status: domain.TicketVersionConfirmed,
		RepositoryRoot:    filepath.Join(t.TempDir(), "repository"),
		BaseRevision:      "0123456789012345678901234567890123456789",
		RequirementBranch: "workbench/requirements/req-1", ConfirmedAt: &confirmedAt,
		Tickets: []domain.Ticket{{
			ID: "TICKET-01", Title: "实现页面", BlockedBy: []string{}, Deliverable: "页面可用",
			AcceptanceCriteria: []string{"类型检查通过"}, ModificationScopes: []string{"apps/web"},
		}},
	}
	for range 2 {
		if err := store.PublishConfirmedTicketVersion(context.Background(), version); err != nil {
			t.Fatalf("PublishConfirmedTicketVersion() error = %v", err)
		}
	}
	facts, err := store.ReadExecutionFacts(context.Background(), domain.TaskRun{
		RequirementID: version.RequirementID, TicketVersionID: version.ID,
		TicketID: "TICKET-01", Attempt: 1,
	})
	if err != nil || facts.Prompt == "" || facts.Workspace.ModificationScopes[0] != "apps/web" {
		t.Fatalf("ExecutionFacts = %#v, error=%v", facts, err)
	}
}

func TestStoreRejectsPathTraversalAndUnknownFields(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if _, err := store.ReadConfirmedTicketVersion(context.Background(), "../req", "tickets-v1"); !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("path traversal error = %v, want ErrInvalidDocument", err)
	}

	directory := filepath.Join(store.root, "req-1")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"id":"tickets-v1","requirementId":"req-1","unknown":true}`)
	if err := os.WriteFile(filepath.Join(directory, "tickets-v1.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadConfirmedTicketVersion(context.Background(), "req-1", "tickets-v1"); !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("unknown field error = %v, want ErrInvalidDocument", err)
	}
}

func writeDocument(t *testing.T, root string, document TicketVersionDocument) {
	t.Helper()
	directory := filepath.Join(root, document.RequirementID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, document.ID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

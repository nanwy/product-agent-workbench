package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"product-agent-workbench/internal/domain"
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
	})
	if err != nil {
		t.Fatalf("ReadExecutionFacts() error = %v", err)
	}
	if facts.Prompt != "实现 B" ||
		facts.Workspace.TicketBranch != "workbench/req-1/ticket-b/attempt-2" ||
		facts.Workspace.ModificationScopes[0] != "apps/server" {
		t.Fatalf("ExecutionFacts = %#v", facts)
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

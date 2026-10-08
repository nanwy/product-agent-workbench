package verification

import (
	"errors"
	"testing"
	"time"
)

func TestBuildMatrixNormalizesAndCopiesRecords(t *testing.T) {
	input := []Evidence{{
		ID:          " check-1 ",
		Label:       " Go tests ",
		Status:      StatusNotRun,
		Reason:      " CI unavailable ",
		Required:    true,
		Source:      SourceCandidate,
		StartedAt:   time.Unix(1, 0).UTC(),
		CompletedAt: time.Unix(2, 0).UTC(),
	}}

	matrix, err := BuildMatrix(input)
	if err != nil {
		t.Fatalf("BuildMatrix() error = %v", err)
	}
	if len(matrix.Records) != 1 {
		t.Fatalf("len(Records) = %d, want 1", len(matrix.Records))
	}
	record := matrix.Records[0]
	if record.ID != "check-1" || record.Label != "Go tests" || record.Reason != "CI unavailable" {
		t.Fatalf("Records[0] = %+v, want normalized strings", record)
	}

	input[0].ID = "changed-by-caller"
	if matrix.Records[0].ID != "check-1" {
		t.Fatalf("Matrix retained caller slice: ID = %q", matrix.Records[0].ID)
	}
}

func TestBuildMatrixRejectsInvalidEvidence(t *testing.T) {
	tests := []struct {
		name     string
		evidence []Evidence
	}{
		{name: "blank id", evidence: []Evidence{validEvidence("", "test", StatusVerified)}},
		{name: "blank label", evidence: []Evidence{validEvidence("test", "", StatusVerified)}},
		{name: "unknown status", evidence: []Evidence{validEvidence("test", "test", "success")}},
		{name: "not-run without reason", evidence: []Evidence{validEvidence("test", "test", StatusNotRun)}},
		{name: "unknown source", evidence: []Evidence{func() Evidence {
			evidence := validEvidence("test", "test", StatusVerified)
			evidence.Source = "browser"
			return evidence
		}()}},
		{name: "invalid time", evidence: []Evidence{func() Evidence {
			evidence := validEvidence("test", "test", StatusVerified)
			evidence.CompletedAt = time.Time{}
			return evidence
		}()}},
		{
			name: "duplicate normalized id",
			evidence: []Evidence{
				validEvidence("test", "first", StatusVerified),
				validEvidence(" test ", "second", StatusVerified),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := BuildMatrix(test.evidence)
			if !errors.Is(err, ErrInvalidEvidence) {
				t.Fatalf("BuildMatrix() error = %v, want ErrInvalidEvidence", err)
			}
		})
	}
}

func validEvidence(id string, label string, status Status) Evidence {
	return Evidence{
		ID: id, Label: label, Status: status,
		Source:    SourceCandidate,
		StartedAt: time.Unix(1, 0).UTC(), CompletedAt: time.Unix(2, 0).UTC(),
	}
}

func TestMatrixBlocksDelivery(t *testing.T) {
	tests := []struct {
		name    string
		records []Evidence
		want    bool
	}{
		{name: "empty matrix", records: []Evidence{}, want: false},
		{name: "required verified", records: []Evidence{{Required: true, Status: StatusVerified}}, want: false},
		{name: "required failed", records: []Evidence{{Required: true, Status: StatusFailed}}, want: true},
		{name: "required not-run", records: []Evidence{{Required: true, Status: StatusNotRun}}, want: true},
		{name: "optional failed", records: []Evidence{{Required: false, Status: StatusFailed}}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matrix := Matrix{Records: test.records}
			if got := matrix.BlocksDelivery(); got != test.want {
				t.Fatalf("BlocksDelivery() = %v, want %v", got, test.want)
			}
		})
	}
}

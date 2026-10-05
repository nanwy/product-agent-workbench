package sqlite_test

import (
	"errors"
	"testing"

	"product-agent-workbench/internal/store/sqlite"
)

func TestNewRejectsNilDatabase(t *testing.T) {
	t.Parallel()

	storeAdapter, err := sqlite.New(nil)
	if !errors.Is(err, sqlite.ErrInvalidDependencies) {
		t.Fatalf("New(nil) error = %v, want ErrInvalidDependencies", err)
	}
	if storeAdapter != nil {
		t.Fatalf("New(nil) store = %#v, want nil", storeAdapter)
	}
}

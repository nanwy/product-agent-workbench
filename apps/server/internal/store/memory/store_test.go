package memory_test

import (
	"testing"

	"product-agent-workbench/internal/store"
	"product-agent-workbench/internal/store/memory"
)

func TestNewImplementsStore(t *testing.T) {
	t.Parallel()

	var subject store.Store = memory.New()
	if subject == nil {
		t.Fatal("memory.New() 返回了 nil")
	}
}

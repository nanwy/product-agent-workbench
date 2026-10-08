package verification

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadPlanReadsTrustedCommandProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.json")
	content := []byte(`{
		"name":"ignored by verification",
		"healthUrl":"http://127.0.0.1:3003/health",
		"validationCommands":[{
			"id":"test","label":"Focused test","command":"pnpm","args":["test"],
			"timeoutMs":120000,"environment":"test","proxyPolicy":"auto",
			"requiresRuntime":true
		}]
	}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	plan, err := LoadPlan(path)
	if err != nil {
		t.Fatalf("LoadPlan() error = %v", err)
	}
	if plan.HealthURL != "http://127.0.0.1:3003/health" || len(plan.Commands) != 1 {
		t.Fatalf("LoadPlan() = %#v", plan)
	}
	command := plan.Commands[0]
	if command.ID != "test" || command.Timeout != 120*time.Second || !command.RequiresRuntime ||
		!command.Required {
		t.Fatalf("command = %#v", command)
	}
}

func TestLoadPlanRejectsDuplicateCommandID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.json")
	content := []byte(`{"healthUrl":"http://127.0.0.1:3003","validationCommands":[
		{"id":"test","label":"A","command":"go","timeoutMs":1000,"environment":"test","proxyPolicy":"omit"},
		{"id":"test","label":"B","command":"go","timeoutMs":1000,"environment":"test","proxyPolicy":"omit"}
	]}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := LoadPlan(path); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("LoadPlan() error = %v, want ErrInvalidPlan", err)
	}
}

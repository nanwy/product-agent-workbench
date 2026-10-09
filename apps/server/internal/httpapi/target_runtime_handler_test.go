package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"product-agent-workbench/internal/httpapi"
	"product-agent-workbench/internal/targetruntime"
)

func TestServer_TargetRuntimeActions(t *testing.T) {
	runtime := &fakeTargetRuntime{
		target:   targetruntime.Target{Name: "fixture", ModificationScope: "apps/front"},
		snapshot: targetruntime.Snapshot{Status: targetruntime.StatusStopped, Ownership: targetruntime.OwnershipNone},
	}
	server, err := httpapi.NewServer(httpapi.Dependencies{
		Application:   stubApplication{},
		TargetRuntime: runtime,
		Preview:       fakePreviewApplication{workspacePath: "/tmp/candidate"},
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus targetruntime.Status
	}{
		{name: "读取", method: http.MethodGet, path: httpapi.PathTargetRuntime, wantStatus: targetruntime.StatusStopped},
		{name: "启动", method: http.MethodPost, path: httpapi.PathTargetRuntime + "/start", wantStatus: targetruntime.StatusReady},
		{name: "停止", method: http.MethodPost, path: httpapi.PathTargetRuntime + "/stop", wantStatus: targetruntime.StatusStopped},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			requestBody := ""
			if test.name == "启动" {
				requestBody = `{"requirementId":"req-1","ticketVersionId":"tickets-1"}`
			}
			server.ServeHTTP(response, httptest.NewRequest(test.method, test.path, strings.NewReader(requestBody)))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			var responseBody httpapi.TargetRuntimeResponse
			if err := json.NewDecoder(response.Body).Decode(&responseBody); err != nil {
				t.Fatal(err)
			}
			if responseBody.Target.Name != "fixture" || responseBody.Process.Status != test.wantStatus {
				t.Fatalf("response = %#v", responseBody)
			}
		})
	}
	if runtime.startCalls != 1 || runtime.stopCalls != 1 {
		t.Fatalf("calls start=%d stop=%d", runtime.startCalls, runtime.stopCalls)
	}
}

type fakeTargetRuntime struct {
	target     targetruntime.Target
	snapshot   targetruntime.Snapshot
	startCalls int
	stopCalls  int
	workspace  string
}

func (runtime *fakeTargetRuntime) Target() targetruntime.Target     { return runtime.target }
func (runtime *fakeTargetRuntime) Snapshot() targetruntime.Snapshot { return runtime.snapshot }

func (runtime *fakeTargetRuntime) Start(_ context.Context, workspacePath string) (targetruntime.Snapshot, error) {
	runtime.startCalls++
	runtime.workspace = workspacePath
	runtime.snapshot.Status = targetruntime.StatusReady
	return runtime.snapshot, nil
}

type fakePreviewApplication struct{ workspacePath string }

func (fake fakePreviewApplication) ResolvePreviewWorkspace(context.Context, string, string) (string, error) {
	return fake.workspacePath, nil
}

func (runtime *fakeTargetRuntime) Stop(context.Context) (targetruntime.Snapshot, error) {
	runtime.stopCalls++
	runtime.snapshot.Status = targetruntime.StatusStopped
	return runtime.snapshot, nil
}

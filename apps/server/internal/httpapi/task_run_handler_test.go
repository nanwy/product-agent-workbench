package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/httpapi"
)

func TestServer_EnqueueTaskRun(t *testing.T) {
	t.Skip("练习 9：实现 TaskRun DTO 映射和 enqueue handler 后删除本行")

	want := taskRunHTTPFixture()
	var gotInput app.EnqueueTaskRunInput
	server := newTaskRunHTTPServer(t, fakeTaskRunApplication{
		enqueue: func(_ context.Context, input app.EnqueueTaskRunInput) (domain.TaskRun, error) {
			gotInput = input
			return want, nil
		},
	})
	request := httptest.NewRequest(http.MethodPost, httpapi.PathTaskRuns, strings.NewReader(
		`{"requirementId":"req-1","ticketId":"ticket-1","attempt":1}`,
	))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("POST TaskRun status = %d, want 201; body=%q", response.Code, response.Body.String())
	}
	wantInput := app.EnqueueTaskRunInput{RequirementID: "req-1", TicketID: "ticket-1", Attempt: 1}
	if gotInput != wantInput {
		t.Fatalf("EnqueueTaskRun input = %#v, want %#v", gotInput, wantInput)
	}
	var body httpapi.TaskRunResponse
	decodeSingleJSON(t, response, &body)
	if body.ID != want.ID || body.Status != domain.TaskStatusQueued || !body.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("TaskRun response = %#v, want queued run %#v", body, want)
	}
}

func TestServer_GetAndCancelTaskRun(t *testing.T) {
	t.Skip("练习 10：实现 TaskRun get/cancel handler 后删除本行")

	want := taskRunHTTPFixture()
	server := newTaskRunHTTPServer(t, fakeTaskRunApplication{
		get: func(context.Context, string) (domain.TaskRun, error) { return want, nil },
		cancel: func(context.Context, string) (domain.TaskRun, error) {
			cancelledAt := want.CreatedAt.Add(time.Minute)
			want.Status = domain.TaskStatusCancelled
			want.FinishedAt = &cancelledAt
			return want, nil
		},
	})

	for _, tt := range []struct {
		name   string
		method string
		path   string
		status domain.TaskStatus
	}{
		{name: "读取", method: http.MethodGet, path: httpapi.PathTaskRuns + "/run-1", status: domain.TaskStatusQueued},
		{name: "取消", method: http.MethodPost, path: httpapi.PathTaskRuns + "/run-1/cancel", status: domain.TaskStatusCancelled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, nil)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("%s %s status = %d, want 200", tt.method, tt.path, response.Code)
			}
			var body httpapi.TaskRunResponse
			decodeSingleJSON(t, response, &body)
			if body.ID != "run-1" || body.Status != tt.status {
				t.Fatalf("TaskRun response = %#v, want status %q", body, tt.status)
			}
		})
	}
}

type fakeTaskRunApplication struct {
	enqueue func(context.Context, app.EnqueueTaskRunInput) (domain.TaskRun, error)
	get     func(context.Context, string) (domain.TaskRun, error)
	cancel  func(context.Context, string) (domain.TaskRun, error)
}

func (fake fakeTaskRunApplication) EnqueueTaskRun(ctx context.Context, input app.EnqueueTaskRunInput) (domain.TaskRun, error) {
	if fake.enqueue == nil {
		panic("测试没有配置 EnqueueTaskRun")
	}
	return fake.enqueue(ctx, input)
}

func (fake fakeTaskRunApplication) GetTaskRun(ctx context.Context, id string) (domain.TaskRun, error) {
	if fake.get == nil {
		panic("测试没有配置 GetTaskRun")
	}
	return fake.get(ctx, id)
}

func (fake fakeTaskRunApplication) CancelTaskRun(ctx context.Context, id string) (domain.TaskRun, error) {
	if fake.cancel == nil {
		panic("测试没有配置 CancelTaskRun")
	}
	return fake.cancel(ctx, id)
}

func newTaskRunHTTPServer(t *testing.T, taskRuns httpapi.TaskRunApplication) *httpapi.Server {
	t.Helper()
	server, err := httpapi.NewServer(httpapi.Dependencies{
		Application: fakeRequirementApplication{},
		TaskRuns:    taskRuns,
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	return server
}

func taskRunHTTPFixture() domain.TaskRun {
	return domain.TaskRun{
		ID:            "run-1",
		RequirementID: "req-1",
		TicketID:      "ticket-1",
		Attempt:       1,
		Status:        domain.TaskStatusQueued,
		CreatedAt:     taskRunTestHTTPTime(),
	}
}

func taskRunTestHTTPTime() time.Time {
	return time.Date(2026, time.October, 5, 10, 0, 0, 0, time.UTC)
}

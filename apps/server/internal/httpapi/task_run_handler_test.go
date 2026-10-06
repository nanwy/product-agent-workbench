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
	"product-agent-workbench/internal/store"
)

func TestServer_EnqueueTaskRun(t *testing.T) {
	// 练习 9：从浏览器 JSON 到 Application 输入，再把领域结果映射为稳定 HTTP DTO。

	want := taskRunHTTPFixture()
	var gotContext context.Context
	var gotInput app.EnqueueTaskRunInput
	server := newTaskRunHTTPServer(t, fakeTaskRunApplication{
		enqueue: func(ctx context.Context, input app.EnqueueTaskRunInput) (domain.TaskRun, error) {
			gotContext = ctx
			gotInput = input
			return want, nil
		},
	})
	request := httptest.NewRequest(http.MethodPost, httpapi.PathTaskRuns, strings.NewReader(
		`{"requirementId":"req-1","ticketVersionId":"req-1-tickets-v1","ticketId":"ticket-1","attempt":1}`,
	))
	request = request.WithContext(context.WithValue(request.Context(), requestContextKey{}, "task-trace-1"))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("POST TaskRun status = %d, want 201; body=%q", response.Code, response.Body.String())
	}
	wantInput := app.EnqueueTaskRunInput{RequirementID: "req-1", TicketVersionID: "req-1-tickets-v1", TicketID: "ticket-1", Attempt: 1}
	if gotInput != wantInput {
		t.Fatalf("EnqueueTaskRun input = %#v, want %#v", gotInput, wantInput)
	}
	if gotContext == nil || gotContext.Value(requestContextKey{}) != "task-trace-1" {
		t.Fatal("handler 没有把 request context 原样传给 Application")
	}
	assertJSONContentType(t, response)
	var body httpapi.TaskRunResponse
	decodeSingleJSON(t, response, &body)
	if body.ID != want.ID || body.Status != domain.TaskStatusQueued || !body.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("TaskRun response = %#v, want queued run %#v", body, want)
	}
}

func TestServer_EnqueueTaskRunRejectsInvalidJSON(t *testing.T) {
	calls := 0
	server := newTaskRunHTTPServer(t, fakeTaskRunApplication{
		enqueue: func(context.Context, app.EnqueueTaskRunInput) (domain.TaskRun, error) {
			calls++
			return domain.TaskRun{}, nil
		},
	})
	request := httptest.NewRequest(
		http.MethodPost,
		httpapi.PathTaskRuns,
		strings.NewReader(`{"requirementId":"req-1","ticketVersionId":"req-1-tickets-v1","ticketId":"ticket-1","attempt":1,"status":"completed"}`),
	)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%q", response.Code, response.Body.String())
	}
	assertErrorResponse(t, response, "invalid_request")
	if calls != 0 {
		t.Fatalf("非法 JSON 调用了 Application %d 次，want 0", calls)
	}
}

func TestServer_GetAndCancelTaskRun(t *testing.T) {
	// 练习 10：路径参数进入查询/取消用例，成功后统一映射为 TaskRunResponse。

	want := taskRunHTTPFixture()
	var gotGetID string
	var gotCancelID string
	server := newTaskRunHTTPServer(t, fakeTaskRunApplication{
		get: func(_ context.Context, id string) (domain.TaskRun, error) {
			gotGetID = id
			return want, nil
		},
		cancel: func(_ context.Context, id string) (domain.TaskRun, error) {
			gotCancelID = id
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
	if gotGetID != "run-1" || gotCancelID != "run-1" {
		t.Fatalf("路径参数传递错误：get=%q cancel=%q", gotGetID, gotCancelID)
	}
}

func TestServer_TaskRunHandlersMapApplicationErrors(t *testing.T) {
	t.Run("查询不存在返回 404", func(t *testing.T) {
		server := newTaskRunHTTPServer(t, fakeTaskRunApplication{
			get: func(context.Context, string) (domain.TaskRun, error) {
				return domain.TaskRun{}, store.ErrNotFound
			},
		})
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, httpapi.PathTaskRuns+"/missing", nil)
		server.ServeHTTP(response, request)

		if response.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", response.Code)
		}
		assertErrorResponse(t, response, "not_found")
	})

	t.Run("取消状态冲突返回 409", func(t *testing.T) {
		server := newTaskRunHTTPServer(t, fakeTaskRunApplication{
			cancel: func(context.Context, string) (domain.TaskRun, error) {
				return domain.TaskRun{}, store.ErrTaskConflict
			},
		})
		response := httptest.NewRecorder()
		server.ServeHTTP(
			response,
			httptest.NewRequest(http.MethodPost, httpapi.PathTaskRuns+"/run-1/cancel", nil),
		)

		if response.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409", response.Code)
		}
		assertErrorResponse(t, response, "conflict")
	})
}

func TestServer_TaskRunResponseMapsCompletedResult(t *testing.T) {
	want := taskRunHTTPFixture()
	claimedAt := want.CreatedAt.Add(time.Minute)
	startedAt := claimedAt.Add(time.Minute)
	finishedAt := startedAt.Add(time.Minute)
	want.Status = domain.TaskStatusCompleted
	want.ClaimedAt = &claimedAt
	want.StartedAt = &startedAt
	want.FinishedAt = &finishedAt
	want.Result = &domain.TaskResult{
		Summary:       "实现完成",
		CodexThreadID: "thread-1",
		WorktreePath:  "/tmp/worktree-1",
	}

	server := newTaskRunHTTPServer(t, fakeTaskRunApplication{
		get: func(context.Context, string) (domain.TaskRun, error) { return want, nil },
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, httpapi.PathTaskRuns+"/run-1", nil)
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	var body httpapi.TaskRunResponse
	decodeSingleJSON(t, response, &body)
	if body.Result == nil ||
		body.Result.Summary != "实现完成" ||
		body.Result.CodexThreadID != "thread-1" ||
		body.Result.WorktreePath != "/tmp/worktree-1" {
		t.Fatalf("TaskRun result response = %#v, want completed result", body.Result)
	}
	if body.ClaimedAt == nil || body.StartedAt == nil || body.FinishedAt == nil {
		t.Fatalf("TaskRun 时间映射不完整：%#v", body)
	}
}

type fakeTaskRunApplication struct {
	enqueue func(context.Context, app.EnqueueTaskRunInput) (domain.TaskRun, error)
	get     func(context.Context, string) (domain.TaskRun, error)
	cancel  func(context.Context, string) (domain.TaskRun, error)
}

func (fake fakeTaskRunApplication) EnqueueTaskRun(
	ctx context.Context,
	input app.EnqueueTaskRunInput,
) (domain.TaskRun, error) {
	if fake.enqueue == nil {
		panic("测试没有配置 EnqueueTaskRun")
	}
	return fake.enqueue(ctx, input)
}

func (fake fakeTaskRunApplication) GetTaskRun(
	ctx context.Context,
	id string,
) (domain.TaskRun, error) {
	if fake.get == nil {
		panic("测试没有配置 GetTaskRun")
	}
	return fake.get(ctx, id)
}

func (fake fakeTaskRunApplication) CancelTaskRun(
	ctx context.Context,
	id string,
) (domain.TaskRun, error) {
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
		ID:              "run-1",
		RequirementID:   "req-1",
		TicketVersionID: "req-1-tickets-v1",
		TicketID:        "ticket-1",
		Attempt:         1,
		Status:          domain.TaskStatusQueued,
		CreatedAt:       taskRunTestHTTPTime(),
	}
}

func taskRunTestHTTPTime() time.Time {
	return time.Date(2026, time.October, 5, 10, 0, 0, 0, time.UTC)
}

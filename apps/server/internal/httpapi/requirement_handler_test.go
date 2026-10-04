package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/httpapi"
	"product-agent-workbench/internal/store"
)

type requestContextKey struct{}

func TestServer_CreateRequirement(t *testing.T) {
	t.Skip("练习 1：实现 JSON helpers 和创建 handler 成功路径后删除本行")

	wantRequirement := requirementHTTPFixture()
	var gotContext context.Context
	var gotInput app.CreateRequirementInput
	application := fakeRequirementApplication{
		create: func(ctx context.Context, input app.CreateRequirementInput) (domain.Requirement, error) {
			gotContext = ctx
			gotInput = input
			return wantRequirement, nil
		},
	}
	server := newHTTPServer(t, application)

	request := httptest.NewRequest(
		http.MethodPost,
		httpapi.PathRequirements,
		strings.NewReader(`{"projectId":"project-1","title":"创建 HTTP 接口"}`),
	)
	request = request.WithContext(context.WithValue(request.Context(), requestContextKey{}, "trace-1"))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("POST %s status = %d, want %d; body=%q", httpapi.PathRequirements, response.Code, http.StatusCreated, response.Body.String())
	}
	assertJSONContentType(t, response)
	gotResponse := decodeRequirementResponse(t, response)
	assertRequirementResponse(t, gotResponse, wantRequirement)

	wantInput := app.CreateRequirementInput{ProjectID: "project-1", Title: "创建 HTTP 接口"}
	if gotInput != wantInput {
		t.Fatalf("CreateRequirement input = %#v, want %#v", gotInput, wantInput)
	}
	if gotContext == nil || gotContext.Value(requestContextKey{}) != "trace-1" {
		t.Fatal("handler 没有把 request context 原样传给 Application")
	}
}

func TestServer_CreateRequirementRejectsInvalidJSON(t *testing.T) {
	t.Skip("练习 2：实现严格 decodeJSON 后删除本行")

	tests := []struct {
		name string
		body string
	}{
		{name: "空 body", body: ""},
		{name: "非法 JSON", body: `{"projectId":`},
		{name: "未知字段", body: `{"projectId":"project-1","title":"标题","stage":"delivered"}`},
		{name: "多个 JSON 对象", body: `{"projectId":"project-1","title":"标题"} {"title":"第二个"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			server := newHTTPServer(t, fakeRequirementApplication{
				create: func(context.Context, app.CreateRequirementInput) (domain.Requirement, error) {
					calls++
					return domain.Requirement{}, nil
				},
			})
			request := httptest.NewRequest(http.MethodPost, httpapi.PathRequirements, strings.NewReader(tt.body))
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, http.StatusBadRequest, response.Body.String())
			}
			assertErrorResponse(t, response, "invalid_request")
			if calls != 0 {
				t.Fatalf("非法 JSON 调用了 Application %d 次，want 0", calls)
			}
		})
	}
}

func TestServer_MapsApplicationErrors(t *testing.T) {
	t.Skip("练习 3：实现 mapError 和 writeError 后删除本行")

	tests := []struct {
		name       string
		appError   error
		wantStatus int
		wantCode   string
	}{
		{name: "输入无效", appError: domain.ErrInvalidInput, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "资源不存在", appError: store.ErrNotFound, wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{name: "资源重复", appError: store.ErrAlreadyExists, wantStatus: http.StatusConflict, wantCode: "conflict"},
		{name: "版本冲突", appError: store.ErrRevisionConflict, wantStatus: http.StatusConflict, wantCode: "conflict"},
		{name: "状态冲突", appError: domain.ErrInvalidTransition, wantStatus: http.StatusConflict, wantCode: "conflict"},
		{name: "未知错误", appError: errors.New("包含不能泄漏的数据库细节"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newHTTPServer(t, fakeRequirementApplication{
				create: func(context.Context, app.CreateRequirementInput) (domain.Requirement, error) {
					return domain.Requirement{}, tt.appError
				},
			})
			request := httptest.NewRequest(
				http.MethodPost,
				httpapi.PathRequirements,
				strings.NewReader(`{"projectId":"project-1","title":"标题"}`),
			)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			body := assertErrorResponse(t, response, tt.wantCode)
			if strings.Contains(body.Message, "数据库") {
				t.Fatalf("错误响应泄漏内部细节：%q", body.Message)
			}
		})
	}
}

func TestServer_GetRequirement(t *testing.T) {
	t.Skip("练习 4：实现 GET Requirement handler 后删除本行")

	wantRequirement := requirementHTTPFixture()
	var gotContext context.Context
	var gotID string
	application := fakeRequirementApplication{
		get: func(ctx context.Context, id string) (domain.Requirement, error) {
			gotContext = ctx
			gotID = id
			return wantRequirement, nil
		},
	}
	server := newHTTPServer(t, application)
	request := httptest.NewRequest(http.MethodGet, httpapi.PathRequirements+"/req-1", nil)
	request = request.WithContext(context.WithValue(request.Context(), requestContextKey{}, "trace-2"))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("GET Requirement status = %d, want %d; body=%q", response.Code, http.StatusOK, response.Body.String())
	}
	assertJSONContentType(t, response)
	gotResponse := decodeRequirementResponse(t, response)
	assertRequirementResponse(t, gotResponse, wantRequirement)
	if gotID != wantRequirement.ID {
		t.Fatalf("GetRequirement id = %q, want %q", gotID, wantRequirement.ID)
	}
	if gotContext == nil || gotContext.Value(requestContextKey{}) != "trace-2" {
		t.Fatal("handler 没有把 request context 原样传给 Application")
	}

	t.Run("不存在返回 404", func(t *testing.T) {
		notFoundServer := newHTTPServer(t, fakeRequirementApplication{
			get: func(context.Context, string) (domain.Requirement, error) {
				return domain.Requirement{}, store.ErrNotFound
			},
		})
		notFoundRequest := httptest.NewRequest(http.MethodGet, httpapi.PathRequirements+"/missing", nil)
		notFoundResponse := httptest.NewRecorder()
		notFoundServer.ServeHTTP(notFoundResponse, notFoundRequest)

		if notFoundResponse.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", notFoundResponse.Code, http.StatusNotFound)
		}
		assertErrorResponse(t, notFoundResponse, "not_found")
	})
}

type fakeRequirementApplication struct {
	create func(context.Context, app.CreateRequirementInput) (domain.Requirement, error)
	get    func(context.Context, string) (domain.Requirement, error)
}

func (fake fakeRequirementApplication) CreateRequirement(
	ctx context.Context,
	input app.CreateRequirementInput,
) (domain.Requirement, error) {
	if fake.create == nil {
		panic("测试没有配置 CreateRequirement")
	}
	return fake.create(ctx, input)
}

func (fake fakeRequirementApplication) GetRequirement(
	ctx context.Context,
	id string,
) (domain.Requirement, error) {
	if fake.get == nil {
		panic("测试没有配置 GetRequirement")
	}
	return fake.get(ctx, id)
}

func newHTTPServer(t *testing.T, application httpapi.RequirementApplication) *httpapi.Server {
	t.Helper()

	server, err := httpapi.NewServer(httpapi.Dependencies{Application: application})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	return server
}

func requirementHTTPFixture() domain.Requirement {
	return domain.Requirement{
		ID:        "req-1",
		ProjectID: "project-1",
		Title:     "创建 HTTP 接口",
		Stage:     domain.RequirementStageGrilling,
		Revision:  1,
	}
}

func assertJSONContentType(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()

	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want application/json; charset=utf-8", got)
	}
}

func decodeRequirementResponse(
	t *testing.T,
	response *httptest.ResponseRecorder,
) httpapi.RequirementResponse {
	t.Helper()

	assertJSONContentType(t, response)
	var body httpapi.RequirementResponse
	decodeSingleJSON(t, response, &body)
	return body
}

func assertErrorResponse(
	t *testing.T,
	response *httptest.ResponseRecorder,
	wantCode string,
) httpapi.ErrorResponse {
	t.Helper()

	assertJSONContentType(t, response)
	var body httpapi.ErrorResponse
	decodeSingleJSON(t, response, &body)
	if body.Code != wantCode {
		t.Fatalf("error code = %q, want %q", body.Code, wantCode)
	}
	if strings.TrimSpace(body.Message) == "" {
		t.Fatal("error message 不能为空")
	}
	return body
}

func decodeSingleJSON(
	t *testing.T,
	response *httptest.ResponseRecorder,
	destination any,
) {
	t.Helper()

	decoder := json.NewDecoder(response.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		t.Fatalf("decode JSON response: %v; body=%q", err, response.Body.String())
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("JSON response 包含多余内容：%v", err)
	}
}

func assertRequirementResponse(
	t *testing.T,
	got httpapi.RequirementResponse,
	want domain.Requirement,
) {
	t.Helper()

	if got.ID != want.ID ||
		got.ProjectID != want.ProjectID ||
		got.Title != want.Title ||
		got.Stage != want.Stage ||
		got.Revision != want.Revision {
		t.Fatalf("RequirementResponse = %#v, want domain %#v", got, want)
	}
}

package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/httpapi"
)

func TestNewServer(t *testing.T) {
	// t.Skip("练习 1：实现 httpapi.NewServer 后删除本行")

	t.Run("依赖完整", func(t *testing.T) {
		server, err := httpapi.NewServer(httpapi.Dependencies{Application: stubApplication{}})
		if err != nil {
			t.Fatalf("NewServer() error = %v", err)
		}
		if server == nil {
			t.Fatal("NewServer() server = nil, want non-nil")
		}
	})

	t.Run("缺少 Application", func(t *testing.T) {
		server, err := httpapi.NewServer(httpapi.Dependencies{})
		if !errors.Is(err, httpapi.ErrInvalidDependencies) {
			t.Fatalf("NewServer() error = %v, want ErrInvalidDependencies", err)
		}
		if server != nil {
			t.Fatalf("NewServer() server = %#v, want nil", server)
		}
	})
}

func TestServer_Health(t *testing.T) {
	// t.Skip("练习 2：实现 ServeHTTP 和 GET /health 后删除本行")

	application := &recordingApplication{}
	server, err := httpapi.NewServer(httpapi.Dependencies{Application: application})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, httpapi.PathHealth, nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want %d; body=%q", httpapi.PathHealth, response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want application/json; charset=utf-8", got)
	}

	var body httpapi.HealthResponse
	decoder := json.NewDecoder(response.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("health status = %q, want ok", body.Status)
	}
	if application.calls != 0 {
		t.Fatalf("health endpoint 调用了 Application %d 次，want 0", application.calls)
	}
}

// recordingApplication 是 HTTP 层测试使用的最小 fake。
// 健康检查不应调用任何应用用例，因此这里只记录调用次数。
type recordingApplication struct {
	calls int
}

func (application *recordingApplication) CreateRequirement(
	context.Context,
	app.CreateRequirementInput,
) (domain.Requirement, error) {
	application.calls++
	return domain.Requirement{}, nil
}

func (application *recordingApplication) GetRequirement(
	context.Context,
	string,
) (domain.Requirement, error) {
	application.calls++
	return domain.Requirement{}, nil
}

// stubApplication 用于只关心依赖是否存在的构造测试。
type stubApplication struct{}

func (stubApplication) CreateRequirement(
	context.Context,
	app.CreateRequirementInput,
) (domain.Requirement, error) {
	return domain.Requirement{}, nil
}

func (stubApplication) GetRequirement(
	context.Context,
	string,
) (domain.Requirement, error) {
	return domain.Requirement{}, nil
}

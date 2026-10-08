package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestHandlerRoutesAPIAssetsAndSPAFallback(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	assets := fstest.MapFS{
		"index.html":    {Data: []byte("<main>spa</main>")},
		"assets/app.js": {Data: []byte("console.log('spa')")},
	}
	handler := newHandler(api, assets, []byte("fallback"))

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
	}{
		{name: "API", method: http.MethodGet, path: "/api/v1/health", wantStatus: http.StatusTeapot},
		{name: "首页", method: http.MethodGet, path: "/", wantStatus: http.StatusOK, wantBody: "<main>spa</main>"},
		{name: "前端路由", method: http.MethodGet, path: "/requirements/req-1", wantStatus: http.StatusOK, wantBody: "<main>spa</main>"},
		{name: "静态资源", method: http.MethodGet, path: "/assets/app.js", wantStatus: http.StatusOK, wantBody: "console.log('spa')"},
		{name: "缺失静态资源", method: http.MethodGet, path: "/assets/missing.js", wantStatus: http.StatusNotFound},
		{name: "页面拒绝写请求", method: http.MethodPost, path: "/", wantStatus: http.StatusMethodNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if test.wantBody != "" && response.Body.String() != test.wantBody {
				t.Fatalf("body = %q, want %q", response.Body.String(), test.wantBody)
			}
		})
	}
}

func TestHandlerExplainsMissingSPA(t *testing.T) {
	empty := fstest.MapFS{"README.txt": {Data: []byte("build first")}}
	handler := newHandler(http.NotFoundHandler(), fs.FS(empty), []byte("请先构建 SPA"))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable || response.Body.String() != "请先构建 SPA" {
		t.Fatalf("response = (%d, %q)", response.Code, response.Body.String())
	}
}

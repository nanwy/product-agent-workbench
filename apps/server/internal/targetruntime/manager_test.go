package targetruntime

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestManagerReusesMarkedExternalRuntimeAndProxiesPreview(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("X-Frame-Options", "DENY")
		response.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		_, _ = io.WriteString(response, "fixture-runtime-marker")
	}))
	defer upstream.Close()

	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	gatewayPort := reservePort(t)
	manager, err := NewManager(Config{
		Name:                  "fixture",
		RepositoryRoot:        t.TempDir(),
		ModificationScope:     "apps/front",
		StartCommand:          Command{Executable: "unused"},
		HealthURL:             upstreamURL,
		PreviewURL:            upstreamURL,
		GatewayPort:           gatewayPort,
		StartupTimeout:        time.Second,
		ReuseHealthBodyMarker: "fixture-runtime-marker",
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	snapshot, err := manager.Start(context.Background(), "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if snapshot.Status != StatusReady || snapshot.Ownership != OwnershipExternal || snapshot.PID != nil {
		t.Fatalf("Start() snapshot = %#v", snapshot)
	}
	response, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/preview", gatewayPort))
	if err != nil {
		t.Fatalf("GET gateway: %v", err)
	}
	defer response.Body.Close()
	if value := response.Header.Get("X-Frame-Options"); value != "" {
		t.Fatalf("X-Frame-Options = %q, want removed", value)
	}
	if value := response.Header.Get("Content-Security-Policy"); value != "default-src 'self'" {
		t.Fatalf("Content-Security-Policy = %q", value)
	}

	stopped, err := manager.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if stopped.Status != StatusStopped || stopped.Ownership != OwnershipNone {
		t.Fatalf("Stop() snapshot = %#v", stopped)
	}
	// 外部服务仍能响应，证明 Stop 只关闭自己的网关，没有按端口猜测并杀进程。
	externalResponse, err := http.Get(upstream.URL)
	if err != nil {
		t.Fatalf("external runtime after Stop(): %v", err)
	}
	externalResponse.Body.Close()
}

func TestManagerRejectsUnmarkedExternalRuntime(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, "another-project")
	}))
	defer upstream.Close()
	upstreamURL, _ := url.Parse(upstream.URL)
	manager, err := NewManager(Config{
		Name:                  "fixture",
		RepositoryRoot:        t.TempDir(),
		ModificationScope:     "apps/front",
		StartCommand:          Command{Executable: "unused"},
		HealthURL:             upstreamURL,
		PreviewURL:            upstreamURL,
		GatewayPort:           reservePort(t),
		StartupTimeout:        time.Second,
		ReuseHealthBodyMarker: "expected-marker",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.Start(context.Background(), "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if snapshot.Status != StatusFailed || snapshot.Ownership != OwnershipNone || snapshot.Error == "" {
		t.Fatalf("Start() snapshot = %#v", snapshot)
	}
}

func reservePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

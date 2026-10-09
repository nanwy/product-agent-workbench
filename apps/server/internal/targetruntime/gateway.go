package targetruntime

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

// previewGateway 是目标服务前面的本地反向代理。前端类比：它承担 Vite devServer proxy
// 的角色，同时移除阻止 iframe 嵌入的响应头；WebSocket Upgrade 由 ReverseProxy 处理。
type previewGateway struct {
	mu     sync.Mutex
	server *http.Server
	port   int
}

func (gateway *previewGateway) Start(upstream *url.URL, port int) error {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if gateway.server != nil {
		return nil
	}

	proxy := httputil.NewSingleHostReverseProxy(upstream)
	originalDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		originalDirector(request)
		request.Host = upstream.Host
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		response.Header.Del("X-Frame-Options")
		if policy := response.Header.Get("Content-Security-Policy"); policy != "" {
			directives := strings.Split(policy, ";")
			kept := directives[:0]
			for _, directive := range directives {
				trimmed := strings.TrimSpace(directive)
				if trimmed != "" && !strings.HasPrefix(strings.ToLower(trimmed), "frame-ancestors") {
					kept = append(kept, trimmed)
				}
			}
			if len(kept) == 0 {
				response.Header.Del("Content-Security-Policy")
			} else {
				response.Header.Set("Content-Security-Policy", strings.Join(kept, "; "))
			}
		}
		return nil
	}
	proxy.ErrorHandler = func(response http.ResponseWriter, _ *http.Request, err error) {
		http.Error(response, "Preview upstream unavailable", http.StatusBadGateway)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("启动预览网关: %w", err)
	}
	server := &http.Server{Handler: proxy, ReadHeaderTimeout: 5 * time.Second}
	gateway.server = server
	gateway.port = port
	go func() {
		_ = server.Serve(listener)
	}()
	return nil
}

func (gateway *previewGateway) Stop(ctx context.Context) error {
	gateway.mu.Lock()
	server := gateway.server
	gateway.server = nil
	gateway.port = 0
	gateway.mu.Unlock()
	if server == nil {
		return nil
	}
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("停止预览网关: %w", err)
	}
	return nil
}

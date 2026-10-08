package verification

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RuntimeProbe 为 requiresRuntime 命令确认目标服务已经可访问。
type RuntimeProbe interface {
	Check(ctx context.Context) error
}

type HTTPRuntimeProbe struct {
	url    string
	client *http.Client
}

func NewHTTPRuntimeProbe(rawURL string) (*HTTPRuntimeProbe, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return nil, ErrInvalidPlan
	}
	host := strings.Trim(parsed.Hostname(), "[]")
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("%w: healthUrl 必须是 loopback", ErrInvalidPlan)
	}
	return &HTTPRuntimeProbe{
		url:    rawURL,
		client: &http.Client{Timeout: 5 * time.Second},
	}, nil
}

func (p *HTTPRuntimeProbe) Check(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("目标运行时健康检查返回 HTTP %d", response.StatusCode)
	}
	return nil
}

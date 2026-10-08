package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/httpapi"
)

func TestServer_EventsReplaysAndContinuesFromLastEventID(t *testing.T) {
	source := &recordingEventApplication{events: []domain.TaskEvent{
		{ID: 1, TaskRunID: "run-a", Sequence: 1, Type: "task.started", Payload: []byte("one"), CreatedAt: time.Unix(1, 0).UTC()},
		{ID: 2, TaskRunID: "run-b", Sequence: 1, Type: "task.started", Payload: []byte("two"), CreatedAt: time.Unix(2, 0).UTC()},
		{ID: 3, TaskRunID: "run-a", Sequence: 2, Type: "task.output", Payload: []byte("three\nlines"), CreatedAt: time.Unix(3, 0).UTC()},
	}}
	server := newEventTestServer(t, source, time.Hour, time.Hour, 1)
	response, cancel, done := startEventStream(server, "1")
	defer cancel()

	body := waitForSSE(t, response, "id: 3", 2*time.Second)
	if response.statusCode() != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.statusCode())
	}
	if got := response.headerValue("Content-Type"); got != "text/event-stream; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if strings.Contains(body, "id: 1") {
		t.Fatalf("重放包含 Last-Event-ID 已确认事件：%q", body)
	}
	for _, want := range []string{
		"id: 2", "id: 3", "event: task-event",
		`"taskRunId":"run-b"`, `"type":"task.output"`, `"payload":"three\nlines"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body 缺少 %q: %q", want, body)
		}
	}
	cancel()
	<-done

	if got := source.firstAfterID(); got != 1 {
		t.Fatalf("首个 replay AfterID = %d, want 1", got)
	}
}

func TestServer_EventsPublishesNewPersistentEvent(t *testing.T) {
	source := &recordingEventApplication{}
	server := newEventTestServer(t, source, 5*time.Millisecond, time.Hour, 10)
	response, cancel, done := startEventStream(server, "")
	defer cancel()

	source.append(domain.TaskEvent{
		ID: 1, TaskRunID: "run-live", Sequence: 1, Type: "task.started", CreatedAt: time.Unix(1, 0).UTC(),
	})
	body := waitForSSE(t, response, "id: 1", 2*time.Second)
	if !strings.Contains(body, `"taskRunId":"run-live"`) {
		t.Fatalf("live SSE body = %q", body)
	}
	cancel()
	<-done
}

func TestServer_EventsHeartbeatDoesNotAdvanceCursor(t *testing.T) {
	server := newEventTestServer(t, &recordingEventApplication{}, time.Hour, 5*time.Millisecond, 10)
	response, cancel, done := startEventStream(server, "")
	defer cancel()
	body := waitForSSE(t, response, ": heartbeat", 2*time.Second)
	if strings.Contains(body, "id:") {
		t.Fatalf("heartbeat 不应推进游标: %q", body)
	}
	cancel()
	<-done
}

func TestServer_EventsRejectsInvalidCursorAndResetsStaleCursor(t *testing.T) {
	source := &recordingEventApplication{events: []domain.TaskEvent{{
		ID: 1, TaskRunID: "run-1", Sequence: 1, Type: "task.started", CreatedAt: time.Unix(1, 0).UTC(),
	}}}
	server := newEventTestServer(t, source, time.Hour, time.Hour, 10)

	invalid := httptest.NewRequest(http.MethodGet, httpapi.PathEvents, nil)
	invalid.Header.Set("Last-Event-ID", "not-a-number")
	invalidResponse := httptest.NewRecorder()
	server.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d, want 400", invalidResponse.Code)
	}

	response, cancel, done := startEventStream(server, "99")
	defer cancel()
	<-done
	got := response.bodyString()
	if !strings.Contains(got, "event: replay-reset") ||
		!strings.Contains(got, `"latestEventId":1`) ||
		!strings.Contains(got, "id: 1") {
		t.Fatalf("reset SSE body = %q", got)
	}
}

func TestServer_EventsAcceptsBrowserCursorQueryAndPrefersReconnectHeader(t *testing.T) {
	source := &recordingEventApplication{events: []domain.TaskEvent{
		{ID: 1, TaskRunID: "run-1", Sequence: 1, Type: "task.started", CreatedAt: time.Unix(1, 0).UTC()},
		{ID: 2, TaskRunID: "run-1", Sequence: 2, Type: "task.output", CreatedAt: time.Unix(2, 0).UTC()},
	}}
	server := newEventTestServer(t, source, time.Hour, time.Hour, 10)

	queryResponse, queryCancel, queryDone := startEventStreamAtPath(
		server,
		httpapi.PathEvents+"?after=1",
		"",
	)
	waitForSSE(t, queryResponse, "id: 2", 2*time.Second)
	queryCancel()
	<-queryDone
	if got := source.firstAfterID(); got != 1 {
		t.Fatalf("查询参数 replay AfterID = %d, want 1", got)
	}

	source.mu.Lock()
	source.calls = nil
	source.mu.Unlock()
	headerResponse, headerCancel, headerDone := startEventStreamAtPath(
		server,
		httpapi.PathEvents+"?after=99",
		"1",
	)
	waitForSSE(t, headerResponse, "id: 2", 2*time.Second)
	headerCancel()
	<-headerDone
	if got := source.firstAfterID(); got != 1 {
		t.Fatalf("请求头优先时 AfterID = %d, want 1", got)
	}
}

func newEventTestServer(
	t *testing.T,
	events *recordingEventApplication,
	pollInterval time.Duration,
	heartbeatInterval time.Duration,
	limit int,
) *httpapi.Server {
	t.Helper()
	server, err := httpapi.NewServer(httpapi.Dependencies{
		Application:            stubApplication{},
		Events:                 events,
		EventPollInterval:      pollInterval,
		EventHeartbeatInterval: heartbeatInterval,
		EventReplayLimit:       limit,
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	return server
}

func startEventStream(
	server *httpapi.Server,
	lastEventID string,
) (*streamingResponse, context.CancelFunc, <-chan struct{}) {
	return startEventStreamAtPath(server, httpapi.PathEvents, lastEventID)
}

func startEventStreamAtPath(
	server *httpapi.Server,
	path string,
	lastEventID string,
) (*streamingResponse, context.CancelFunc, <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
	request.Header.Set("Last-Event-ID", lastEventID)
	response := newStreamingResponse()
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.ServeHTTP(response, request)
	}()
	return response, cancel, done
}

func waitForSSE(t *testing.T, response *streamingResponse, marker string, timeout time.Duration) string {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		content := response.bodyString()
		if strings.Contains(content, marker) {
			return content
		}
		select {
		case <-response.flushed:
		case <-timer.C:
			t.Fatalf("等待 SSE %q 超时，当前响应 %q", marker, content)
			return ""
		}
	}
}

// streamingResponse 模拟一个支持 Flush 的真实连接，同时让测试安全读取正在增长的 body。
// httptest.ResponseRecorder 不保证一边由 handler 写、一边由测试读时并发安全。
type streamingResponse struct {
	mu      sync.Mutex
	header  http.Header
	status  int
	body    strings.Builder
	flushed chan struct{}
}

func newStreamingResponse() *streamingResponse {
	return &streamingResponse{header: make(http.Header), flushed: make(chan struct{}, 1)}
}

func (r *streamingResponse) Header() http.Header { return r.header }

func (r *streamingResponse) WriteHeader(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == 0 {
		r.status = status
	}
}

func (r *streamingResponse) Write(payload []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(payload)
}

func (r *streamingResponse) Flush() {
	select {
	case r.flushed <- struct{}{}:
	default:
	}
}

func (r *streamingResponse) statusCode() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

func (r *streamingResponse) headerValue(name string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.header.Get(name)
}

func (r *streamingResponse) bodyString() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.String()
}

type recordingEventApplication struct {
	mu     sync.Mutex
	events []domain.TaskEvent
	calls  []app.ReplayTaskEventsInput
}

func (s *recordingEventApplication) ReplayTaskEvents(
	ctx context.Context,
	input app.ReplayTaskEventsInput,
) (app.TaskEventReplay, error) {
	if err := ctx.Err(); err != nil {
		return app.TaskEventReplay{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, input)
	oldestID, latestID := int64(0), int64(0)
	if len(s.events) > 0 {
		oldestID = s.events[0].ID
		latestID = s.events[len(s.events)-1].ID
	}
	replay := app.TaskEventReplay{
		Events:   make([]domain.TaskEvent, 0),
		OldestID: oldestID,
		LatestID: latestID,
	}
	if input.AfterID > 0 && (input.AfterID > latestID || (oldestID > 0 && input.AfterID < oldestID-1)) {
		replay.ResetRequired = true
		return replay, nil
	}
	for _, event := range s.events {
		if event.ID <= input.AfterID {
			continue
		}
		replay.Events = append(replay.Events, event)
		if len(replay.Events) == input.Limit {
			break
		}
	}
	return replay, nil
}

func (s *recordingEventApplication) append(event domain.TaskEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *recordingEventApplication) firstAfterID() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[0].AfterID
}

package httpapi

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"product-agent-workbench/internal/app"
)

const replayResetReason = "cursor_outside_retained_window"

// handleEvents 把持久化 TaskEvent 投影成一条可断线续传的 SSE 流。
//
// 前端类比：初次 replay 相当于先恢复 query cache，后面的轮询相当于订阅增量更新。
// handler 本身不保存客户端进度；浏览器断线后通过 Last-Event-ID 把进度重新带回来。
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	afterID, err := parseLastEventID(r.Header.Get("Last-Event-ID"))
	if err != nil {
		writeError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, fmt.Errorf("SSE response writer 不支持 flush"))
		return
	}

	// 第一次数据库读取放在提交 SSE 响应头之前，失败时仍能返回普通 JSON 错误。
	replay, err := s.events.ReplayTaskEvents(r.Context(), app.ReplayTaskEventsInput{
		AfterID: afterID,
		Limit:   s.eventReplayLimit,
	})
	if err != nil {
		writeError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	// drainReplay 会连续翻页直到追上数据库末尾，避免历史事件超过一页时等待轮询周期。
	drainReplay := func(current app.TaskEventReplay) (int64, bool) {
		for {
			if current.ResetRequired {
				reset := ReplayResetResponse{
					Reason:        replayResetReason,
					OldestEventID: current.OldestID,
					LatestEventID: current.LatestID,
				}
				if err := writeSSEEvent(w, current.LatestID, "replay-reset", reset); err != nil {
					return afterID, false
				}
				flusher.Flush()
				return current.LatestID, false
			}

			for _, event := range current.Events {
				if err := writeSSEEvent(w, event.ID, "task-event", newTaskEventResponse(event)); err != nil {
					return afterID, false
				}
				afterID = event.ID
			}
			if len(current.Events) > 0 {
				flusher.Flush()
			}
			if len(current.Events) < s.eventReplayLimit {
				return afterID, true
			}

			next, replayErr := s.events.ReplayTaskEvents(r.Context(), app.ReplayTaskEventsInput{
				AfterID: afterID,
				Limit:   s.eventReplayLimit,
			})
			if replayErr != nil {
				// SSE 响应已经提交，不能再混入 JSON 错误；记录后关闭连接，浏览器会重连。
				log.Printf("replay SSE events after id %d: %v", afterID, replayErr)
				return afterID, false
			}
			current = next
		}
	}

	if _, keepOpen := drainReplay(replay); !keepOpen {
		return
	}

	pollTicker := time.NewTicker(s.eventPollInterval)
	defer pollTicker.Stop()
	heartbeatTicker := time.NewTicker(s.eventHeartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeatTicker.C:
			// SSE 注释不会触发前端 message，也不会推进 Last-Event-ID，只用于保持连接活跃。
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-pollTicker.C:
			next, replayErr := s.events.ReplayTaskEvents(r.Context(), app.ReplayTaskEventsInput{
				AfterID: afterID,
				Limit:   s.eventReplayLimit,
			})
			if replayErr != nil {
				log.Printf("poll SSE events after id %d: %v", afterID, replayErr)
				return
			}
			var keepOpen bool
			afterID, keepOpen = drainReplay(next)
			if !keepOpen {
				return
			}
		}
	}
}

func parseLastEventID(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 0 {
		return 0, fmt.Errorf("%w: Last-Event-ID", ErrInvalidRequest)
	}
	return id, nil
}

// writeSSEEvent 编码一个完整帧。data 先经 JSON 编码，所以换行会被转义，不会注入伪造帧。
func writeSSEEvent(w http.ResponseWriter, id int64, eventName string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("编码 SSE data: %w", err)
	}
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", id, eventName, payload); err != nil {
		return fmt.Errorf("写入 SSE event: %w", err)
	}
	return nil
}

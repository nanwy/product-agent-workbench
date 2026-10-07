package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

const maxTaskEventMessageBytes = 32 << 10

var (
	bearerPattern     = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/-]+=*`)
	credentialPattern = regexp.MustCompile(`(?i)(token|password|secret|authorization)(\s*[:=]\s*)([^\s,;]+)`)
)

type turnCompletion struct {
	Status  string
	Message string
}

type turnObserver struct {
	emit func(ctx context.Context, eventType string, payload []byte) error

	mu           sync.Mutex
	turnID       string
	textByItem   map[string]*strings.Builder
	textOrder    []string
	completion   chan turnCompletion
	completeOnce sync.Once
}

func newTurnObserver(
	emit func(ctx context.Context, eventType string, payload []byte) error,
) *turnObserver {
	return &turnObserver{
		emit:       emit,
		textByItem: make(map[string]*strings.Builder),
		completion: make(chan turnCompletion, 1),
	}
}

func (o *turnObserver) Handle(ctx context.Context, notification Notification) error {
	switch notification.Method {
	case "turn/started":
		var params turnNotification
		if err := json.Unmarshal(notification.Body, &params); err != nil {
			return fmt.Errorf("解析 turn/started: %w", err)
		}
		o.setTurnID(params.Turn.ID)
		return o.emitJSON(ctx, "codex.turn.started", map[string]string{
			"turnId": params.Turn.ID,
		})

	case "item/agentMessage/delta":
		var params agentMessageDelta
		if err := json.Unmarshal(notification.Body, &params); err != nil {
			return fmt.Errorf("解析 item/agentMessage/delta: %w", err)
		}
		if strings.TrimSpace(params.ItemID) == "" || params.Delta == "" {
			return nil
		}
		o.appendText(params.ItemID, params.Delta)
		return o.emitJSON(ctx, "codex.text.delta", map[string]string{
			"itemId": params.ItemID,
			"turnId": params.TurnID,
			"delta":  redact(params.Delta),
		})

	case "item/started", "item/completed":
		payload, err := normalizeItemNotification(notification.Method, notification.Body)
		if err != nil {
			return err
		}
		return o.emitJSON(ctx, "codex."+strings.ReplaceAll(notification.Method, "/", "."), payload)

	case "turn/completed":
		var params turnNotification
		if err := json.Unmarshal(notification.Body, &params); err != nil {
			return fmt.Errorf("解析 turn/completed: %w", err)
		}
		status := strings.TrimSpace(params.Turn.Status)
		if status == "" {
			status = "failed"
		}
		message := redact(strings.TrimSpace(params.Turn.Error.Message))
		completion := turnCompletion{Status: status, Message: message}
		if err := o.emitJSON(ctx, "codex.turn."+normalizeTurnStatus(status), map[string]string{
			"turnId":  params.Turn.ID,
			"status":  status,
			"message": message,
		}); err != nil {
			return err
		}
		// 完成事件持久化成功后再唤醒 Run，避免 TaskRun 已标记 completed，事件流却缺尾帧。
		o.completeOnce.Do(func() { o.completion <- completion })
		return nil
	}
	return nil
}

func (o *turnObserver) Completion() <-chan turnCompletion {
	return o.completion
}

func (o *turnObserver) TurnID() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.turnID
}

func (o *turnObserver) Summary() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	parts := make([]string, 0, len(o.textOrder))
	for _, itemID := range o.textOrder {
		if text := strings.TrimSpace(o.textByItem[itemID].String()); text != "" {
			parts = append(parts, text)
		}
	}
	return truncate(redact(strings.Join(parts, "\n")), maxTaskEventMessageBytes)
}

func (o *turnObserver) setTurnID(turnID string) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	o.mu.Lock()
	o.turnID = turnID
	o.mu.Unlock()
}

func (o *turnObserver) appendText(itemID, delta string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	builder, ok := o.textByItem[itemID]
	if !ok {
		builder = &strings.Builder{}
		o.textByItem[itemID] = builder
		o.textOrder = append(o.textOrder, itemID)
	}
	remaining := maxTaskEventMessageBytes - builder.Len()
	if remaining <= 0 {
		return
	}
	_, _ = builder.WriteString(truncate(delta, remaining))
}

func (o *turnObserver) emitJSON(ctx context.Context, eventType string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("编码 %s 事件: %w", eventType, err)
	}
	return o.emit(ctx, eventType, payload)
}

func normalizeItemNotification(method string, body []byte) (map[string]string, error) {
	var params struct {
		TurnID string `json:"turnId"`
		Item   struct {
			ID               string `json:"id"`
			Type             string `json:"type"`
			Status           string `json:"status"`
			Text             string `json:"text"`
			Message          string `json:"message"`
			Command          string `json:"command"`
			AggregatedOutput string `json:"aggregatedOutput"`
		} `json:"item"`
	}
	if err := json.Unmarshal(body, &params); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", method, err)
	}
	message := firstNonBlank(
		params.Item.Message,
		params.Item.Text,
		params.Item.Command,
		params.Item.AggregatedOutput,
	)
	return map[string]string{
		"turnId":   params.TurnID,
		"itemId":   params.Item.ID,
		"itemType": params.Item.Type,
		"status":   params.Item.Status,
		"message":  truncate(redact(message), maxTaskEventMessageBytes),
	}, nil
}

func normalizeTurnStatus(status string) string {
	switch strings.ToLower(status) {
	case "completed":
		return "completed"
	case "cancelled", "canceled", "aborted", "interrupted":
		return "interrupted"
	default:
		return "failed"
	}
}

func redact(value string) string {
	// 先抹掉完整 Bearer 值，再处理 key=value 形式；反过来会只删掉单词 Bearer，
	// 把后面的真实令牌遗留在文本中。
	value = bearerPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	return credentialPattern.ReplaceAllString(value, "$1$2[REDACTED]")
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

type turnNotification struct {
	Turn struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"turn"`
}

type agentMessageDelta struct {
	ItemID string `json:"itemId"`
	TurnID string `json:"turnId"`
	Delta  string `json:"delta"`
}

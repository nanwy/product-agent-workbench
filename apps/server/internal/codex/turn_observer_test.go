package codex

import (
	"context"
	"strings"
	"testing"
)

func TestTurnObserverNormalizesNotificationsAndRedactsSecrets(t *testing.T) {
	type emittedEvent struct {
		eventType string
		payload   string
	}
	emitted := make([]emittedEvent, 0)
	observer := newTurnObserver(func(_ context.Context, eventType string, payload []byte) error {
		emitted = append(emitted, emittedEvent{eventType: eventType, payload: string(payload)})
		return nil
	})
	ctx := context.Background()

	notifications := []Notification{
		{Method: "turn/started", Body: []byte(`{"turn":{"id":"turn-1"}}`)},
		{Method: "item/agentMessage/delta", Body: []byte(`{"turnId":"turn-1","itemId":"item-1","delta":"完成 token=secret-value"}`)},
		{Method: "turn/completed", Body: []byte(`{"turn":{"id":"turn-1","status":"completed"}}`)},
	}
	for _, notification := range notifications {
		if err := observer.Handle(ctx, notification); err != nil {
			t.Fatalf("Handle(%s) error = %v", notification.Method, err)
		}
	}

	completion := <-observer.Completion()
	if completion.Status != "completed" || observer.TurnID() != "turn-1" {
		t.Fatalf("completion=%#v turnID=%q", completion, observer.TurnID())
	}
	if summary := observer.Summary(); strings.Contains(summary, "secret-value") || !strings.Contains(summary, "[REDACTED]") {
		t.Fatalf("Summary() did not redact secret: %q", summary)
	}
	if len(emitted) != 3 || emitted[0].eventType != "codex.turn.started" ||
		emitted[1].eventType != "codex.text.delta" || emitted[2].eventType != "codex.turn.completed" {
		t.Fatalf("emitted events = %#v", emitted)
	}
}

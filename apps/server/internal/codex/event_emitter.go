package codex

import (
	"context"
	"fmt"
	"sync"
	"time"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/runner"
)

type eventRequest struct {
	ctx       context.Context
	eventType string
	payload   []byte
	ack       chan error
}

// eventEmitter 是 TaskEvent 的单写者：只有它分配 Sequence 并调用 EventSink。
// 这样主流程产生的 session 事件和读循环产生的通知不会发生乱序。
type eventEmitter struct {
	taskRunID string
	sink      runner.EventSink
	clock     func() time.Time
	requests  chan eventRequest
	stop      chan struct{}
	done      chan struct{}

	closeOnce sync.Once
}

func newEventEmitter(taskRunID string, sink runner.EventSink, clock func() time.Time) *eventEmitter {
	emitter := &eventEmitter{
		taskRunID: taskRunID,
		sink:      sink,
		clock:     clock,
		requests:  make(chan eventRequest),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	go emitter.loop()
	return emitter
}

func (e *eventEmitter) Emit(ctx context.Context, eventType string, payload []byte) error {
	request := eventRequest{
		ctx:       ctx,
		eventType: eventType,
		payload:   append([]byte(nil), payload...),
		ack:       make(chan error, 1),
	}
	select {
	case e.requests <- request:
	case <-ctx.Done():
		return ctx.Err()
	case <-e.stop:
		return fmt.Errorf("TaskEvent emitter 已关闭")
	}

	select {
	case err := <-request.ack:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-e.stop:
		return fmt.Errorf("TaskEvent emitter 已关闭")
	}
}

func (e *eventEmitter) Close() {
	e.closeOnce.Do(func() {
		close(e.stop)
		<-e.done
	})
}

func (e *eventEmitter) loop() {
	defer close(e.done)
	var sequence int64
	var terminalErr error
	for {
		var request eventRequest
		select {
		case <-e.stop:
			return
		case request = <-e.requests:
		}
		if terminalErr != nil {
			request.ack <- terminalErr
			continue
		}

		sequence++
		event := domain.TaskEvent{
			TaskRunID: e.taskRunID,
			Sequence:  sequence,
			Type:      request.eventType,
			Payload:   request.payload,
			CreatedAt: e.clock().UTC(),
		}
		if err := e.sink(request.ctx, event); err != nil {
			terminalErr = fmt.Errorf("保存 TaskRun %q 的事件 %d: %w", e.taskRunID, sequence, err)
			request.ack <- terminalErr
			continue
		}
		request.ack <- nil
	}
}

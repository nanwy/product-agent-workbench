package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
	"product-agent-workbench/internal/store/memory"
)

func TestApplication_BeginConversationTurn(t *testing.T) {
	t.Run("建立 running Turn 和连续的消息骨架", func(t *testing.T) {
		storeAdapter := memory.New()
		createdAt := time.Date(2026, time.October, 9, 10, 0, 0, 0, time.UTC)
		startedAt := createdAt.Add(time.Minute)
		clockValues := []time.Time{createdAt, startedAt}
		application, err := app.New(app.Dependencies{
			Store: storeAdapter,
			// 类型前缀由 Application 添加，所以固定 token 也能得到不同种类的可读 ID。
			NewID: func() string { return "id" },
			Now: func() time.Time {
				value := clockValues[0]
				clockValues = clockValues[1:]
				return value
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		requirement, err := application.CreateRequirement(ctx(), app.CreateRequirementInput{
			ProjectID: "project-1",
			Title:     "迁移 Agent 对话",
		})
		if err != nil {
			t.Fatal(err)
		}

		got, err := application.BeginConversationTurn(ctx(), app.BeginConversationTurnInput{
			RequirementID:    "  " + requirement.ID + "  ",
			ExpectedRevision: 1,
			UserContent:      "  请先澄清登录页范围  ",
		})
		if err != nil {
			t.Fatalf("BeginConversationTurn() error = %v", err)
		}

		if got.Conversation.Status != domain.ConversationStatusRunning ||
			got.Conversation.Revision != 2 || !got.Conversation.UpdatedAt.Equal(startedAt) {
			t.Fatalf("Conversation = %#v", got.Conversation)
		}
		if got.Turn.ID != "turn-id" || got.Turn.Sequence != 1 ||
			got.Turn.Status != domain.ConversationTurnRunning {
			t.Fatalf("Turn = %#v", got.Turn)
		}
		if got.UserMessage.Sequence != 2 || got.UserMessage.Segments[0].Content != "请先澄清登录页范围" {
			t.Fatalf("UserMessage = %#v", got.UserMessage)
		}
		if got.AssistantMessage.Sequence != 3 ||
			got.AssistantMessage.Status != domain.ConversationMessageStreaming ||
			len(got.AssistantMessage.Segments) != 0 {
			t.Fatalf("AssistantMessage = %#v", got.AssistantMessage)
		}

		persistedTurns, err := storeAdapter.ListConversationTurns(ctx(), got.Conversation.ID)
		if err != nil || len(persistedTurns) != 1 || persistedTurns[0] != got.Turn {
			t.Fatalf("persisted turns = %#v, error = %v", persistedTurns, err)
		}
		persistedMessages, err := storeAdapter.ListConversationMessages(ctx(), got.Conversation.ID, 0)
		if err != nil || len(persistedMessages) != 3 {
			t.Fatalf("persisted messages = %#v, error = %v", persistedMessages, err)
		}
	})

	t.Run("无效输入和旧 revision 不生成运行事实", func(t *testing.T) {
		storeAdapter := memory.New()
		generatedIDs := 0
		readTimes := 0
		application, err := app.New(app.Dependencies{
			Store: storeAdapter,
			NewID: func() string {
				generatedIDs++
				return "id"
			},
			Now: func() time.Time {
				readTimes++
				return time.Date(2026, time.October, 9, 10, 0, 0, 0, time.UTC)
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := application.CreateRequirement(ctx(), app.CreateRequirementInput{
			ProjectID: "project-1", Title: "并发保护",
		}); err != nil {
			t.Fatal(err)
		}
		createIDCalls, createClockCalls := generatedIDs, readTimes

		invalidInputs := []app.BeginConversationTurnInput{
			{RequirementID: " ", ExpectedRevision: 1, UserContent: "回答"},
			{RequirementID: "id", ExpectedRevision: 0, UserContent: "回答"},
			{RequirementID: "id", ExpectedRevision: 1, UserContent: " \t "},
			{RequirementID: "id", ExpectedRevision: 1, UserContent: strings.Repeat("界", 4001)},
		}
		for _, input := range invalidInputs {
			if _, err := application.BeginConversationTurn(ctx(), input); !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("BeginConversationTurn(%#v) error = %v", input, err)
			}
		}
		if generatedIDs != createIDCalls || readTimes != createClockCalls {
			t.Fatalf("无效输入后 ID 调用=%d、时钟调用=%d", generatedIDs, readTimes)
		}

		if _, err := application.BeginConversationTurn(ctx(), app.BeginConversationTurnInput{
			RequirementID: "id", ExpectedRevision: 2, UserContent: "旧页面提交",
		}); !errors.Is(err, store.ErrConversationConflict) {
			t.Fatalf("BeginConversationTurn(stale) error = %v", err)
		}
		if generatedIDs != createIDCalls || readTimes != createClockCalls {
			t.Fatalf("revision 冲突后 ID 调用=%d、时钟调用=%d", generatedIDs, readTimes)
		}
	})

	t.Run("取消的 context 不读取 Store 或生成 ID", func(t *testing.T) {
		generatedIDs := 0
		readTimes := 0
		application, err := app.New(app.Dependencies{
			Store: memory.New(),
			NewID: func() string {
				generatedIDs++
				return "id"
			},
			Now: func() time.Time {
				readTimes++
				return time.Now()
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = application.BeginConversationTurn(cancelled, app.BeginConversationTurnInput{
			RequirementID: "req-1", ExpectedRevision: 1, UserContent: "回答",
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("BeginConversationTurn(cancelled) error = %v", err)
		}
		if generatedIDs != 0 || readTimes != 0 {
			t.Fatalf("取消请求调用 NewID=%d、Now=%d", generatedIDs, readTimes)
		}
	})
}

func TestApplication_ApplyAndCompleteConversationTurn(t *testing.T) {
	application, storeAdapter, requirement, started := runningConversationFixture(t)

	// 两个 delta 使用相同 itemId，应该追加到同一个 text segment，而不是产生两条消息。
	for _, payload := range []string{
		`{"itemId":"agent-message-1","delta":"共同"}`,
		`{"itemId":"agent-message-1","delta":"理解已形成"}`,
	} {
		if _, err := application.ApplyConversationEvent(ctx(), app.ApplyConversationEventInput{
			RequirementID: requirement.ID, TurnID: started.Turn.ID,
			ExpectedRevision: started.Conversation.Revision,
			EventType:        app.ConversationEventTextDelta, Payload: []byte(payload),
		}); err != nil {
			t.Fatalf("ApplyConversationEvent(delta) error = %v", err)
		}
	}

	// item.started 和 item.completed 使用相同 itemId，应该 upsert 同一个 activity segment。
	if _, err := application.ApplyConversationEvent(ctx(), app.ApplyConversationEventInput{
		RequirementID: requirement.ID, TurnID: started.Turn.ID,
		ExpectedRevision: started.Conversation.Revision,
		EventType:        app.ConversationEventItemStarted,
		Payload:          []byte(`{"itemId":"command-1","itemType":"commandExecution","status":"inProgress","message":"读取项目"}`),
	}); err != nil {
		t.Fatal(err)
	}
	streaming, err := application.ApplyConversationEvent(ctx(), app.ApplyConversationEventInput{
		RequirementID: requirement.ID, TurnID: started.Turn.ID,
		ExpectedRevision: started.Conversation.Revision,
		EventType:        app.ConversationEventItemCompleted,
		Payload:          []byte(`{"itemId":"command-1","itemType":"commandExecution","status":"completed","message":"读取完成"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(streaming.Segments) != 2 || streaming.Segments[0].Content != "共同理解已形成" ||
		streaming.Segments[1].Status != domain.ConversationSegmentCompleted ||
		streaming.Segments[1].Detail != "读取完成" {
		t.Fatalf("folded assistant = %#v", streaming)
	}

	finished, err := application.CompleteConversationTurn(ctx(), app.FinishConversationTurnInput{
		RequirementID: requirement.ID, TurnID: started.Turn.ID,
		ExpectedRevision: started.Conversation.Revision, ReadyForConfirmation: true,
	})
	if err != nil {
		t.Fatalf("CompleteConversationTurn() error = %v", err)
	}
	if finished.Conversation.Status != domain.ConversationStatusAwaitingConfirmation ||
		finished.Conversation.Revision != started.Conversation.Revision+1 ||
		finished.Turn.Status != domain.ConversationTurnCompleted ||
		finished.AssistantMessage.Status != domain.ConversationMessageCompleted {
		t.Fatalf("finished = %#v", finished)
	}

	// 终态提交后，迟到事件不能把 completed 消息改回 streaming。
	_, err = application.ApplyConversationEvent(ctx(), app.ApplyConversationEventInput{
		RequirementID: requirement.ID, TurnID: started.Turn.ID,
		ExpectedRevision: finished.Conversation.Revision,
		EventType:        app.ConversationEventTextDelta,
		Payload:          []byte(`{"itemId":"late","delta":"迟到"}`),
	})
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("ApplyConversationEvent(late) error = %v", err)
	}

	persisted, err := storeAdapter.GetConversationTurn(ctx(), started.Turn.ID)
	if err != nil || persisted.Status != domain.ConversationTurnCompleted {
		t.Fatalf("persisted Turn = %#v, %v", persisted, err)
	}
}

func TestApplication_FailAndCancelConversationTurn(t *testing.T) {
	t.Run("失败保留已有消息片段和可展示原因", func(t *testing.T) {
		application, _, requirement, started := runningConversationFixture(t)
		if _, err := application.ApplyConversationEvent(ctx(), app.ApplyConversationEventInput{
			RequirementID: requirement.ID, TurnID: started.Turn.ID,
			ExpectedRevision: started.Conversation.Revision,
			EventType:        app.ConversationEventTextDelta,
			Payload:          []byte(`{"itemId":"agent-1","delta":"已经分析一部分"}`),
		}); err != nil {
			t.Fatal(err)
		}
		failed, err := application.FailConversationTurn(ctx(), app.FailConversationTurnInput{
			RequirementID: requirement.ID, TurnID: started.Turn.ID,
			ExpectedRevision: started.Conversation.Revision, Reason: "  Codex 连接中断  ",
		})
		if err != nil {
			t.Fatal(err)
		}
		if failed.Conversation.Status != domain.ConversationStatusFailed ||
			failed.Conversation.Error != "Codex 连接中断" ||
			failed.Turn.Status != domain.ConversationTurnFailed ||
			failed.AssistantMessage.Status != domain.ConversationMessageFailed ||
			failed.AssistantMessage.Segments[0].Content != "已经分析一部分" {
			t.Fatalf("failed = %#v", failed)
		}
	})

	t.Run("取消同时结束三份运行态", func(t *testing.T) {
		application, _, requirement, started := runningConversationFixture(t)
		cancelled, err := application.CancelConversationTurn(
			ctx(), requirement.ID, started.Turn.ID, started.Conversation.Revision,
		)
		if err != nil {
			t.Fatal(err)
		}
		if cancelled.Conversation.Status != domain.ConversationStatusCancelled ||
			cancelled.Turn.Status != domain.ConversationTurnCancelled ||
			cancelled.AssistantMessage.Status != domain.ConversationMessageCancelled {
			t.Fatalf("cancelled = %#v", cancelled)
		}
	})
}

func TestApplication_ApplyConversationEventRejectsInvalidPayload(t *testing.T) {
	application, _, requirement, started := runningConversationFixture(t)
	tests := []app.ApplyConversationEventInput{
		{RequirementID: requirement.ID, TurnID: started.Turn.ID, ExpectedRevision: 2, EventType: "unknown", Payload: []byte(`{}`)},
		{RequirementID: requirement.ID, TurnID: started.Turn.ID, ExpectedRevision: 2, EventType: app.ConversationEventTextDelta, Payload: []byte(`not-json`)},
		{RequirementID: requirement.ID, TurnID: started.Turn.ID, ExpectedRevision: 2, EventType: app.ConversationEventTextDelta, Payload: []byte(`{"itemId":"","delta":"text"}`)},
	}
	for _, input := range tests {
		if _, err := application.ApplyConversationEvent(ctx(), input); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("ApplyConversationEvent(%#v) error = %v", input, err)
		}
	}
}

func runningConversationFixture(
	t *testing.T,
) (*app.Application, store.Store, domain.Requirement, app.ConversationTurnStart) {
	t.Helper()
	createdAt := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	times := []time.Time{createdAt, createdAt.Add(time.Minute), createdAt.Add(2 * time.Minute)}
	storeAdapter := memory.New()
	application, err := app.New(app.Dependencies{
		Store: storeAdapter,
		NewID: func() string { return "fixture" },
		Now: func() time.Time {
			value := times[0]
			times = times[1:]
			return value
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	requirement, err := application.CreateRequirement(ctx(), app.CreateRequirementInput{
		ProjectID: "project-1", Title: "对话事件归一化",
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := application.BeginConversationTurn(ctx(), app.BeginConversationTurnInput{
		RequirementID: requirement.ID, ExpectedRevision: 1, UserContent: "开始分析",
	})
	if err != nil {
		t.Fatal(err)
	}
	return application, storeAdapter, requirement, started
}

func ctx() context.Context {
	return context.Background()
}

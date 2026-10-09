package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
)

func runConversationStoreContract(t *testing.T, factory storeFactory) {
	t.Helper()

	t.Run("对话头使用 Requirement 唯一约束和 revision 乐观锁", func(t *testing.T) {
		ctx := context.Background()
		subject, conversation := createConversationFixture(t, factory)

		got, err := subject.GetConversationByRequirement(ctx, conversation.RequirementID)
		if err != nil || got.ID != conversation.ID || got.Revision != 1 {
			t.Fatalf("GetConversationByRequirement() = %#v, %v", got, err)
		}
		duplicate := conversation
		duplicate.ID = "conversation-duplicate"
		if err := subject.CreateConversation(ctx, duplicate); !errors.Is(err, store.ErrAlreadyExists) {
			t.Fatalf("CreateConversation(duplicate requirement) error = %v", err)
		}

		conversation.Status = domain.ConversationStatusRunning
		conversation.UpdatedAt = conversation.UpdatedAt.Add(time.Second)
		revision, err := subject.SaveConversation(ctx, conversation, 1)
		if err != nil || revision != 2 {
			t.Fatalf("SaveConversation() = (%d, %v), want (2, nil)", revision, err)
		}
		if _, err := subject.SaveConversation(ctx, conversation, 1); !errors.Is(err, store.ErrConversationConflict) {
			t.Fatalf("SaveConversation(stale) error = %v", err)
		}
	})

	t.Run("Turn 连续追加且同一对话只能运行一个", func(t *testing.T) {
		ctx := context.Background()
		subject, conversation := createConversationFixture(t, factory)
		turn := conversationTurnFixture(conversation.ID, "turn-1", 1, conversation.CreatedAt)
		if err := subject.CreateConversationTurn(ctx, turn); err != nil {
			t.Fatalf("CreateConversationTurn() error = %v", err)
		}
		second := conversationTurnFixture(conversation.ID, "turn-2", 2, conversation.CreatedAt.Add(time.Second))
		if err := subject.CreateConversationTurn(ctx, second); !errors.Is(err, store.ErrAlreadyExists) {
			t.Fatalf("CreateConversationTurn(active duplicate) error = %v", err)
		}

		finishedAt := turn.StartedAt.Add(time.Second)
		turn.Status = domain.ConversationTurnCompleted
		turn.FinishedAt = &finishedAt
		if err := subject.SaveConversationTurn(ctx, turn, domain.ConversationTurnRunning); err != nil {
			t.Fatalf("SaveConversationTurn() error = %v", err)
		}
		if err := subject.SaveConversationTurn(ctx, turn, domain.ConversationTurnRunning); !errors.Is(err, store.ErrConversationTurnConflict) {
			t.Fatalf("SaveConversationTurn(stale) error = %v", err)
		}
		if err := subject.CreateConversationTurn(ctx, second); err != nil {
			t.Fatalf("CreateConversationTurn(second) error = %v", err)
		}
		turns, err := subject.ListConversationTurns(ctx, conversation.ID)
		if err != nil || len(turns) != 2 || turns[0].ID != turn.ID || turns[1].ID != second.ID {
			t.Fatalf("ListConversationTurns() = %#v, %v", turns, err)
		}
	})

	t.Run("消息批次原子追加并允许流式快照使用状态 CAS 收口", func(t *testing.T) {
		ctx := context.Background()
		subject, conversation := createConversationFixture(t, factory)
		turn := conversationTurnFixture(conversation.ID, "turn-1", 1, conversation.CreatedAt)
		if err := subject.CreateConversationTurn(ctx, turn); err != nil {
			t.Fatal(err)
		}
		messages := []domain.ConversationMessage{
			{
				ID: "message-1", ConversationID: conversation.ID, Sequence: 1,
				Role: domain.ConversationRoleUser, Phase: domain.ConversationPhaseGrilling,
				Status: domain.ConversationMessageCompleted, RecordedAt: conversation.CreatedAt,
				Segments: []domain.ConversationSegment{{
					ID: "segment-1", Type: domain.ConversationSegmentText, Content: "我要修改登录页",
				}},
			},
			{
				ID: "message-2", ConversationID: conversation.ID, TurnID: turn.ID, Sequence: 2,
				Role: domain.ConversationRoleAssistant, Phase: domain.ConversationPhaseGrilling,
				Status: domain.ConversationMessageStreaming, RecordedAt: conversation.CreatedAt.Add(time.Second),
				Segments: []domain.ConversationSegment{},
			},
		}
		if err := subject.AppendConversationMessages(ctx, messages); err != nil {
			t.Fatalf("AppendConversationMessages() error = %v", err)
		}

		messages[1].Status = domain.ConversationMessageCompleted
		messages[1].Segments = []domain.ConversationSegment{{
			ID: "segment-2", Type: domain.ConversationSegmentText, Content: "请确认目标页面",
		}}
		if err := subject.SaveConversationMessage(
			ctx,
			messages[1],
			domain.ConversationMessageStreaming,
		); err != nil {
			t.Fatalf("SaveConversationMessage() error = %v", err)
		}
		if err := subject.SaveConversationMessage(
			ctx,
			messages[1],
			domain.ConversationMessageStreaming,
		); !errors.Is(err, store.ErrConversationMessageConflict) {
			t.Fatalf("SaveConversationMessage(stale) error = %v", err)
		}

		got, err := subject.ListConversationMessages(ctx, conversation.ID, 1)
		if err != nil || len(got) != 1 || got[0].Segments[0].Content != "请确认目标页面" {
			t.Fatalf("ListConversationMessages() = %#v, %v", got, err)
		}
		got[0].Segments[0].Content = "篡改副本"
		again, err := subject.ListConversationMessages(ctx, conversation.ID, 1)
		if err != nil || again[0].Segments[0].Content != "请确认目标页面" {
			t.Fatalf("消息未深拷贝: %#v, %v", again, err)
		}

		invalidBatch := []domain.ConversationMessage{{
			ID: "message-4", ConversationID: conversation.ID, Sequence: 4,
			Role: domain.ConversationRoleSystem, Phase: domain.ConversationPhaseGrilling,
			Status: domain.ConversationMessageCompleted, RecordedAt: conversation.CreatedAt.Add(2 * time.Second),
			Segments: []domain.ConversationSegment{{
				ID: "segment-4", Type: domain.ConversationSegmentGate, Label: "门禁", Detail: "等待确认",
			}},
		}}
		if err := subject.AppendConversationMessages(ctx, invalidBatch); !errors.Is(err, store.ErrInvalidMessageSequence) {
			t.Fatalf("AppendConversationMessages(gap) error = %v", err)
		}

		atomicBatch := []domain.ConversationMessage{
			{
				ID: "message-3", ConversationID: conversation.ID, Sequence: 3,
				Role: domain.ConversationRoleSystem, Phase: domain.ConversationPhaseGrilling,
				Status: domain.ConversationMessageCompleted, RecordedAt: conversation.CreatedAt.Add(2 * time.Second),
				Segments: []domain.ConversationSegment{{
					ID: "segment-3", Type: domain.ConversationSegmentGate, Label: "门禁", Detail: "等待确认",
				}},
			},
			{
				ID: "message-1", ConversationID: conversation.ID, Sequence: 4,
				Role: domain.ConversationRoleSystem, Phase: domain.ConversationPhaseGrilling,
				Status: domain.ConversationMessageCompleted, RecordedAt: conversation.CreatedAt.Add(3 * time.Second),
				Segments: []domain.ConversationSegment{{
					ID: "segment-4", Type: domain.ConversationSegmentGate, Label: "重复", Detail: "整批回滚",
				}},
			},
		}
		if err := subject.AppendConversationMessages(ctx, atomicBatch); !errors.Is(err, store.ErrAlreadyExists) {
			t.Fatalf("AppendConversationMessages(duplicate) error = %v", err)
		}
		all, err := subject.ListConversationMessages(ctx, conversation.ID, 0)
		if err != nil || len(all) != 2 {
			t.Fatalf("失败批次留下了部分消息: %#v, %v", all, err)
		}
	})
}

func runConversationTransactionContract(t *testing.T, factory storeFactory) {
	t.Helper()

	t.Run("原子创建 Requirement 对话和初始消息", func(t *testing.T) {
		ctx := context.Background()
		subject := factory(t)
		input := requirementConversationInputFixture(t)
		if err := subject.CreateRequirementConversation(ctx, input); err != nil {
			t.Fatalf("CreateRequirementConversation() error = %v", err)
		}
		storedRequirement, err := subject.GetRequirement(ctx, input.Requirement.ID)
		if err != nil || storedRequirement != input.Requirement {
			t.Fatalf("GetRequirement() = %#v, %v", storedRequirement, err)
		}
		storedConversation, err := subject.GetConversationByRequirement(ctx, input.Requirement.ID)
		if err != nil || storedConversation != input.Conversation {
			t.Fatalf("GetConversationByRequirement() = %#v, %v", storedConversation, err)
		}
		messages, err := subject.ListConversationMessages(ctx, input.Conversation.ID, 0)
		if err != nil || len(messages) != 1 || messages[0].ID != input.InitialMessage.ID {
			t.Fatalf("ListConversationMessages() = %#v, %v", messages, err)
		}
	})

	t.Run("创建冲突时不留下孤立 Conversation", func(t *testing.T) {
		ctx := context.Background()
		subject := factory(t)
		input := requirementConversationInputFixture(t)
		if err := subject.CreateRequirement(ctx, input.Requirement); err != nil {
			t.Fatal(err)
		}
		if err := subject.CreateRequirementConversation(ctx, input); !errors.Is(err, store.ErrAlreadyExists) {
			t.Fatalf("CreateRequirementConversation(conflict) error = %v", err)
		}
		if _, err := subject.GetConversationByRequirement(ctx, input.Requirement.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("冲突后 Conversation error = %v, want ErrNotFound", err)
		}
	})

	t.Run("最后一步消息冲突时回滚新 Requirement 和 Conversation", func(t *testing.T) {
		ctx := context.Background()
		subject := factory(t)
		first := requirementConversationInputFixture(t)
		if err := subject.CreateRequirementConversation(ctx, first); err != nil {
			t.Fatal(err)
		}

		second := requirementConversationInputFixture(t)
		second.Requirement.ID = "requirement-2"
		second.Requirement.Title = "第二个需求"
		second.Conversation.ID = "conversation-2"
		second.Conversation.RequirementID = second.Requirement.ID
		second.InitialMessage.ConversationID = second.Conversation.ID
		// 故意保留第一组数据的 message ID，让事务在第三次 INSERT 时才失败。
		// 这样能证明前两次 INSERT 也会回滚，而不只是“第一步失败所以什么都没写”。
		if err := subject.CreateRequirementConversation(ctx, second); !errors.Is(err, store.ErrAlreadyExists) {
			t.Fatalf("CreateRequirementConversation(late conflict) error = %v", err)
		}
		if _, err := subject.GetRequirement(ctx, second.Requirement.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("消息冲突后 Requirement error = %v, want ErrNotFound", err)
		}
		if _, err := subject.GetConversationByRequirement(ctx, second.Requirement.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("消息冲突后 Conversation error = %v, want ErrNotFound", err)
		}
	})

	t.Run("原子开始 Turn 并建立两条消息", func(t *testing.T) {
		ctx := context.Background()
		subject := factory(t)
		created := requirementConversationInputFixture(t)
		if err := subject.CreateRequirementConversation(ctx, created); err != nil {
			t.Fatal(err)
		}
		begin := beginConversationTurnInputFixture(t, created.Conversation)
		if err := subject.BeginConversationTurn(ctx, begin); err != nil {
			t.Fatalf("BeginConversationTurn() error = %v", err)
		}
		stored, err := subject.GetConversationByRequirement(ctx, created.Requirement.ID)
		if err != nil || stored != begin.Conversation {
			t.Fatalf("stored Conversation = %#v, %v", stored, err)
		}
		turn, err := subject.GetConversationTurn(ctx, begin.Turn.ID)
		if err != nil || turn != begin.Turn {
			t.Fatalf("stored Turn = %#v, %v", turn, err)
		}
		messages, err := subject.ListConversationMessages(ctx, created.Conversation.ID, 0)
		if err != nil || len(messages) != 3 || messages[1].ID != begin.UserMessage.ID ||
			messages[2].ID != begin.AssistantMessage.ID {
			t.Fatalf("stored messages = %#v, %v", messages, err)
		}
	})

	t.Run("Turn 中途写入冲突时整个事务回滚", func(t *testing.T) {
		ctx := context.Background()
		subject := factory(t)
		created := requirementConversationInputFixture(t)
		if err := subject.CreateRequirementConversation(ctx, created); err != nil {
			t.Fatal(err)
		}
		begin := beginConversationTurnInputFixture(t, created.Conversation)
		// assistant 在事务的最后一步才写入；复用初始消息 ID 可以证明此前 UPDATE/INSERT 会回滚。
		begin.AssistantMessage.ID = created.InitialMessage.ID
		if err := subject.BeginConversationTurn(ctx, begin); !errors.Is(err, store.ErrAlreadyExists) {
			t.Fatalf("BeginConversationTurn(conflict) error = %v", err)
		}
		stored, err := subject.GetConversationByRequirement(ctx, created.Requirement.ID)
		if err != nil || stored != created.Conversation {
			t.Fatalf("回滚后的 Conversation = %#v, %v", stored, err)
		}
		turns, err := subject.ListConversationTurns(ctx, created.Conversation.ID)
		if err != nil || len(turns) != 0 {
			t.Fatalf("回滚后留下 Turn：%#v, %v", turns, err)
		}
		messages, err := subject.ListConversationMessages(ctx, created.Conversation.ID, 0)
		if err != nil || len(messages) != 1 || messages[0].ID != created.InitialMessage.ID {
			t.Fatalf("回滚后消息异常：%#v, %v", messages, err)
		}
	})

	t.Run("旧 revision 不能开始 Turn", func(t *testing.T) {
		ctx := context.Background()
		subject := factory(t)
		created := requirementConversationInputFixture(t)
		if err := subject.CreateRequirementConversation(ctx, created); err != nil {
			t.Fatal(err)
		}
		begin := beginConversationTurnInputFixture(t, created.Conversation)
		begin.ExpectedRevision = 2
		begin.Conversation.Revision = 3
		if err := subject.BeginConversationTurn(ctx, begin); !errors.Is(err, store.ErrConversationConflict) {
			t.Fatalf("BeginConversationTurn(stale) error = %v", err)
		}
		stored, err := subject.GetConversationByRequirement(ctx, created.Requirement.ID)
		if err != nil || stored != created.Conversation {
			t.Fatalf("revision 冲突后 Conversation = %#v, %v", stored, err)
		}
		turns, err := subject.ListConversationTurns(ctx, created.Conversation.ID)
		if err != nil || len(turns) != 0 {
			t.Fatalf("revision 冲突后留下 Turn：%#v, %v", turns, err)
		}
	})

	t.Run("原子结束 Conversation Turn 和 assistant 消息", func(t *testing.T) {
		ctx := context.Background()
		subject := factory(t)
		created := requirementConversationInputFixture(t)
		if err := subject.CreateRequirementConversation(ctx, created); err != nil {
			t.Fatal(err)
		}
		begin := beginConversationTurnInputFixture(t, created.Conversation)
		if err := subject.BeginConversationTurn(ctx, begin); err != nil {
			t.Fatal(err)
		}

		finishedAt := begin.Turn.StartedAt.Add(time.Second)
		conversation, turn, err := begin.Conversation.CompleteTurn(begin.Turn, true, finishedAt)
		if err != nil {
			t.Fatal(err)
		}
		assistant := begin.AssistantMessage
		assistant.Status = domain.ConversationMessageCompleted
		assistant.Segments = []domain.ConversationSegment{{
			ID: "agent-item-1", Type: domain.ConversationSegmentText, Content: "请确认共同理解",
		}}
		// 事件流先以 streaming 状态保存正文；终态事务只能收口这份最新快照，不能覆盖它。
		streamingAssistant := assistant
		streamingAssistant.Status = domain.ConversationMessageStreaming
		if err := subject.SaveConversationMessage(
			ctx,
			streamingAssistant,
			domain.ConversationMessageStreaming,
		); err != nil {
			t.Fatal(err)
		}
		if err := subject.FinishConversationTurn(ctx, store.FinishConversationTurnInput{
			Conversation:            conversation,
			ExpectedRevision:        begin.Conversation.Revision,
			Turn:                    turn,
			ExpectedTurnStatus:      domain.ConversationTurnRunning,
			AssistantMessage:        assistant,
			ExpectedAssistantStatus: domain.ConversationMessageStreaming,
		}); err != nil {
			t.Fatalf("FinishConversationTurn() error = %v", err)
		}

		storedConversation, err := subject.GetConversationByRequirement(ctx, created.Requirement.ID)
		if err != nil || storedConversation != conversation {
			t.Fatalf("finished Conversation = %#v, %v", storedConversation, err)
		}
		storedTurn, err := subject.GetConversationTurn(ctx, turn.ID)
		if err != nil || storedTurn.ID != turn.ID || storedTurn.Status != turn.Status ||
			storedTurn.FinishedAt == nil || turn.FinishedAt == nil ||
			!storedTurn.FinishedAt.Equal(*turn.FinishedAt) {
			t.Fatalf("finished Turn = %#v, %v", storedTurn, err)
		}
		messages, err := subject.ListConversationMessages(ctx, conversation.ID, 0)
		if err != nil || len(messages) != 3 || messages[2].Status != domain.ConversationMessageCompleted ||
			messages[2].Segments[0].Content != "请确认共同理解" {
			t.Fatalf("finished messages = %#v, %v", messages, err)
		}

		// 相同 running 快照不能再次结束已经完成的 Turn。
		if err := subject.FinishConversationTurn(ctx, store.FinishConversationTurnInput{
			Conversation:            conversation,
			ExpectedRevision:        begin.Conversation.Revision,
			Turn:                    turn,
			ExpectedTurnStatus:      domain.ConversationTurnRunning,
			AssistantMessage:        assistant,
			ExpectedAssistantStatus: domain.ConversationMessageStreaming,
		}); !errors.Is(err, store.ErrConversationConflict) {
			t.Fatalf("FinishConversationTurn(stale) error = %v", err)
		}
	})
}

func createConversationFixture(t *testing.T, factory storeFactory) (store.Store, domain.Conversation) {
	t.Helper()
	ctx := context.Background()
	subject := factory(t)
	requirement := requirementFixture()
	if err := subject.CreateRequirement(ctx, requirement); err != nil {
		t.Fatalf("CreateRequirement() error = %v", err)
	}
	now := testTime()
	conversation := domain.Conversation{
		ID: "conversation-1", RequirementID: requirement.ID,
		Phase: domain.ConversationPhaseGrilling, Status: domain.ConversationStatusIdle,
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := subject.CreateConversation(ctx, conversation); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	return subject, conversation
}

func conversationTurnFixture(
	conversationID string,
	id string,
	sequence int64,
	startedAt time.Time,
) domain.ConversationTurn {
	return domain.ConversationTurn{
		ID: id, ConversationID: conversationID, Sequence: sequence,
		Phase: domain.ConversationPhaseGrilling, Status: domain.ConversationTurnRunning,
		StartedAt: startedAt,
	}
}

func requirementConversationInputFixture(t *testing.T) store.CreateRequirementConversationInput {
	t.Helper()
	requirement := requirementFixture()
	now := testTime()
	conversation, err := domain.NewConversation(
		domain.NewConversationInput{ID: "conversation-1", RequirementID: requirement.ID},
		now,
	)
	if err != nil {
		t.Fatalf("NewConversation() error = %v", err)
	}
	message := domain.ConversationMessage{
		ID:             "message-system-1",
		ConversationID: conversation.ID,
		Sequence:       1,
		Role:           domain.ConversationRoleSystem,
		Phase:          domain.ConversationPhaseGrilling,
		Status:         domain.ConversationMessageCompleted,
		Segments: []domain.ConversationSegment{{
			ID:     "segment-gate-1",
			Type:   domain.ConversationSegmentGate,
			Label:  "进入 Grilling",
			Detail: "等待 Agent 建立设计树",
		}},
		RecordedAt: now,
	}
	return store.CreateRequirementConversationInput{
		Requirement:    requirement,
		Conversation:   conversation,
		InitialMessage: message,
	}
}

func beginConversationTurnInputFixture(
	t *testing.T,
	current domain.Conversation,
) store.BeginConversationTurnInput {
	t.Helper()
	startedAt := current.UpdatedAt.Add(time.Second)
	conversation, turn, err := current.BeginTurn(
		domain.BeginConversationTurnInput{ID: "turn-1", Sequence: 1},
		startedAt,
	)
	if err != nil {
		t.Fatalf("BeginTurn() error = %v", err)
	}
	return store.BeginConversationTurnInput{
		Conversation:     conversation,
		ExpectedRevision: current.Revision,
		Turn:             turn,
		UserMessage: domain.ConversationMessage{
			ID:             "message-user-1",
			ConversationID: current.ID,
			TurnID:         turn.ID,
			Sequence:       2,
			Role:           domain.ConversationRoleUser,
			Phase:          current.Phase,
			Status:         domain.ConversationMessageCompleted,
			Segments: []domain.ConversationSegment{{
				ID: "segment-user-1", Type: domain.ConversationSegmentText, Content: "登录页需要增加搜索",
			}},
			RecordedAt: current.UpdatedAt,
		},
		AssistantMessage: domain.ConversationMessage{
			ID:             "message-assistant-1",
			ConversationID: current.ID,
			TurnID:         turn.ID,
			Sequence:       3,
			Role:           domain.ConversationRoleAssistant,
			Phase:          current.Phase,
			Status:         domain.ConversationMessageStreaming,
			Segments:       []domain.ConversationSegment{},
			RecordedAt:     startedAt,
		},
	}
}

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

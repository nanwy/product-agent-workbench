package domain

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestNewConversation(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, time.October, 9, 10, 0, 0, 0, location)
	got, err := NewConversation(
		NewConversationInput{ID: " conversation-1 ", RequirementID: " req-1 "},
		now,
	)
	if err != nil {
		t.Fatalf("NewConversation() error = %v", err)
	}
	wantTime := now.UTC()
	want := Conversation{
		ID:            "conversation-1",
		RequirementID: "req-1",
		Phase:         ConversationPhaseGrilling,
		Status:        ConversationStatusIdle,
		Revision:      1,
		CreatedAt:     wantTime,
		UpdatedAt:     wantTime,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NewConversation() = %#v, want %#v", got, want)
	}
}

func TestNewConversationRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	now := conversationTestTime()
	tests := []struct {
		name    string
		input   NewConversationInput
		now     time.Time
		wantErr error
	}{
		{name: "缺少对话 ID", input: NewConversationInput{RequirementID: "req-1"}, now: now, wantErr: ErrInvalidInput},
		{name: "缺少 Requirement ID", input: NewConversationInput{ID: "conversation-1"}, now: now, wantErr: ErrInvalidInput},
		{name: "创建时间为零值", input: NewConversationInput{ID: "conversation-1", RequirementID: "req-1"}, wantErr: ErrInvalidTime},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewConversation(tt.input, tt.now)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewConversation() error = %v, want %v", err, tt.wantErr)
			}
			if got != (Conversation{}) {
				t.Fatalf("NewConversation() 失败时返回半初始化值：%#v", got)
			}
		})
	}
}

func TestConversationBeginTurn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status ConversationStatus
		ready  bool
		error  string
	}{
		{name: "首次开始", status: ConversationStatusIdle},
		{name: "用户回复", status: ConversationStatusAwaitingUser},
		{name: "修改待确认产物", status: ConversationStatusAwaitingConfirmation, ready: true},
		{name: "失败后重试", status: ConversationStatusFailed, error: "上次失败"},
		{name: "取消后重试", status: ConversationStatusCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			original := conversationFixture(tt.status)
			original.TurnReadyForConfirmation = tt.ready
			original.Error = tt.error
			now := original.UpdatedAt.Add(time.Minute)
			gotConversation, gotTurn, err := original.BeginTurn(
				BeginConversationTurnInput{ID: " turn-2 ", Sequence: 2},
				now,
			)
			if err != nil {
				t.Fatalf("BeginTurn() error = %v", err)
			}
			if gotConversation.Status != ConversationStatusRunning ||
				gotConversation.TurnReadyForConfirmation || gotConversation.Error != "" ||
				gotConversation.Revision != original.Revision+1 ||
				!gotConversation.UpdatedAt.Equal(now) {
				t.Fatalf("BeginTurn() conversation = %#v", gotConversation)
			}
			if gotTurn.ID != "turn-2" || gotTurn.ConversationID != original.ID ||
				gotTurn.Sequence != 2 || gotTurn.Phase != original.Phase ||
				gotTurn.Status != ConversationTurnRunning || !gotTurn.StartedAt.Equal(now) {
				t.Fatalf("BeginTurn() turn = %#v", gotTurn)
			}
		})
	}
}

func TestConversationBeginTurnRejectsInvalidTransition(t *testing.T) {
	t.Parallel()

	original := conversationFixture(ConversationStatusRunning)
	gotConversation, gotTurn, err := original.BeginTurn(
		BeginConversationTurnInput{ID: "turn-2", Sequence: 2},
		original.UpdatedAt.Add(time.Minute),
	)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("BeginTurn() error = %v, want ErrInvalidTransition", err)
	}
	if !reflect.DeepEqual(gotConversation, original) || gotTurn != (ConversationTurn{}) {
		t.Fatalf("BeginTurn() 失败后改变了值：%#v, %#v", gotConversation, gotTurn)
	}
}

func TestConversationBeginTurnRejectsInvalidInputAndTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   BeginConversationTurnInput
		now     func(Conversation) time.Time
		wantErr error
	}{
		{
			name:    "缺少 Turn ID",
			input:   BeginConversationTurnInput{Sequence: 1},
			now:     func(value Conversation) time.Time { return value.UpdatedAt },
			wantErr: ErrInvalidInput,
		},
		{
			name:    "Sequence 不是正数",
			input:   BeginConversationTurnInput{ID: "turn-1"},
			now:     func(value Conversation) time.Time { return value.UpdatedAt },
			wantErr: ErrInvalidInput,
		},
		{
			name:    "开始时间为零值",
			input:   BeginConversationTurnInput{ID: "turn-1", Sequence: 1},
			now:     func(Conversation) time.Time { return time.Time{} },
			wantErr: ErrInvalidTime,
		},
		{
			name:    "开始时间早于当前快照",
			input:   BeginConversationTurnInput{ID: "turn-1", Sequence: 1},
			now:     func(value Conversation) time.Time { return value.UpdatedAt.Add(-time.Nanosecond) },
			wantErr: ErrInvalidTime,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			original := conversationFixture(ConversationStatusIdle)
			gotConversation, gotTurn, err := original.BeginTurn(tt.input, tt.now(original))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("BeginTurn() error = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(gotConversation, original) || gotTurn != (ConversationTurn{}) {
				t.Fatalf("BeginTurn() 失败后改变了值：%#v, %#v", gotConversation, gotTurn)
			}
		})
	}
}

func TestConversationCompleteTurn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		ready      bool
		wantStatus ConversationStatus
	}{
		{name: "等待用户继续", wantStatus: ConversationStatusAwaitingUser},
		{name: "等待用户确认", ready: true, wantStatus: ConversationStatusAwaitingConfirmation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conversation, turn := runningConversationFixture(t)
			now := turn.StartedAt.Add(time.Minute)
			gotConversation, gotTurn, err := conversation.CompleteTurn(turn, tt.ready, now)
			if err != nil {
				t.Fatalf("CompleteTurn() error = %v", err)
			}
			if gotConversation.Status != tt.wantStatus ||
				gotConversation.TurnReadyForConfirmation != tt.ready ||
				gotConversation.Revision != conversation.Revision+1 {
				t.Fatalf("CompleteTurn() conversation = %#v", gotConversation)
			}
			if gotTurn.Status != ConversationTurnCompleted || gotTurn.FinishedAt == nil ||
				!gotTurn.FinishedAt.Equal(now) {
				t.Fatalf("CompleteTurn() turn = %#v", gotTurn)
			}
		})
	}
}

func TestConversationCompleteTurnRejectsImplementationConfirmation(t *testing.T) {
	t.Parallel()

	conversation, turn := runningConversationFixture(t)
	conversation.Phase = ConversationPhaseImplement
	turn.Phase = ConversationPhaseImplement
	gotConversation, gotTurn, err := conversation.CompleteTurn(
		turn,
		true,
		turn.StartedAt.Add(time.Minute),
	)
	assertConversationTransitionFailure(t, gotConversation, gotTurn, err, conversation, turn, ErrInvalidTransition)
}

func TestConversationFailAndCancelTurn(t *testing.T) {
	t.Parallel()

	t.Run("失败保存原因", func(t *testing.T) {
		t.Parallel()
		conversation, turn := runningConversationFixture(t)
		now := turn.StartedAt.Add(time.Minute)
		gotConversation, gotTurn, err := conversation.FailTurn(turn, " Codex 退出 ", now)
		if err != nil {
			t.Fatalf("FailTurn() error = %v", err)
		}
		if gotConversation.Status != ConversationStatusFailed || gotConversation.Error != "Codex 退出" ||
			gotTurn.Status != ConversationTurnFailed || gotTurn.FailureReason != "Codex 退出" {
			t.Fatalf("FailTurn() = %#v, %#v", gotConversation, gotTurn)
		}
	})

	t.Run("取消清理错误", func(t *testing.T) {
		t.Parallel()
		conversation, turn := runningConversationFixture(t)
		conversation.Error = "旧错误"
		now := turn.StartedAt.Add(time.Minute)
		gotConversation, gotTurn, err := conversation.CancelTurn(turn, now)
		if err != nil {
			t.Fatalf("CancelTurn() error = %v", err)
		}
		if gotConversation.Status != ConversationStatusCancelled || gotConversation.Error != "" ||
			gotTurn.Status != ConversationTurnCancelled || gotTurn.FinishedAt == nil {
			t.Fatalf("CancelTurn() = %#v, %#v", gotConversation, gotTurn)
		}
	})
}

func TestConversationTerminalTurnRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(Conversation, ConversationTurn) (Conversation, ConversationTurn)
		now     func(ConversationTurn) time.Time
		wantErr error
	}{
		{
			name: "Turn 属于另一条对话",
			mutate: func(conversation Conversation, turn ConversationTurn) (Conversation, ConversationTurn) {
				turn.ConversationID = "conversation-other"
				return conversation, turn
			},
			wantErr: ErrInvalidTransition,
		},
		{
			name: "Turn 阶段不一致",
			mutate: func(conversation Conversation, turn ConversationTurn) (Conversation, ConversationTurn) {
				turn.Phase = ConversationPhaseToSpec
				return conversation, turn
			},
			wantErr: ErrInvalidTransition,
		},
		{
			name: "结束时间早于开始时间",
			mutate: func(conversation Conversation, turn ConversationTurn) (Conversation, ConversationTurn) {
				return conversation, turn
			},
			now:     func(turn ConversationTurn) time.Time { return turn.StartedAt.Add(-time.Nanosecond) },
			wantErr: ErrInvalidTime,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conversation, turn := runningConversationFixture(t)
			conversation, turn = tt.mutate(conversation, turn)
			now := turn.StartedAt.Add(time.Minute)
			if tt.now != nil {
				now = tt.now(turn)
			}
			gotConversation, gotTurn, err := conversation.CompleteTurn(turn, false, now)
			assertConversationTransitionFailure(
				t,
				gotConversation,
				gotTurn,
				err,
				conversation,
				turn,
				tt.wantErr,
			)
		})
	}

	conversation, turn := runningConversationFixture(t)
	gotConversation, gotTurn, err := conversation.FailTurn(
		turn,
		"   ",
		turn.StartedAt.Add(time.Minute),
	)
	assertConversationTransitionFailure(t, gotConversation, gotTurn, err, conversation, turn, ErrInvalidInput)
}

func TestConversationAdvancePhase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from ConversationPhase
		to   ConversationPhase
	}{
		{name: "Grilling 到 Spec", from: ConversationPhaseGrilling, to: ConversationPhaseToSpec},
		{name: "Spec 到 Tickets", from: ConversationPhaseToSpec, to: ConversationPhaseToTickets},
		{name: "Tickets 到 Implement", from: ConversationPhaseToTickets, to: ConversationPhaseImplement},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			original := conversationFixture(ConversationStatusAwaitingConfirmation)
			original.Phase = tt.from
			original.TurnReadyForConfirmation = true
			now := original.UpdatedAt.Add(time.Minute)
			got, err := original.AdvancePhase(tt.to, now)
			if err != nil {
				t.Fatalf("AdvancePhase() error = %v", err)
			}
			if got.Phase != tt.to || got.Status != ConversationStatusIdle ||
				got.TurnReadyForConfirmation || got.Revision != original.Revision+1 ||
				!got.UpdatedAt.Equal(now) {
				t.Fatalf("AdvancePhase() = %#v", got)
			}
		})
	}
}

func TestConversationAdvancePhaseRejectsInvalidTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status ConversationStatus
		phase  ConversationPhase
		ready  bool
		next   ConversationPhase
	}{
		{name: "尚未等待确认", status: ConversationStatusAwaitingUser, phase: ConversationPhaseGrilling, next: ConversationPhaseToSpec},
		{name: "缺少确认产物", status: ConversationStatusAwaitingConfirmation, phase: ConversationPhaseGrilling, next: ConversationPhaseToSpec},
		{name: "不能跳级", status: ConversationStatusAwaitingConfirmation, phase: ConversationPhaseGrilling, ready: true, next: ConversationPhaseToTickets},
		{name: "Implement 没有下一阶段", status: ConversationStatusAwaitingConfirmation, phase: ConversationPhaseImplement, ready: true, next: ConversationPhaseImplement},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			original := conversationFixture(tt.status)
			original.Phase = tt.phase
			original.TurnReadyForConfirmation = tt.ready
			got, err := original.AdvancePhase(tt.next, original.UpdatedAt.Add(time.Minute))
			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("AdvancePhase() error = %v, want ErrInvalidTransition", err)
			}
			if !reflect.DeepEqual(got, original) {
				t.Fatalf("AdvancePhase() 失败后改变了值：%#v", got)
			}
		})
	}
}

func TestConversationAdvancePhaseRejectsInvalidTime(t *testing.T) {
	t.Parallel()

	original := conversationFixture(ConversationStatusAwaitingConfirmation)
	original.TurnReadyForConfirmation = true
	got, err := original.AdvancePhase(
		ConversationPhaseToSpec,
		original.UpdatedAt.Add(-time.Nanosecond),
	)
	if !errors.Is(err, ErrInvalidTime) {
		t.Fatalf("AdvancePhase() error = %v, want ErrInvalidTime", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("AdvancePhase() 失败后改变了值：%#v", got)
	}
}

func conversationFixture(status ConversationStatus) Conversation {
	now := conversationTestTime()
	return Conversation{
		ID:            "conversation-1",
		RequirementID: "req-1",
		Phase:         ConversationPhaseGrilling,
		Status:        status,
		Revision:      1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func runningConversationFixture(t *testing.T) (Conversation, ConversationTurn) {
	t.Helper()
	original := conversationFixture(ConversationStatusIdle)
	conversation, turn, err := original.BeginTurn(
		BeginConversationTurnInput{ID: "turn-1", Sequence: 1},
		original.UpdatedAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("BeginTurn() fixture error = %v", err)
	}
	return conversation, turn
}

func assertConversationTransitionFailure(
	t *testing.T,
	gotConversation Conversation,
	gotTurn ConversationTurn,
	err error,
	wantConversation Conversation,
	wantTurn ConversationTurn,
	wantErr error,
) {
	t.Helper()
	if !errors.Is(err, wantErr) {
		t.Fatalf("transition error = %v, want %v", err, wantErr)
	}
	if !reflect.DeepEqual(gotConversation, wantConversation) || !reflect.DeepEqual(gotTurn, wantTurn) {
		t.Fatalf("失败迁移改变了值：%#v, %#v", gotConversation, gotTurn)
	}
}

func conversationTestTime() time.Time {
	return time.Date(2026, time.October, 9, 10, 0, 0, 0, time.UTC)
}

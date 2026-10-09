package domain

import (
	"strings"
	"time"
)

// ConversationPhase 是同一条产品对话当前服务的工作流阶段。
//
// 前端类比：它相当于页面状态机的 route/state，而不是某一条聊天消息的类型。
type ConversationPhase string

const (
	ConversationPhaseGrilling  ConversationPhase = "grilling"
	ConversationPhaseToSpec    ConversationPhase = "to-spec"
	ConversationPhaseToTickets ConversationPhase = "to-tickets"
	ConversationPhaseImplement ConversationPhase = "implement"
)

// ConversationStatus 表示整条对话当前是否等待 Agent、用户或确认操作。
type ConversationStatus string

const (
	ConversationStatusIdle                 ConversationStatus = "idle"
	ConversationStatusRunning              ConversationStatus = "running"
	ConversationStatusAwaitingUser         ConversationStatus = "awaiting-user"
	ConversationStatusAwaitingConfirmation ConversationStatus = "awaiting-confirmation"
	ConversationStatusFailed               ConversationStatus = "failed"
	ConversationStatusCancelled            ConversationStatus = "cancelled"
)

// Conversation 是 Requirement 级的逻辑对话头。
//
// 消息和 Turn 单独保存，避免每收到一个流式事件就覆盖一整块大 JSON。Revision 用于后续
// HTTP mutation 的乐观锁；两个并发请求不能用旧快照覆盖较新的阶段或 Thread ID。
type Conversation struct {
	ID                           string
	RequirementID                string
	Phase                        ConversationPhase
	Status                       ConversationStatus
	PlanningThreadID             string
	ImplementationThreadID       string
	PendingUnderstandingRevision string
	TurnReadyForConfirmation     bool
	Error                        string
	Revision                     int64
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
}

// ConversationTurnStatus 表示一次“用户输入 → Agent 完成”的运行状态。
type ConversationTurnStatus string

const (
	ConversationTurnRunning   ConversationTurnStatus = "running"
	ConversationTurnCompleted ConversationTurnStatus = "completed"
	ConversationTurnFailed    ConversationTurnStatus = "failed"
	ConversationTurnCancelled ConversationTurnStatus = "cancelled"
)

// ConversationTurn 保存一次 Agent 调用的稳定边界。
//
// Sequence 是同一 Conversation 内的业务顺序。重试会创建下一条 Turn，而不是覆盖失败记录。
type ConversationTurn struct {
	ID             string
	ConversationID string
	Sequence       int64
	Phase          ConversationPhase
	Status         ConversationTurnStatus
	CodexThreadID  string
	StartedAt      time.Time
	FinishedAt     *time.Time
	FailureReason  string
}

// ConversationRole 是消息发送方；system 用于展示阶段门禁等工作台事实。
type ConversationRole string

const (
	ConversationRoleUser      ConversationRole = "user"
	ConversationRoleAssistant ConversationRole = "assistant"
	ConversationRoleSystem    ConversationRole = "system"
)

// ConversationMessageStatus 描述一条消息是否仍在流式更新。
type ConversationMessageStatus string

const (
	ConversationMessageStreaming ConversationMessageStatus = "streaming"
	ConversationMessageCompleted ConversationMessageStatus = "completed"
	ConversationMessageFailed    ConversationMessageStatus = "failed"
	ConversationMessageCancelled ConversationMessageStatus = "cancelled"
)

// ConversationSegmentType 与原产品界面的五类时间线片段一一对应。
type ConversationSegmentType string

const (
	ConversationSegmentText     ConversationSegmentType = "text"
	ConversationSegmentActivity ConversationSegmentType = "activity"
	ConversationSegmentGate     ConversationSegmentType = "gate"
	ConversationSegmentSpec     ConversationSegmentType = "spec"
	ConversationSegmentTickets  ConversationSegmentType = "tickets"
)

// ConversationSegmentStatus 只用于 activity，表示同一工具活动在页面上的展示状态。
type ConversationSegmentStatus string

const (
	ConversationSegmentRunning   ConversationSegmentStatus = "running"
	ConversationSegmentCompleted ConversationSegmentStatus = "completed"
	ConversationSegmentFailed    ConversationSegmentStatus = "failed"
)

// ConversationSegment 保存页面可直接投影的稳定消息片段。
//
// 不同 Type 使用不同字段：text 使用 Content，activity/gate 使用 Label、Detail，
// spec/tickets 使用 VersionID。阶段 23 再负责把 Codex 原始事件归一化成这些字段。
type ConversationSegment struct {
	ID        string                    `json:"id"`
	Type      ConversationSegmentType   `json:"type"`
	Content   string                    `json:"content,omitempty"`
	Label     string                    `json:"label,omitempty"`
	Detail    string                    `json:"detail,omitempty"`
	Status    ConversationSegmentStatus `json:"status,omitempty"`
	VersionID string                    `json:"versionId,omitempty"`
}

// ConversationMessage 是统一时间线中的一条稳定消息。
//
// TurnID 为空表示消息由工作台状态机产生；用户和 Agent 消息通常属于某个 Turn。
type ConversationMessage struct {
	ID             string
	ConversationID string
	TurnID         string
	Sequence       int64
	Role           ConversationRole
	Phase          ConversationPhase
	Status         ConversationMessageStatus
	Segments       []ConversationSegment
	RecordedAt     time.Time
}

// ValidateConversation 在进入 Store 前检查对话头的基本不变量。
func ValidateConversation(value Conversation) error {
	if isBlankConversationValue(value.ID) || isBlankConversationValue(value.RequirementID) ||
		!validConversationPhase(value.Phase) || !validConversationStatus(value.Status) ||
		value.Revision < 1 {
		return ErrInvalidInput
	}
	if value.CreatedAt.IsZero() || value.UpdatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
		return ErrInvalidTime
	}
	if value.Status == ConversationStatusFailed && isBlankConversationValue(value.Error) {
		return ErrInvalidInput
	}
	return nil
}

// ValidateConversationTurn 检查 Turn 的顺序、状态和终止时间是否一致。
func ValidateConversationTurn(value ConversationTurn) error {
	if isBlankConversationValue(value.ID) || isBlankConversationValue(value.ConversationID) ||
		value.Sequence < 1 || !validConversationPhase(value.Phase) || !validConversationTurnStatus(value.Status) {
		return ErrInvalidInput
	}
	if value.StartedAt.IsZero() {
		return ErrInvalidTime
	}
	if value.Status == ConversationTurnRunning {
		if value.FinishedAt != nil {
			return ErrInvalidTime
		}
		return nil
	}
	if value.FinishedAt == nil || value.FinishedAt.Before(value.StartedAt) {
		return ErrInvalidTime
	}
	if value.Status == ConversationTurnFailed && isBlankConversationValue(value.FailureReason) {
		return ErrInvalidInput
	}
	return nil
}

// ValidateConversationMessage 检查消息身份、顺序和片段形状。
func ValidateConversationMessage(value ConversationMessage) error {
	if isBlankConversationValue(value.ID) || isBlankConversationValue(value.ConversationID) ||
		value.Sequence < 1 || !validConversationRole(value.Role) ||
		!validConversationPhase(value.Phase) || !validConversationMessageStatus(value.Status) {
		return ErrInvalidInput
	}
	if value.RecordedAt.IsZero() {
		return ErrInvalidTime
	}
	if value.Status == ConversationMessageStreaming && isBlankConversationValue(value.TurnID) {
		return ErrInvalidInput
	}
	for _, segment := range value.Segments {
		if err := validateConversationSegment(segment); err != nil {
			return err
		}
	}
	return nil
}

func validateConversationSegment(value ConversationSegment) error {
	if isBlankConversationValue(value.ID) {
		return ErrInvalidInput
	}
	switch value.Type {
	case ConversationSegmentText:
		if isBlankConversationValue(value.Content) {
			return ErrInvalidInput
		}
	case ConversationSegmentActivity, ConversationSegmentGate:
		if isBlankConversationValue(value.Label) || isBlankConversationValue(value.Detail) {
			return ErrInvalidInput
		}
		if value.Type == ConversationSegmentActivity && !validConversationSegmentStatus(value.Status) {
			return ErrInvalidInput
		}
	case ConversationSegmentSpec, ConversationSegmentTickets:
		if isBlankConversationValue(value.VersionID) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func validConversationPhase(value ConversationPhase) bool {
	switch value {
	case ConversationPhaseGrilling, ConversationPhaseToSpec, ConversationPhaseToTickets,
		ConversationPhaseImplement:
		return true
	default:
		return false
	}
}

func validConversationStatus(value ConversationStatus) bool {
	switch value {
	case ConversationStatusIdle, ConversationStatusRunning, ConversationStatusAwaitingUser,
		ConversationStatusAwaitingConfirmation, ConversationStatusFailed, ConversationStatusCancelled:
		return true
	default:
		return false
	}
}

func validConversationTurnStatus(value ConversationTurnStatus) bool {
	switch value {
	case ConversationTurnRunning, ConversationTurnCompleted, ConversationTurnFailed,
		ConversationTurnCancelled:
		return true
	default:
		return false
	}
}

func validConversationRole(value ConversationRole) bool {
	switch value {
	case ConversationRoleUser, ConversationRoleAssistant, ConversationRoleSystem:
		return true
	default:
		return false
	}
}

func validConversationMessageStatus(value ConversationMessageStatus) bool {
	switch value {
	case ConversationMessageStreaming, ConversationMessageCompleted, ConversationMessageFailed,
		ConversationMessageCancelled:
		return true
	default:
		return false
	}
}

func validConversationSegmentStatus(value ConversationSegmentStatus) bool {
	switch value {
	case ConversationSegmentRunning, ConversationSegmentCompleted, ConversationSegmentFailed:
		return true
	default:
		return false
	}
}

func isBlankConversationValue(value string) bool {
	return strings.TrimSpace(value) == ""
}

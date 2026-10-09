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

// NewConversationInput 是创建 Requirement 对话所需的稳定身份。
//
// 前端类比：构造函数相当于 reducer 的 initialState，调用方不能自行拼出任意初始状态。
type NewConversationInput struct {
	ID            string
	RequirementID string
}

// NewConversation 创建处于 Grilling/idle 的逻辑对话。
func NewConversation(input NewConversationInput, now time.Time) (Conversation, error) {
	id := strings.TrimSpace(input.ID)
	requirementID := strings.TrimSpace(input.RequirementID)
	if id == "" || requirementID == "" {
		return Conversation{}, ErrInvalidInput
	}
	if now.IsZero() {
		return Conversation{}, ErrInvalidTime
	}
	now = now.UTC()
	return Conversation{
		ID:            id,
		RequirementID: requirementID,
		Phase:         ConversationPhaseGrilling,
		Status:        ConversationStatusIdle,
		Revision:      1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
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

// BeginConversationTurnInput 保存新 Turn 的身份和对话内顺序。
// Sequence 由应用层根据已持久化历史计算，领域层只检查它是正数。
type BeginConversationTurnInput struct {
	ID       string
	Sequence int64
}

// BeginTurn 开始一次 Agent Turn，并同步把 Conversation 切换为 running。
//
// 这里返回两个新值而不修改 receiver，和前端 reducer 返回新 state 的方式相同。Store 会在
// 23B 中把两个返回值与消息放进同一事务，避免只保存其中一半。
func (conversation Conversation) BeginTurn(
	input BeginConversationTurnInput,
	now time.Time,
) (Conversation, ConversationTurn, error) {
	if err := ValidateConversation(conversation); err != nil {
		return conversation, ConversationTurn{}, err
	}
	if !canBeginConversationTurn(conversation.Status) {
		return conversation, ConversationTurn{}, ErrInvalidTransition
	}
	id := strings.TrimSpace(input.ID)
	if id == "" || input.Sequence < 1 {
		return conversation, ConversationTurn{}, ErrInvalidInput
	}
	if err := validateConversationTransitionTime(conversation, now); err != nil {
		return conversation, ConversationTurn{}, err
	}
	now = now.UTC()
	turn := ConversationTurn{
		ID:             id,
		ConversationID: conversation.ID,
		Sequence:       input.Sequence,
		Phase:          conversation.Phase,
		Status:         ConversationTurnRunning,
		StartedAt:      now,
	}
	conversation.Status = ConversationStatusRunning
	conversation.TurnReadyForConfirmation = false
	conversation.Error = ""
	conversation.Revision++
	conversation.UpdatedAt = now
	return conversation, turn, nil
}

// AttachThread 把 Codex Thread ID 同时记录到逻辑对话和当前 Turn。
// 规划阶段复用 PlanningThreadID，实现阶段复用 ImplementationThreadID。
func (conversation Conversation) AttachThread(
	turn ConversationTurn,
	threadID string,
	now time.Time,
) (Conversation, ConversationTurn, error) {
	if err := validateRunningConversationTurn(conversation, turn, now); err != nil {
		return conversation, turn, err
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return conversation, turn, ErrInvalidInput
	}
	now = now.UTC()
	turn.CodexThreadID = threadID
	if conversation.Phase == ConversationPhaseImplement {
		conversation.ImplementationThreadID = threadID
	} else {
		conversation.PlanningThreadID = threadID
	}
	conversation.Revision++
	conversation.UpdatedAt = now
	return conversation, turn, nil
}

// CompleteTurn 结束成功 Turn，并决定下一步等待用户继续输入还是确认当前产物。
func (conversation Conversation) CompleteTurn(
	turn ConversationTurn,
	readyForConfirmation bool,
	now time.Time,
) (Conversation, ConversationTurn, error) {
	if err := validateRunningConversationTurn(conversation, turn, now); err != nil {
		return conversation, turn, err
	}
	if readyForConfirmation && conversation.Phase == ConversationPhaseImplement {
		return conversation, turn, ErrInvalidTransition
	}
	now = now.UTC()
	turn.Status = ConversationTurnCompleted
	turn.FinishedAt = &now
	turn.FailureReason = ""
	conversation.Status = ConversationStatusAwaitingUser
	if readyForConfirmation {
		conversation.Status = ConversationStatusAwaitingConfirmation
	}
	conversation.TurnReadyForConfirmation = readyForConfirmation
	conversation.Error = ""
	conversation.Revision++
	conversation.UpdatedAt = now
	return conversation, turn, nil
}

// FailTurn 保存可展示的失败原因；失败记录保留，下一次重试会创建新的 Turn。
func (conversation Conversation) FailTurn(
	turn ConversationTurn,
	reason string,
	now time.Time,
) (Conversation, ConversationTurn, error) {
	if err := validateRunningConversationTurn(conversation, turn, now); err != nil {
		return conversation, turn, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return conversation, turn, ErrInvalidInput
	}
	now = now.UTC()
	turn.Status = ConversationTurnFailed
	turn.FinishedAt = &now
	turn.FailureReason = reason
	conversation.Status = ConversationStatusFailed
	conversation.TurnReadyForConfirmation = false
	conversation.Error = reason
	conversation.Revision++
	conversation.UpdatedAt = now
	return conversation, turn, nil
}

// CancelTurn 把正在运行的 Turn 和 Conversation 一起收口为 cancelled。
func (conversation Conversation) CancelTurn(
	turn ConversationTurn,
	now time.Time,
) (Conversation, ConversationTurn, error) {
	if err := validateRunningConversationTurn(conversation, turn, now); err != nil {
		return conversation, turn, err
	}
	now = now.UTC()
	turn.Status = ConversationTurnCancelled
	turn.FinishedAt = &now
	turn.FailureReason = ""
	conversation.Status = ConversationStatusCancelled
	conversation.TurnReadyForConfirmation = false
	conversation.Error = ""
	conversation.Revision++
	conversation.UpdatedAt = now
	return conversation, turn, nil
}

// AdvancePhase 在用户确认当前阶段产物后进入下一个阶段。
//
// 合法顺序固定为 grilling → to-spec → to-tickets → implement，不能跳级或倒退。
func (conversation Conversation) AdvancePhase(
	next ConversationPhase,
	now time.Time,
) (Conversation, error) {
	if err := ValidateConversation(conversation); err != nil {
		return conversation, err
	}
	if conversation.Status != ConversationStatusAwaitingConfirmation ||
		!conversation.TurnReadyForConfirmation {
		return conversation, ErrInvalidTransition
	}
	expected, exists := nextConversationPhase(conversation.Phase)
	if !exists || next != expected {
		return conversation, ErrInvalidTransition
	}
	if err := validateConversationTransitionTime(conversation, now); err != nil {
		return conversation, err
	}
	conversation.Phase = next
	conversation.Status = ConversationStatusIdle
	conversation.TurnReadyForConfirmation = false
	conversation.Error = ""
	conversation.Revision++
	conversation.UpdatedAt = now.UTC()
	return conversation, nil
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

func canBeginConversationTurn(status ConversationStatus) bool {
	switch status {
	case ConversationStatusIdle, ConversationStatusAwaitingUser,
		ConversationStatusAwaitingConfirmation, ConversationStatusFailed, ConversationStatusCancelled:
		return true
	default:
		return false
	}
}

func validateRunningConversationTurn(
	conversation Conversation,
	turn ConversationTurn,
	now time.Time,
) error {
	if err := ValidateConversation(conversation); err != nil {
		return err
	}
	if err := ValidateConversationTurn(turn); err != nil {
		return err
	}
	sameOwner := turn.ConversationID == conversation.ID
	samePhase := turn.Phase == conversation.Phase
	bothRunning := conversation.Status == ConversationStatusRunning &&
		turn.Status == ConversationTurnRunning
	if !sameOwner || !samePhase || !bothRunning {
		return ErrInvalidTransition
	}
	if err := validateConversationTransitionTime(conversation, now); err != nil {
		return err
	}
	if now.Before(turn.StartedAt) {
		return ErrInvalidTime
	}
	return nil
}

func validateConversationTransitionTime(conversation Conversation, now time.Time) error {
	if now.IsZero() || conversation.UpdatedAt.IsZero() || now.Before(conversation.UpdatedAt) {
		return ErrInvalidTime
	}
	return nil
}

func nextConversationPhase(current ConversationPhase) (ConversationPhase, bool) {
	switch current {
	case ConversationPhaseGrilling:
		return ConversationPhaseToSpec, true
	case ConversationPhaseToSpec:
		return ConversationPhaseToTickets, true
	case ConversationPhaseToTickets:
		return ConversationPhaseImplement, true
	default:
		return "", false
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

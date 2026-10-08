package domain

import (
	"strings"
	"time"
)

// GrillingUnderstanding 是 21A 冻结后的共同理解，不是临时输入框状态。
//
// 前端类比：它类似提交表单后由服务端保存的 canonical data；聊天气泡可以很多条，
// 但下游 Spec 只依赖这四个经过确认的稳定字段。
type GrillingUnderstanding struct {
	RequirementID    string
	Summary          string
	DesiredOutcome   string
	ChangeBoundary   string
	AcceptanceSignal string
	ConfirmedAt      time.Time
}

// NormalizeGrillingUnderstanding 清理文本并拒绝不完整的共同理解。
func NormalizeGrillingUnderstanding(value GrillingUnderstanding) (GrillingUnderstanding, error) {
	value.RequirementID = strings.TrimSpace(value.RequirementID)
	value.Summary = strings.TrimSpace(value.Summary)
	value.DesiredOutcome = strings.TrimSpace(value.DesiredOutcome)
	value.ChangeBoundary = strings.TrimSpace(value.ChangeBoundary)
	value.AcceptanceSignal = strings.TrimSpace(value.AcceptanceSignal)
	if value.RequirementID == "" || value.Summary == "" || value.DesiredOutcome == "" ||
		value.ChangeBoundary == "" || value.AcceptanceSignal == "" || value.ConfirmedAt.IsZero() {
		return GrillingUnderstanding{}, ErrInvalidInput
	}
	value.ConfirmedAt = value.ConfirmedAt.UTC()
	return value, nil
}

// SpecVersionStatus 区分可修改候选、已冻结版本和被新草稿替代的历史。
type SpecVersionStatus string

const (
	SpecVersionDraft      SpecVersionStatus = "draft"
	SpecVersionConfirmed  SpecVersionStatus = "confirmed"
	SpecVersionSuperseded SpecVersionStatus = "superseded"
)

// SpecDocument 是 Codex 返回并由应用层校验的结构化规格正文。
type SpecDocument struct {
	Problem                 string
	Solution                string
	UserStories             []string
	ImplementationDecisions []string
	TestingDecisions        []string
	OutOfScope              []string
	FurtherNotes            []string
}

// SpecVersion 是不可覆盖的一版 Spec。修订会创建新版本并把旧草稿标记为 superseded。
type SpecVersion struct {
	ID            string
	RequirementID string
	Version       int
	Status        SpecVersionStatus
	Document      SpecDocument
	CreatedAt     time.Time
	ConfirmedAt   *time.Time
}

// NormalizeSpecDocument 清理 Agent 输出；任何章节为空都不允许发布为草稿。
func NormalizeSpecDocument(value SpecDocument) (SpecDocument, error) {
	value.Problem = strings.TrimSpace(value.Problem)
	value.Solution = strings.TrimSpace(value.Solution)
	value.UserStories = normalizeRequiredList(value.UserStories)
	value.ImplementationDecisions = normalizeRequiredList(value.ImplementationDecisions)
	value.TestingDecisions = normalizeRequiredList(value.TestingDecisions)
	value.OutOfScope = normalizeRequiredList(value.OutOfScope)
	value.FurtherNotes = normalizeRequiredList(value.FurtherNotes)
	if value.Problem == "" || value.Solution == "" || len(value.UserStories) == 0 ||
		len(value.ImplementationDecisions) == 0 || len(value.TestingDecisions) == 0 ||
		len(value.OutOfScope) == 0 || len(value.FurtherNotes) == 0 {
		return SpecDocument{}, ErrInvalidInput
	}
	return value, nil
}

func normalizeRequiredList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

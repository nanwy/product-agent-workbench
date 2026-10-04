package domain

// RequirementStage 表示产品需求在 SDD 工作流中的阶段。
type RequirementStage string

const (
	// RequirementStageGrilling 表示正在澄清共同理解。
	RequirementStageGrilling RequirementStage = "grilling"
	// RequirementStageSpec 表示正在形成或确认版本化 Spec。
	RequirementStageSpec RequirementStage = "spec"
	// RequirementStageTickets 表示正在拆分或确认执行票。
	RequirementStageTickets RequirementStage = "tickets"
	// RequirementStageImplementation 表示已经进入候选实现阶段。
	RequirementStageImplementation RequirementStage = "implementation"
	// RequirementStageAcceptance 表示候选实现正在等待产品验收。
	RequirementStageAcceptance RequirementStage = "acceptance"
	// RequirementStageDelivered 表示已经形成稳定候选提交和 Handoff。
	RequirementStageDelivered RequirementStage = "delivered"
)

// Requirement 是 Go 迁移初期使用的最小聚合头。
//
// 暂时不要把现有 TypeScript Requirement 的所有字段一次性复制过来。每迁移一个用例，
// 再加入该用例真正需要的字段，并用测试说明新增字段的不变量。
type Requirement struct {
	ID        string
	ProjectID string
	Title     string
	Stage     RequirementStage
	Revision  int64
}

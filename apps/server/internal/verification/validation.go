package verification

import (
	"errors"
	"time"
)

var (
	// ErrInvalidEvidence 表示某条验证证据缺少稳定身份、展示名称，或者状态组合不合法。
	//
	// 前端类比：这相当于提交表单前的 schema 校验失败，而不是接口请求失败。
	ErrInvalidEvidence = errors.New("验证证据无效")
	// ErrInvalidDependencies 表示验证器缺少命令执行器、候选检查器或报告仓库。
	ErrInvalidDependencies = errors.New("Verification 依赖无效")
	// ErrInvalidPlan 表示受信任配置中的验证命令缺字段、重复 ID 或包含非法枚举。
	ErrInvalidPlan = errors.New("Verification 计划无效")
	// ErrReportNotFound 表示指定 Requirement 尚未保存验证报告。
	ErrReportNotFound = errors.New("Verification 报告不存在")
)

// Status 是单条验证记录允许出现的三种结果。
//
// 前端类比：它相当于 TypeScript 的联合类型：
//
//	type Status = "verified" | "failed" | "not-run"
//
// Go 不会因为底层类型是 string 就自动限制取值，所以 BuildMatrix 仍需用 switch
// 拒绝未知状态。只声明常量，不等于完成运行时校验。
type Status string

const (
	StatusVerified Status = "verified"
	StatusFailed   Status = "failed"
	StatusNotRun   Status = "not-run"
)

// Source 表示证据由哪类 Adapter 产生。前端可以用它选择图标，但不能根据文案猜来源。
type Source string

const (
	SourceCommand   Source = "command"
	SourceCandidate Source = "candidate"
)

// Evidence 是证据收集 Adapter 返回的一条原始事实。
//
// 可以先把它理解成前端从接口获得的一条 DTO。它只描述“检查发生了什么”，不负责决定
// 整个候选版本能否交付；多条 Evidence 的组合判断由 Matrix 负责。
type Evidence struct {
	// ID 相当于 React 列表的稳定 key。空值或重复值会让同一检查无法可靠追踪。
	ID string `json:"id"`
	// Label 是页面显示给人的检查名称，例如“Go 单元测试”。
	Label string `json:"label"`
	// Status 是机器证据的结果，不能通过 Detail 文案猜测。
	Status Status `json:"status"`
	// Detail 保存可展示的简短结果，例如退出码或检查摘要。
	Detail string `json:"detail"`
	// Reason 只解释为什么没有执行。StatusNotRun 时必须填写。
	Reason string `json:"reason,omitempty"`
	// Required 类似表单字段的 required：该项不是 verified 时必须阻止交付。
	Required bool `json:"required"`
	// Source、Command、ExitCode 和时间把“结论”连回实际执行事实。
	Source      Source    `json:"source"`
	Command     string    `json:"command,omitempty"`
	ExitCode    *int      `json:"exitCode,omitempty"`
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
}

// Matrix 是 UI 和交付门禁共同消费的稳定验证结果。
//
// Records 使用切片而不是 map，因为页面需要保留配置中的展示顺序。BuildMatrix 必须创建
// 新切片，不能把调用方传进来的 evidence 直接保存下来；这和 React 中避免直接复用并修改
// props 数组是同一个边界意识。
type Matrix struct {
	Records []Evidence `json:"records"`
}

// ReportStatus 是整份矩阵的机器结论。
type ReportStatus string

const (
	ReportStatusPassed  ReportStatus = "passed"
	ReportStatusBlocked ReportStatus = "blocked"
)

// Report 是一次不可覆盖的验证尝试。重新验证会创建新报告，而不是修改旧证据。
//
// 前端类比：它像一次 CI run。Matrix 是 job 列表，Status 是整次 run 的聚合状态；旧 run
// 仍保留用于解释“为什么上一轮不能进入验收”。
type Report struct {
	ID                string
	RequirementID     string
	TicketVersionID   string
	CandidateRevision string
	WorkspacePath     string
	Status            ReportStatus
	Matrix            Matrix
	StartedAt         time.Time
	CompletedAt       time.Time
}

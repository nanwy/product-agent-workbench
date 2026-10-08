package app

import (
	"context"
	"errors"
	"time"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/scheduler"
	"product-agent-workbench/internal/store"
	"product-agent-workbench/internal/verification"
)

// ErrInvalidDependencies 表示创建 Application 时缺少必需依赖。
//
// 这是启动期配置错误，不应该映射成面向浏览器的 4xx 响应。main 遇到它时应停止启动。
var ErrInvalidDependencies = errors.New("应用依赖无效")

// ErrSpecGeneratorUnavailable 表示进程没有装配只读 Spec 生成器。
var ErrSpecGeneratorUnavailable = errors.New("Spec 生成器不可用")

// ErrInvalidSpecOutput 表示生成器成功返回文本，但没有形成完整结构化 Spec。
var ErrInvalidSpecOutput = errors.New("Spec 生成结果无效")

// ErrTicketGeneratorUnavailable 表示进程没有装配只读 Tickets 生成器。
var ErrTicketGeneratorUnavailable = errors.New("Ticket 生成器不可用")

// ErrInvalidTicketOutput 表示生成器没有形成合法且无环的 Ticket DAG。
var ErrInvalidTicketOutput = errors.New("Ticket 生成结果无效")

// IDGenerator 生成新的领域实体 ID。
//
// 把变化点作为依赖传入，测试才能使用确定值，生产环境以后可以注入 UUID 生成器。
// 它不是 Store 的职责：Store 只负责拒绝重复 ID，不负责发明 ID。
type IDGenerator func() string

// Clock 返回应用用例发生时的当前时间。
//
// 时间和 ID 一样都是会变化的外部输入。把它放在依赖 seam 上，测试就能固定 CreatedAt，
// 而不是依赖 time.Now() 和模糊的时间范围断言。生产装配时再传入 time.Now。
type Clock func() time.Time

// Dependencies 列出应用用例层真正依赖的模块。
//
// main 包负责提供具体 Adapter；Application 只依赖接口，不知道 SQLite、Codex 或 Git
// 的构造细节。这里只放当前用例真实使用的依赖；Runner 要等到执行用例出现后再加入，
// 不要把未来可能使用的模块预先塞进依赖工具箱。
type Dependencies struct {
	Store           store.Store
	Tickets         VerificationTicketReader
	Verification    DeliveryVerifier
	SpecGenerator   SpecGenerator
	TicketGenerator TicketGenerator
	TicketPublisher TicketPublisher
	NewID           IDGenerator
	Now             Clock
}

// Application 是 HTTP 层调用的用例入口。
//
// HTTP handler 以后只调用这里公开的用例，不直接操作 Store 或领域状态机。这样 HTTP、CLI
// 或测试都能复用相同规则，也避免把业务判断散落到不同入口。
type Application struct {
	store           store.Store
	tickets         VerificationTicketReader
	verification    DeliveryVerifier
	specGenerator   SpecGenerator
	ticketGenerator TicketGenerator
	ticketPublisher TicketPublisher
	newID           IDGenerator
	now             Clock
}

// New 创建应用用例模块。
//
// 实现要点：
//  1. Store、NewID 和 Now 任意一个为 nil，都返回 nil, ErrInvalidDependencies。
//  2. 校验通过后，把依赖保存进 Application 并返回其指针。
//  3. 构造函数不能连接数据库、调用 NewID/Now，也不能启动 goroutine；这些都不是“保存依赖”。
//
// 已完成练习：构造函数校验并保存 Clock，但不会在构造期间读取时间。
func New(dependencies Dependencies) (*Application, error) {
	if dependencies.Store == nil || dependencies.NewID == nil || dependencies.Now == nil {
		return nil, ErrInvalidDependencies
	}
	return &Application{
		store:           dependencies.Store,
		tickets:         dependencies.Tickets,
		verification:    dependencies.Verification,
		specGenerator:   dependencies.SpecGenerator,
		ticketGenerator: dependencies.TicketGenerator,
		ticketPublisher: dependencies.TicketPublisher,
		newID:           dependencies.NewID,
		now:             dependencies.Now,
	}, nil
}

// SpecGenerationInput 只包含生成 Spec 必需的可信事实。
// RepositoryRoot、Codex 命令和 sandbox 权限由 Adapter 的启动配置固定，不接受浏览器输入。
type SpecGenerationInput struct {
	Requirement   domain.Requirement
	Understanding domain.GrillingUnderstanding
	PreviousSpec  *domain.SpecDocument
	Instruction   string
}

// SpecGenerator 隔离“业务用例”和“怎样调用 Codex”。测试可以注入 fake，生产使用只读 Turn。
type SpecGenerator interface {
	Generate(ctx context.Context, input SpecGenerationInput) (domain.SpecDocument, error)
}

// TicketGenerationInput 只携带已经确认的 Spec 和用户本轮修订意图。
type TicketGenerationInput struct {
	Requirement     domain.Requirement
	Spec            domain.SpecVersion
	PreviousVersion *domain.TicketVersion
	Instruction     string
}

// GeneratedTicketPlan 是只读规划 Adapter 返回的 Git 快照和 Ticket DAG。
type GeneratedTicketPlan struct {
	RepositoryRoot    string
	BaseRevision      string
	RequirementBranch string
	Tickets           []domain.Ticket
}

type TicketGenerator interface {
	GenerateTickets(ctx context.Context, input TicketGenerationInput) (GeneratedTicketPlan, error)
}

// TicketPublisher 把已确认版本发布成 Scheduler/Worker 共用的不可变 manifest。
type TicketPublisher interface {
	PublishConfirmedTicketVersion(ctx context.Context, version domain.TicketVersion) error
}

// VerificationTicketReader 提供“这个版本到底有几张票”的可信事实。
// 只检查数据库里已有记录会把缺失 Ticket 误认为全部完成，因此必须同时读取确认 artifact。
type VerificationTicketReader interface {
	ReadConfirmedTicketVersion(
		ctx context.Context,
		requirementID string,
		ticketVersionID string,
	) (scheduler.TicketVersion, error)
}

// DeliveryVerifier 执行并持久化 18B 的验证报告。
type DeliveryVerifier interface {
	Verify(ctx context.Context, request verification.VerifyRequest) (verification.Report, error)
	LatestReport(
		ctx context.Context,
		requirementID string,
		ticketVersionID string,
	) (verification.Report, error)
}

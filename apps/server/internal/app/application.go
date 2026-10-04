package app

import (
	"errors"

	"product-agent-workbench/internal/store"
)

// ErrInvalidDependencies 表示创建 Application 时缺少必需依赖。
//
// 这是启动期配置错误，不应该映射成面向浏览器的 4xx 响应。main 遇到它时应停止启动。
var ErrInvalidDependencies = errors.New("应用依赖无效")

// IDGenerator 生成新的领域实体 ID。
//
// 把变化点作为依赖传入，测试才能使用确定值，生产环境以后可以注入 UUID 生成器。
// 它不是 Store 的职责：Store 只负责拒绝重复 ID，不负责发明 ID。
type IDGenerator func() string

// Dependencies 列出应用用例层真正依赖的模块。
//
// main 包负责提供具体 Adapter；Application 只依赖接口，不知道 SQLite、Codex 或 Git
// 的构造细节。这里只放当前用例真实使用的依赖；Runner 要等到执行用例出现后再加入，
// 不要把未来可能使用的模块预先塞进依赖工具箱。
type Dependencies struct {
	Store store.Store
	NewID IDGenerator
}

// Application 是 HTTP 层调用的用例入口。
//
// HTTP handler 以后只调用这里公开的用例，不直接操作 Store 或领域状态机。这样 HTTP、CLI
// 或测试都能复用相同规则，也避免把业务判断散落到不同入口。
type Application struct {
	store store.Store
	newID IDGenerator
}

// New 创建应用用例模块。
//
// 实现要点：
//  1. Store 和 NewID 任意一个为 nil，都返回 nil, ErrInvalidDependencies。
//  2. 校验通过后，把依赖保存进 Application 并返回其指针。
//  3. 构造函数不能连接数据库、调用 NewID，也不能启动 goroutine；这些都不是“保存依赖”。
func New(dependencies Dependencies) (*Application, error) {
	if dependencies.Store == nil || dependencies.NewID == nil {
		return nil, ErrInvalidDependencies
	}
	return &Application{
		store: dependencies.Store,
		newID: dependencies.NewID,
	}, nil
}

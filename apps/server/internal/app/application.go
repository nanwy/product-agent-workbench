package app

import (
	"errors"

	"product-agent-workbench/internal/runner"
	"product-agent-workbench/internal/store"
)

// ErrNotImplemented 表示应用用例层还没有完成当前学习任务。
var ErrNotImplemented = errors.New("应用用例尚未实现")

// Dependencies 列出应用用例层真正依赖的模块。
//
// main 包负责提供具体 Adapter；Application 只依赖接口，不知道 SQLite、Codex 或 Git
// 的构造细节。随着用例增加，可以添加时钟、ID 生成器等真实变化点，但不要预先塞入工具箱。
type Dependencies struct {
	Store  store.Store
	Runner runner.Runner
}

// Application 是 HTTP 层调用的用例入口。
//
// TODO(学习者):完成内存 Store 后，先实现“创建 Requirement”和“读取 Requirement”两个
// 同步用例。长时间运行的 TaskRun 由调度器拥有，不能绑在 HTTP request context 上。
type Application struct {
	store  store.Store
	runner runner.Runner
}

// New 创建应用用例模块。
//
// TODO(学习者):校验必需依赖不能为 nil。这里仅保存依赖，不连接数据库、不启动 goroutine。
func New(dependencies Dependencies) (*Application, error) {
	return nil, ErrNotImplemented
}

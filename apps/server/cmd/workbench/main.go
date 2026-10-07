// Command workbench 是 Product Agent Workbench 本地控制面的唯一进程入口。
//
// 学习任务：
//  1. 加载进程级配置，产品输入不能成为 Shell 命令。
//  2. 按依赖顺序显式装配 Store、Runner、应用模块和 HTTP 模块。
//  3. 启动 HTTP 服务，并在收到 SIGINT/SIGTERM 时优雅退出。
//  4. 保持入口足够薄，不在 main 中编写领域规则。
//
// 练习 17C 的纯依赖装配保留在 runtime.go；练习 17D 由 lifecycle.go 打开 SQLite、创建
// 真实 Adapter、启动 HTTP/Supervisor，并在这里把操作系统退出信号转换成 context 取消。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	config, err := loadProcessConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 Workbench 配置失败: %v\n", err)
		os.Exit(1)
	}

	// 前端类比：SignalContext 类似绑定到整个应用的 AbortController。SIGINT/SIGTERM
	// 到来时只需 cancel 一次，HTTP、Worker、数据库调用和 Codex 子进程会沿调用链停止。
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	if err := run(ctx, config); err != nil {
		fmt.Fprintf(os.Stderr, "Workbench 退出: %v\n", err)
		os.Exit(1)
	}
}

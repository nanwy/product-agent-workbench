// Package runner 负责任务调度和执行生命周期，不负责 Codex 或 Git 的实现细节。
//
// 对外接口保持很小：接收一次运行描述和事件接收器，在遵守取消与超时的前提下返回结果。
// 调度并发必须有上限。TaskRun 可以比创建它的 HTTP 请求活得更久，但不能比应用进程活得更久。
//
// 学习顺序：先实现 fake Runner 并写调度测试；明确谁关闭 channel、调用方如何等待每个
// goroutine，再接入真实子进程。
package runner

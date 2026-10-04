// Command workbench 是 Product Agent Workbench 本地控制面的唯一进程入口。
//
// 学习任务：
//  1. 加载进程级配置，产品输入不能成为 Shell 命令。
//  2. 按依赖顺序显式装配 Store、Runner、应用模块和 HTTP 模块。
//  3. 启动 HTTP 服务，并在收到 SIGINT/SIGTERM 时优雅退出。
//  4. 保持入口足够薄，不在 main 中编写领域规则。
//
// 当前入口故意为空。先完成 internal/domain 的状态机练习，再回来装配应用。
package main

func main() {}

// Package codex 把原生 Codex app-server JSON-RPC 协议转换成 Runner 事件和结果。
//
// Go 不负责运行 Codex 模型。本 Adapter 只解析并启动原生 codex 可执行文件，完成协议初始化、
// 创建或恢复 Thread、启动或中断 Turn、归一化通知，并等待进程退出。stdout 是协议通道，
// 诊断信息只能进入 stderr 或结构化日志；持久化和日志内容必须先脱敏。
//
// 学习顺序：先录制 fake app-server 协议样本，再写解析器。测试必须覆盖半行输入、非法消息、
// request id 对应、取消、超时、意外 EOF 和优雅退出。
package codex

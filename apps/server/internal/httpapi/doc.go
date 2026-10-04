// Package httpapi 向浏览器提供 Snapshot、动作、健康检查、环境预检和 SSE 接口。
//
// HTTP handler 只负责把传输层输入翻译为用例调用，不能包含 Requirement 状态规则、SQL、
// Git 命令或 Codex 协议处理。写请求必须携带幂等键和调用方看到的 Snapshot revision。
//
// 学习顺序：先用 httptest 和 fake 应用接口完成普通 JSON endpoint；只有 Snapshot 与动作
// endpoint 已经确定后，才添加 SSE。
package httpapi

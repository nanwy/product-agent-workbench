package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	// ErrJSONRPCNotStarted 表示调用方在启动协议读写循环之前发送了消息。
	ErrJSONRPCNotStarted = errors.New("JSON-RPC 连接尚未启动")
	// ErrJSONRPCAlreadyStarted 表示同一连接被重复启动。
	ErrJSONRPCAlreadyStarted = errors.New("JSON-RPC 连接已经启动")
	// ErrJSONRPCClosed 表示连接已经关闭，不能继续收发消息。
	ErrJSONRPCClosed = errors.New("JSON-RPC 连接已经关闭")
	// ErrInvalidJSONRPC 表示对端发来的消息不符合 app-server 的 JSON-RPC 风格结构。
	ErrInvalidJSONRPC = errors.New("JSON-RPC 消息无效")
	// ErrInvalidJSONRPCDependencies 表示连接缺少必要的输入或输出管道。
	ErrInvalidJSONRPCDependencies = errors.New("JSON-RPC 连接依赖无效")
)

// RPCError 是 JSON-RPC 响应里的结构化错误。
//
// Code 和 Message 来自协议；Data 保留服务端附加的机器可读信息，当前阶段不解释其内容。
type RPCError struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error 让 RPCError 可以作为普通 Go error 向上传播。
func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return fmt.Sprintf("JSON-RPC 请求失败（code=%d）", e.Code)
	}
	return fmt.Sprintf("JSON-RPC 请求失败（code=%d）：%s", e.Code, e.Message)
}

// IncomingRequest 是 app-server 主动发给客户端的反向请求，例如后续的审批请求。
type IncomingRequest struct {
	ID     RequestID
	Method string
	Params json.RawMessage
}

// RequestID 保存 app-server 允许使用的整数或字符串请求 ID。
//
// 工作台主动发出的请求统一使用递增整数；反向请求则必须原样保留服务端给出的类型和值，
// 否则响应无法和原请求对应。
type RequestID struct {
	integer *int64
	text    *string
}

// Integer 返回整数 ID；字符串 ID 返回 false。
func (id RequestID) Integer() (int64, bool) {
	if id.integer == nil {
		return 0, false
	}
	return *id.integer, true
}

// Text 返回字符串 ID；整数 ID 返回 false。
func (id RequestID) Text() (string, bool) {
	if id.text == nil {
		return "", false
	}
	return *id.text, true
}

// MarshalJSON 在响应反向请求时原样保留 ID 的 JSON 类型。
func (id RequestID) MarshalJSON() ([]byte, error) {
	if id.integer != nil {
		return json.Marshal(*id.integer)
	}
	if id.text != nil {
		return json.Marshal(*id.text)
	}
	return nil, fmt.Errorf("请求 ID 为空")
}

// RequestResponse 是客户端处理反向请求后返回给 app-server 的结果。
// Error 非 nil 时发送 error；否则发送 Result，即使 Result 本身是 nil。
type RequestResponse struct {
	Result any
	Error  *RPCError
}

// RequestHandler 处理 app-server 主动发来的反向请求。
//
// 处理函数运行在唯一读循环中，必须尽快返回，不能在这里等待另一个 JSON-RPC 响应；
// 后续审批适配器只需根据本地策略构造一个立即响应。
type RequestHandler func(ctx context.Context, request IncomingRequest) RequestResponse

// jsonRPCMessage 是只在传输层内部使用的线上消息模型。
//
// RawMessage 用来区分字段“没有出现”和“出现但值为 null”。JSON-RPC 响应允许 result=null，
// 因此不能直接用 any 的零值判断响应里是否存在 result。
type jsonRPCMessage struct {
	// app-server 当前不会发送 jsonrpc 字段；这里仍接收它，以兼容标准 JSON-RPC 客户端。
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type pendingResponse struct {
	result json.RawMessage
	err    error
}

type pendingRequest struct {
	response chan pendingResponse
}

type outgoingMessage struct {
	body []byte
	ack  chan error
}

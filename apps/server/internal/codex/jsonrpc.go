package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
)

const defaultMaxJSONRPCMessageBytes = 8 << 20

// JSONRPCDependencies 是协议连接需要的字节流和消息处理器。
//
// Input 对应 app-server stdout，Output 对应 app-server stdin。要求它们可关闭，是为了在
// context 取消或任一循环失败时解除另一个循环可能发生的阻塞，避免 goroutine 泄漏。
type JSONRPCDependencies struct {
	Input            io.ReadCloser
	Output           io.WriteCloser
	NotificationSink NotificationSink
	RequestHandler   RequestHandler
	MaxMessageBytes  int
}

// JSONRPCConnection 管理一条换行分隔的 JSON-RPC 风格双向连接。
// app-server 省略标准协议里的 jsonrpc 字段，本实现发送时省略、接收时兼容该字段存在。
//
// 它只负责协议收发，不负责启动或回收 Codex 子进程。整个连接始终只有一个 reader loop
// 和一个 writer loop：reader 保证入站消息顺序，writer 防止多个调用方并发写坏 stdout 协议帧。
type JSONRPCConnection struct {
	input            io.ReadCloser
	output           io.WriteCloser
	notificationSink NotificationSink
	requestHandler   RequestHandler
	maxMessageBytes  int

	stateMu sync.Mutex
	started bool
	ctx     context.Context
	cancel  context.CancelFunc

	nextID atomic.Int64
	writes chan outgoingMessage

	pendingMu sync.Mutex
	pending   map[int64]pendingRequest

	errMu       sync.Mutex
	terminalErr error

	closeStreamsOnce sync.Once
	waitGroup        sync.WaitGroup
	loopsDone        atomic.Int32
	done             chan struct{}
}

// NewJSONRPCConnection 创建尚未启动的协议连接，不创建 goroutine，也不读取任何字节。
func NewJSONRPCConnection(dependencies JSONRPCDependencies) (*JSONRPCConnection, error) {
	if dependencies.Input == nil || dependencies.Output == nil {
		return nil, fmt.Errorf("创建 JSON-RPC 连接: %w", ErrInvalidJSONRPCDependencies)
	}
	maxMessageBytes := dependencies.MaxMessageBytes
	if maxMessageBytes == 0 {
		maxMessageBytes = defaultMaxJSONRPCMessageBytes
	}
	if maxMessageBytes < 1 {
		return nil, fmt.Errorf("创建 JSON-RPC 连接: MaxMessageBytes 必须大于 0")
	}

	connection := &JSONRPCConnection{
		input:            dependencies.Input,
		output:           dependencies.Output,
		notificationSink: dependencies.NotificationSink,
		requestHandler:   dependencies.RequestHandler,
		maxMessageBytes:  maxMessageBytes,
		writes:           make(chan outgoingMessage),
		pending:          make(map[int64]pendingRequest),
		done:             make(chan struct{}),
	}
	// JSON-RPC 请求 ID 从 1 开始，0 留作“尚未分配”的调试信号。
	connection.nextID.Store(0)
	return connection, nil
}

// Start 启动唯一的读循环和写循环。同一个 Connection 只能启动一次。
func (c *JSONRPCConnection) Start(parent context.Context) error {
	if parent == nil {
		return fmt.Errorf("启动 JSON-RPC 连接: context 不能为空")
	}
	if err := parent.Err(); err != nil {
		return err
	}

	c.stateMu.Lock()
	if c.started {
		c.stateMu.Unlock()
		return ErrJSONRPCAlreadyStarted
	}
	c.started = true
	c.ctx, c.cancel = context.WithCancel(parent)
	c.stateMu.Unlock()

	c.waitGroup.Add(2)
	go c.readLoop()
	go c.writeLoop()
	return nil
}

// Request 发送一个请求，并按数字 ID 等待属于该请求的响应。
//
// result 必须是 json.Unmarshal 可接受的指针；传 nil 表示调用方只关心请求是否成功。
// 超时由调用方传入的 ctx 控制，传输层不偷偷创建另一套计时器。
func (c *JSONRPCConnection) Request(
	ctx context.Context,
	method string,
	params any,
	result any,
) error {
	if ctx == nil {
		return fmt.Errorf("发送 JSON-RPC 请求: context 不能为空")
	}
	if method == "" {
		return fmt.Errorf("发送 JSON-RPC 请求: method 不能为空")
	}

	connectionContext, err := c.startedContext()
	if err != nil {
		return err
	}
	id := c.nextID.Add(1)
	body, err := marshalRequest(id, method, params)
	if err != nil {
		return fmt.Errorf("编码 JSON-RPC 请求 %q: %w", method, err)
	}

	waiter := pendingRequest{response: make(chan pendingResponse, 1)}
	c.pendingMu.Lock()
	c.pending[id] = waiter
	c.pendingMu.Unlock()

	if err := c.send(ctx, connectionContext, body); err != nil {
		c.removePending(id)
		return fmt.Errorf("发送 JSON-RPC 请求 %q: %w", method, err)
	}

	select {
	case response := <-waiter.response:
		if response.err != nil {
			return response.err
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(response.result, result); err != nil {
			return fmt.Errorf("解析 JSON-RPC 请求 %q 的结果: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case <-connectionContext.Done():
		c.removePending(id)
		return c.connectionError()
	}
}

// Notify 发送不需要响应的 JSON-RPC 通知。
func (c *JSONRPCConnection) Notify(ctx context.Context, method string, params any) error {
	if ctx == nil {
		return fmt.Errorf("发送 JSON-RPC 通知: context 不能为空")
	}
	if method == "" {
		return fmt.Errorf("发送 JSON-RPC 通知: method 不能为空")
	}
	connectionContext, err := c.startedContext()
	if err != nil {
		return err
	}
	body, err := marshalNotification(method, params)
	if err != nil {
		return fmt.Errorf("编码 JSON-RPC 通知 %q: %w", method, err)
	}
	if err := c.send(ctx, connectionContext, body); err != nil {
		return fmt.Errorf("发送 JSON-RPC 通知 %q: %w", method, err)
	}
	return nil
}

// Close 取消协议连接、关闭底层管道，并等待两个循环退出。
func (c *JSONRPCConnection) Close() error {
	c.stateMu.Lock()
	if !c.started {
		c.stateMu.Unlock()
		return nil
	}
	c.cancel()
	c.stateMu.Unlock()

	c.closeStreams()
	c.failPending(ErrJSONRPCClosed)
	c.waitGroup.Wait()
	return c.terminalError()
}

// Wait 等待读写循环退出。子进程适配器可用它观察意外 EOF 或协议错误。
func (c *JSONRPCConnection) Wait() error {
	c.stateMu.Lock()
	started := c.started
	c.stateMu.Unlock()
	if !started {
		return ErrJSONRPCNotStarted
	}
	c.waitGroup.Wait()
	return c.terminalError()
}

// Done 在 reader loop 和 writer loop 都退出后关闭，用于上层同时等待 Turn 与协议故障。
func (c *JSONRPCConnection) Done() <-chan struct{} {
	if c == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return c.done
}

func (c *JSONRPCConnection) readLoop() {
	defer c.loopFinished()
	defer c.closeStreams()

	scanner := bufio.NewScanner(c.input)
	scanner.Buffer(make([]byte, 64<<10), c.maxMessageBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		// Scanner 的缓冲区会在下一次 Scan 时复用；复制后才能安全交给解析与回调。
		if err := c.handleIncoming(bytes.Clone(line)); err != nil {
			c.stop(fmt.Errorf("读取 JSON-RPC 消息: %w", err))
			return
		}
	}
	if err := scanner.Err(); err != nil {
		c.stop(fmt.Errorf("读取 JSON-RPC 流: %w", err))
		return
	}
	if c.contextDone() {
		return
	}
	c.stop(fmt.Errorf("读取 JSON-RPC 流: %w", io.ErrUnexpectedEOF))
}

func (c *JSONRPCConnection) writeLoop() {
	defer c.loopFinished()
	defer c.closeStreams()

	for {
		select {
		case <-c.ctx.Done():
			return
		case message := <-c.writes:
			// app-server 的 stdio 协议以换行分帧。只有本循环可以写 Output，因此并发请求
			// 不会把两段 JSON 交错成一条非法消息。
			payload := append(message.body, '\n')
			err := writeAll(c.output, payload)
			message.ack <- err
			if err != nil {
				c.stop(fmt.Errorf("写入 JSON-RPC 流: %w", err))
				return
			}
		}
	}
}

func (c *JSONRPCConnection) loopFinished() {
	c.waitGroup.Done()
	if c.loopsDone.Add(1) == 2 {
		close(c.done)
	}
}

func (c *JSONRPCConnection) handleIncoming(line []byte) error {
	var message jsonRPCMessage
	if err := json.Unmarshal(line, &message); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidJSONRPC, err)
	}
	if message.JSONRPC != "" && message.JSONRPC != "2.0" {
		return fmt.Errorf("%w: jsonrpc 如果存在则必须是 2.0", ErrInvalidJSONRPC)
	}

	hasID := len(message.ID) > 0 && !bytes.Equal(message.ID, []byte("null"))
	hasResult := len(message.Result) > 0
	hasError := len(message.Error) > 0 && !bytes.Equal(message.Error, []byte("null"))

	if message.Method != "" {
		if hasResult || hasError {
			return fmt.Errorf("%w: request/notification 不能包含 result 或 error", ErrInvalidJSONRPC)
		}
		if hasID {
			id, err := parseRequestID(message.ID)
			if err != nil {
				return err
			}
			return c.handleServerRequest(id, message.Method, message.Params)
		}
		return c.handleNotification(message.Method, message.Params)
	}

	if !hasID || hasResult == hasError {
		return fmt.Errorf("%w: response 必须包含 id，并且只能包含 result 或 error 之一", ErrInvalidJSONRPC)
	}
	id, err := parsePendingRequestID(message.ID)
	if err != nil {
		return err
	}
	return c.handleResponse(id, message.Result, message.Error)
}

func (c *JSONRPCConnection) handleResponse(id int64, result, rawError json.RawMessage) error {
	var responseError error
	if len(rawError) > 0 {
		var rpcError RPCError
		if err := json.Unmarshal(rawError, &rpcError); err != nil {
			return fmt.Errorf("%w: error 字段无效: %v", ErrInvalidJSONRPC, err)
		}
		responseError = &rpcError
	}

	pending, ok := c.takePending(id)
	if !ok {
		// 请求可能已因调用方超时而从 pending map 移除；迟到响应不应杀死整条连接。
		return nil
	}
	pending.response <- pendingResponse{result: bytes.Clone(result), err: responseError}
	return nil
}

func (c *JSONRPCConnection) handleNotification(method string, params json.RawMessage) error {
	if c.notificationSink == nil {
		return nil
	}
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	return c.notificationSink(c.ctx, Notification{
		Method: method,
		Body:   bytes.Clone(params),
	})
}

func (c *JSONRPCConnection) handleServerRequest(
	id RequestID,
	method string,
	params json.RawMessage,
) error {
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	response := RequestResponse{
		Error: &RPCError{Code: -32601, Message: "method not found"},
	}
	if c.requestHandler != nil {
		response = c.requestHandler(c.ctx, IncomingRequest{
			ID:     id,
			Method: method,
			Params: bytes.Clone(params),
		})
	}

	body, err := marshalResponse(id, response)
	if err != nil {
		return fmt.Errorf("编码 JSON-RPC 反向请求 %q 的响应: %w", method, err)
	}
	return c.send(c.ctx, c.ctx, body)
}

func (c *JSONRPCConnection) send(
	ctx context.Context,
	connectionContext context.Context,
	body []byte,
) error {
	message := outgoingMessage{body: body, ack: make(chan error, 1)}
	select {
	case c.writes <- message:
	case <-ctx.Done():
		return ctx.Err()
	case <-connectionContext.Done():
		return c.connectionError()
	}

	select {
	case err := <-message.ack:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-connectionContext.Done():
		return c.connectionError()
	}
}

func (c *JSONRPCConnection) startedContext() (context.Context, error) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if !c.started {
		return nil, ErrJSONRPCNotStarted
	}
	if err := c.ctx.Err(); err != nil {
		return nil, c.connectionError()
	}
	return c.ctx, nil
}

func (c *JSONRPCConnection) stop(err error) {
	if err != nil && !c.contextDone() {
		c.errMu.Lock()
		if c.terminalErr == nil {
			c.terminalErr = err
		}
		c.errMu.Unlock()
	}
	c.stateMu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	c.stateMu.Unlock()
	c.failPending(c.connectionError())
}

func (c *JSONRPCConnection) connectionError() error {
	if err := c.terminalError(); err != nil {
		return err
	}
	return ErrJSONRPCClosed
}

func (c *JSONRPCConnection) terminalError() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	return c.terminalErr
}

func (c *JSONRPCConnection) contextDone() bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.ctx != nil && c.ctx.Err() != nil
}

func (c *JSONRPCConnection) closeStreams() {
	c.closeStreamsOnce.Do(func() {
		_ = c.output.Close()
		_ = c.input.Close()
	})
}

func (c *JSONRPCConnection) removePending(id int64) {
	c.pendingMu.Lock()
	delete(c.pending, id)
	c.pendingMu.Unlock()
}

func (c *JSONRPCConnection) takePending(id int64) (pendingRequest, bool) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	pending, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	return pending, ok
}

func (c *JSONRPCConnection) failPending(err error) {
	c.pendingMu.Lock()
	pending := c.pending
	c.pending = make(map[int64]pendingRequest)
	c.pendingMu.Unlock()
	for _, request := range pending {
		request.response <- pendingResponse{err: err}
	}
}

func marshalRequest(id int64, method string, params any) ([]byte, error) {
	return json.Marshal(struct {
		ID     int64  `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{ID: id, Method: method, Params: paramsOrEmptyObject(params)})
}

func marshalNotification(method string, params any) ([]byte, error) {
	return json.Marshal(struct {
		Method string `json:"method"`
		Params any    `json:"params"`
	}{Method: method, Params: paramsOrEmptyObject(params)})
}

func marshalResponse(id RequestID, response RequestResponse) ([]byte, error) {
	if response.Error != nil {
		return json.Marshal(struct {
			ID    RequestID `json:"id"`
			Error *RPCError `json:"error"`
		}{ID: id, Error: response.Error})
	}
	return json.Marshal(struct {
		ID     RequestID `json:"id"`
		Result any       `json:"result"`
	}{ID: id, Result: response.Result})
}

func paramsOrEmptyObject(params any) any {
	if params == nil {
		return struct{}{}
	}
	return params
}

func parsePendingRequestID(raw json.RawMessage) (int64, error) {
	// 工作台发出的请求使用正整数 ID，因此响应也必须回显同一个整数类型。
	id, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("%w: id 必须是正整数", ErrInvalidJSONRPC)
	}
	return id, nil
}

func parseRequestID(raw json.RawMessage) (RequestID, error) {
	if id, err := strconv.ParseInt(string(raw), 10, 64); err == nil {
		return RequestID{integer: &id}, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil && text != "" {
		return RequestID{text: &text}, nil
	}
	return RequestID{}, fmt.Errorf("%w: id 必须是整数或非空字符串", ErrInvalidJSONRPC)
}

func writeAll(writer io.Writer, body []byte) error {
	for len(body) > 0 {
		written, err := writer.Write(body)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		body = body[written:]
	}
	return nil
}

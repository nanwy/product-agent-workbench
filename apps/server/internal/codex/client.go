package codex

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

var (
	// ErrInvalidClientConfig 表示 Connection 或宿主标识不完整。
	ErrInvalidClientConfig = errors.New("Codex app-server Client 配置无效")
	// ErrClientNotInitialized 表示调用方跳过了 initialize/initialized 握手。
	ErrClientNotInitialized = errors.New("Codex app-server Client 尚未初始化")
	// ErrClientAlreadyInitialized 表示同一个 Client 被重复握手。
	ErrClientAlreadyInitialized = errors.New("Codex app-server Client 已经初始化")
	// ErrInvalidTurnRequest 表示工作目录、Prompt、Thread 或 Sandbox 输入无效。
	ErrInvalidTurnRequest = errors.New("Codex Turn 请求无效")
)

const (
	// SandboxReadOnly 禁止 Codex 修改工作目录。
	SandboxReadOnly = "read-only"
	// SandboxWorkspaceWrite 只允许 Codex 修改受管 Worktree。
	SandboxWorkspaceWrite = "workspace-write"
)

// ClientInfo 是 initialize 请求中标识宿主应用的稳定信息。
type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

// AppServerClient 在 16A 的传输层之上实现 app-server 会话协议。
// 它不启动进程，也不解释业务事件；调用方必须严格按初始化、Thread、Turn 的顺序使用。
type AppServerClient struct {
	connection *JSONRPCConnection
	clientInfo ClientInfo

	stateMu     sync.Mutex
	initialized bool
}

// NewAppServerClient 创建协议客户端，不发送任何请求。
func NewAppServerClient(connection *JSONRPCConnection, clientInfo ClientInfo) (*AppServerClient, error) {
	clientInfo.Name = strings.TrimSpace(clientInfo.Name)
	clientInfo.Title = strings.TrimSpace(clientInfo.Title)
	clientInfo.Version = strings.TrimSpace(clientInfo.Version)
	if connection == nil || clientInfo.Name == "" || clientInfo.Title == "" || clientInfo.Version == "" {
		return nil, ErrInvalidClientConfig
	}
	return &AppServerClient{connection: connection, clientInfo: clientInfo}, nil
}

// Initialize 完成 request/response 握手，再发送 initialized 通知。
// 必须等待 initialize 成功后才能通知服务端初始化完成。
func (c *AppServerClient) Initialize(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("初始化 Codex app-server: context 不能为空")
	}
	c.stateMu.Lock()
	if c.initialized {
		c.stateMu.Unlock()
		return ErrClientAlreadyInitialized
	}
	c.stateMu.Unlock()

	params := initializeParams{
		ClientInfo: c.clientInfo,
		Capabilities: initializeCapabilities{
			ExperimentalAPI: true,
		},
	}
	if err := c.connection.Request(ctx, "initialize", params, nil); err != nil {
		return fmt.Errorf("Codex app-server initialize: %w", err)
	}
	if err := c.connection.Notify(ctx, "initialized", struct{}{}); err != nil {
		return fmt.Errorf("Codex app-server initialized: %w", err)
	}

	c.stateMu.Lock()
	c.initialized = true
	c.stateMu.Unlock()
	return nil
}

// OpenThread 创建新 Thread，或者恢复 request.ThreadID 指定的已有 Thread。
func (c *AppServerClient) OpenThread(ctx context.Context, request TurnRequest) (string, error) {
	if err := c.requireInitialized(); err != nil {
		return "", err
	}
	if err := validateThreadRequest(request); err != nil {
		return "", err
	}

	var result threadResult
	if strings.TrimSpace(request.ThreadID) == "" {
		params := threadStartParams{
			CWD:                   request.WorkingDirectory,
			RuntimeWorkspaceRoots: []string{request.WorkingDirectory},
			ApprovalPolicy:        "never",
			Sandbox:               request.SandboxMode,
			DeveloperInstructions: nil,
			Ephemeral:             false,
			ThreadSource:          "product-agent-workbench",
			ExperimentalRawEvents: false,
		}
		if err := c.connection.Request(ctx, "thread/start", params, &result); err != nil {
			return "", fmt.Errorf("Codex thread/start: %w", err)
		}
	} else {
		params := threadResumeParams{
			ThreadID:              strings.TrimSpace(request.ThreadID),
			CWD:                   request.WorkingDirectory,
			RuntimeWorkspaceRoots: []string{request.WorkingDirectory},
			ApprovalPolicy:        "never",
			Sandbox:               request.SandboxMode,
			DeveloperInstructions: nil,
		}
		if err := c.connection.Request(ctx, "thread/resume", params, &result); err != nil {
			return "", fmt.Errorf("Codex thread/resume: %w", err)
		}
	}

	threadID := firstNonBlank(result.Thread.ID, result.ThreadID)
	if threadID == "" {
		return "", fmt.Errorf("Codex Thread 响应缺少 thread id")
	}
	return threadID, nil
}

// StartTurn 向已经打开的 Thread 提交一段文本输入，并返回本次 Turn ID。
func (c *AppServerClient) StartTurn(
	ctx context.Context,
	threadID string,
	request TurnRequest,
) (string, error) {
	if err := c.requireInitialized(); err != nil {
		return "", err
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" || strings.TrimSpace(request.Prompt) == "" {
		return "", ErrInvalidTurnRequest
	}
	if err := validateThreadRequest(request); err != nil {
		return "", err
	}

	params := turnStartParams{
		ThreadID: threadID,
		Input: []turnInput{{
			Type: "text",
			Text: request.Prompt,
		}},
		CWD:            request.WorkingDirectory,
		ApprovalPolicy: "never",
		SandboxPolicy:  sandboxPolicy(request.SandboxMode, request.WorkingDirectory),
	}
	var result turnStartResult
	if err := c.connection.Request(ctx, "turn/start", params, &result); err != nil {
		return "", fmt.Errorf("Codex turn/start: %w", err)
	}
	turnID := firstNonBlank(result.Turn.ID, result.TurnID)
	if turnID == "" {
		return "", fmt.Errorf("Codex turn/start 响应缺少 turn id")
	}
	return turnID, nil
}

// InterruptTurn 请求 app-server 中断当前 Turn。真正的进程回收仍由 AppServerProcess 负责。
func (c *AppServerClient) InterruptTurn(ctx context.Context, threadID, turnID string) error {
	if err := c.requireInitialized(); err != nil {
		return err
	}
	threadID = strings.TrimSpace(threadID)
	turnID = strings.TrimSpace(turnID)
	if threadID == "" || turnID == "" {
		return ErrInvalidTurnRequest
	}
	if err := c.connection.Request(ctx, "turn/interrupt", turnInterruptParams{
		ThreadID: threadID,
		TurnID:   turnID,
	}, nil); err != nil {
		return fmt.Errorf("Codex turn/interrupt: %w", err)
	}
	return nil
}

func (c *AppServerClient) requireInitialized() error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if !c.initialized {
		return ErrClientNotInitialized
	}
	return nil
}

func validateThreadRequest(request TurnRequest) error {
	if strings.TrimSpace(request.WorkingDirectory) == "" ||
		!filepath.IsAbs(request.WorkingDirectory) ||
		(request.SandboxMode != SandboxReadOnly && request.SandboxMode != SandboxWorkspaceWrite) {
		return ErrInvalidTurnRequest
	}
	return nil
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func sandboxPolicy(mode, workspaceRoot string) any {
	if mode == SandboxReadOnly {
		return readOnlySandboxPolicy{Type: "readOnly", NetworkAccess: false}
	}
	return workspaceWriteSandboxPolicy{
		Type:                "workspaceWrite",
		WritableRoots:       []string{workspaceRoot},
		NetworkAccess:       false,
		ExcludeTmpdirEnvVar: false,
		ExcludeSlashTmp:     false,
	}
}

type initializeParams struct {
	ClientInfo   ClientInfo             `json:"clientInfo"`
	Capabilities initializeCapabilities `json:"capabilities"`
}

type initializeCapabilities struct {
	ExperimentalAPI bool `json:"experimentalApi"`
}

type threadStartParams struct {
	CWD                   string   `json:"cwd"`
	RuntimeWorkspaceRoots []string `json:"runtimeWorkspaceRoots"`
	ApprovalPolicy        string   `json:"approvalPolicy"`
	Sandbox               string   `json:"sandbox"`
	DeveloperInstructions *string  `json:"developerInstructions"`
	Ephemeral             bool     `json:"ephemeral"`
	ThreadSource          string   `json:"threadSource"`
	ExperimentalRawEvents bool     `json:"experimentalRawEvents"`
}

type threadResumeParams struct {
	ThreadID              string   `json:"threadId"`
	CWD                   string   `json:"cwd"`
	RuntimeWorkspaceRoots []string `json:"runtimeWorkspaceRoots"`
	ApprovalPolicy        string   `json:"approvalPolicy"`
	Sandbox               string   `json:"sandbox"`
	DeveloperInstructions *string  `json:"developerInstructions"`
}

type threadResult struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
	ThreadID string `json:"threadId"`
}

type turnInput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type turnStartParams struct {
	ThreadID       string      `json:"threadId"`
	Input          []turnInput `json:"input"`
	CWD            string      `json:"cwd"`
	ApprovalPolicy string      `json:"approvalPolicy"`
	SandboxPolicy  any         `json:"sandboxPolicy"`
}

type turnStartResult struct {
	Turn struct {
		ID string `json:"id"`
	} `json:"turn"`
	TurnID string `json:"turnId"`
}

type turnInterruptParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type readOnlySandboxPolicy struct {
	Type          string `json:"type"`
	NetworkAccess bool   `json:"networkAccess"`
}

type workspaceWriteSandboxPolicy struct {
	Type                string   `json:"type"`
	WritableRoots       []string `json:"writableRoots"`
	NetworkAccess       bool     `json:"networkAccess"`
	ExcludeTmpdirEnvVar bool     `json:"excludeTmpdirEnvVar"`
	ExcludeSlashTmp     bool     `json:"excludeSlashTmp"`
}

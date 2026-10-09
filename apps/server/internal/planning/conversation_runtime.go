package planning

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/codex"
)

// ErrInvalidConversationRuntime 表示只读对话运行时缺少 Planner 或可信仓库根目录。
var ErrInvalidConversationRuntime = errors.New("Conversation Runtime 配置无效")

// ConversationRuntime 把具体 Codex Planner 适配为应用层的 ConversationRuntime port。
//
// RepositoryRoot 在进程启动时从目标配置读取并固定。Application 和浏览器只能提供 Prompt 与
// Thread ID，不能借对话接口让 Codex 读取另一个目录。
type ConversationRuntime struct {
	planner        *codex.ReadOnlyPlanner
	repositoryRoot string
}

// NewConversationRuntime 创建只读运行时 Adapter，不启动进程。
func NewConversationRuntime(
	planner *codex.ReadOnlyPlanner,
	repositoryRoot string,
) (*ConversationRuntime, error) {
	// 第 1 步：清理并验证启动期配置。相对路径会受当前工作目录影响，因此直接拒绝。
	repositoryRoot = filepath.Clean(strings.TrimSpace(repositoryRoot))
	if planner == nil || repositoryRoot == "." || !filepath.IsAbs(repositoryRoot) {
		return nil, ErrInvalidConversationRuntime
	}

	// 第 2 步：只保存依赖；真正的 app-server 在每次 Run 时启动并由该次调用负责关闭。
	return &ConversationRuntime{planner: planner, repositoryRoot: repositoryRoot}, nil
}

// Run 把应用层请求和 callback 翻译为 Codex 只读对话协议。
func (runtime *ConversationRuntime) Run(
	ctx context.Context,
	request app.ConversationRuntimeRequest,
	events app.ConversationEventSink,
	sessions app.ConversationSessionSink,
) (app.ConversationRuntimeResult, error) {
	// 第 1 步：Application 不传执行目录；这里强制补入构造时固定的可信 RepositoryRoot。
	codexRequest := codex.ReadOnlyConversationRequest{
		WorkingDirectory: runtime.repositoryRoot,
		Prompt:           request.Prompt,
		ThreadID:         request.ThreadID,
	}

	// 第 2 步：桥接事件和 Session callback。
	// Codex readLoop 会串行调用 Event；Session 会在 turn/start 之前调用。
	callbacks := codex.ReadOnlyConversationCallbacks{
		Event: func(eventCtx context.Context, eventType string, payload []byte) error {
			if events == nil {
				return nil
			}
			return events(eventCtx, eventType, payload)
		},
		Session: func(sessionCtx context.Context, threadID string) error {
			if sessions == nil {
				return nil
			}
			return sessions(sessionCtx, threadID)
		},
	}

	// 第 3 步：执行一次完整 Turn。即使失败也保留 result 中已经得到的 Thread/Turn ID。
	result, err := runtime.planner.RunConversationTurn(ctx, codexRequest, callbacks)
	return app.ConversationRuntimeResult{
		ThreadID: result.ThreadID,
		TurnID:   result.TurnID,
		Status:   result.Status,
		Message:  result.Message,
		Summary:  result.Summary,
	}, err
}

var _ app.ConversationRuntime = (*ConversationRuntime)(nil)

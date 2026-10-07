package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"product-agent-workbench/internal/domain"
)

// ErrInvalidExecutionFacts 表示 TaskRun 与服务端读取出的执行事实缺失或身份不一致。
// 这属于可信数据边界错误，不能继续启动 Codex 或创建 Worktree。
var ErrInvalidExecutionFacts = errors.New("执行事实无效")

// ExecutionFacts 是从服务端可信配置和已确认 Ticket artifact 读取出的执行事实。
//
// 它不是 HTTP DTO：浏览器不能直接提交这些字段。前端类比是“服务端根据 session 和数据库
// 查出的内部配置”，而不是表单 body。Builder 只把这些事实校验并映射成 RunRequest。
type ExecutionFacts struct {
	RequirementID   string
	TicketVersionID string
	TicketID        string
	RepositoryRoot  string
	Prompt          string
	CodexThreadID   string
	Workspace       WorkspacePlan
}

// ExecutionFactsReader 隔离“事实从哪里读取”。
//
// 17B 先定义消费方需要的最小接口；后续文件 Adapter 再组合项目配置、Ticket artifact 和前次
// TaskRun 结果。RequestBuilder 不应该自己解析 Markdown，也不能相信浏览器提供仓库路径。
type ExecutionFactsReader interface {
	ReadExecutionFacts(ctx context.Context, run domain.TaskRun) (ExecutionFacts, error)
}

// NewTrustedRequestBuilder 创建 Worker 使用的 RequestBuilder。
//
// 前端可以把它理解成一个只读取服务端状态的 queryFn：
//
//	const buildRequest = async (runningTask) => {
//	  const facts = await trustedFacts.read(runningTask)
//	  assertSameIdentity(facts, runningTask)
//	  return { taskRun: runningTask, ...facts }
//	}
//
// Go 实现分成“构造时”和“每次调用时”两层：
//
//  1. 构造时检查 reader 不是 nil，然后返回闭包；构造函数不能提前读取 artifact；
//  2. 闭包先检查 ctx.Err()，再确认 run 是 running，且 TaskRun ID 和三个归属字段不为空；
//  3. 调用 reader.ReadExecutionFacts(ctx, run)；
//  4. facts 的 RequirementID、TicketVersionID、TicketID 必须和 run 完全一致；
//  5. RepositoryRoot、Prompt 和 Workspace 必填项交给现有 validateWorkspaceRequest 统一校验，
//     不要复制第二套路径/分支规则；
//  6. 构造 RunRequest，并用 append([]string(nil), scopes...) 复制 ModificationScopes；
//  7. 调用 validateWorkspaceRequest，失败时包装上下文后返回；成功返回 request。
//
// 这里校验身份的原因和前端缓存 key 相似：请求 `ticket-A` 时绝不能把 `ticket-B` 的数据
// 填进结果；区别是后端错配会让 Codex 修改错误分支，因此必须作为安全边界拒绝。
func NewTrustedRequestBuilder(reader ExecutionFactsReader) (RequestBuilder, error) {
	if reader == nil {
		return nil, ErrInvalidDependencies
	}
	return func(ctx context.Context, run domain.TaskRun) (RunRequest, error) {
		if err := ctx.Err(); err != nil {
			return RunRequest{}, err
		}
		if err := validateBuilderTaskRun(run); err != nil {
			return RunRequest{}, err
		}

		facts, err := reader.ReadExecutionFacts(ctx, run)
		if err != nil {
			return RunRequest{}, fmt.Errorf("读取 TaskRun %q 的执行事实: %w", run.ID, err)
		}
		if !sameExecutionIdentity(run, facts) {
			return RunRequest{}, fmt.Errorf(
				"%w: TaskRun %q 与读取结果的 Requirement/Ticket 身份不一致",
				ErrInvalidExecutionFacts,
				run.ID,
			)
		}

		request := RunRequest{
			TaskRun:        run,
			RepositoryRoot: facts.RepositoryRoot,
			Prompt:         facts.Prompt,
			CodexThreadID:  facts.CodexThreadID,
			Workspace:      facts.Workspace,
		}
		request.Workspace.ModificationScopes = append(
			[]string(nil),
			facts.Workspace.ModificationScopes...,
		)

		if err := validateWorkspaceRequest(request); err != nil {
			return RunRequest{}, fmt.Errorf("构造 TaskRun %q 的执行请求: %w", run.ID, err)
		}
		return request, nil
	}, nil
}

func validateBuilderTaskRun(run domain.TaskRun) error {
	if run.Status != domain.TaskStatusRunning {
		return fmt.Errorf("%w: TaskRun %q 必须处于 running", ErrInvalidExecutionFacts, run.ID)
	}
	identityFields := []string{run.ID, run.RequirementID, run.TicketVersionID, run.TicketID}
	for _, value := range identityFields {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: running TaskRun 身份字段不能为空", ErrInvalidExecutionFacts)
		}
	}
	return nil
}

func sameExecutionIdentity(run domain.TaskRun, facts ExecutionFacts) bool {
	sameRequirement := facts.RequirementID == run.RequirementID
	sameVersion := facts.TicketVersionID == run.TicketVersionID
	sameTicket := facts.TicketID == run.TicketID
	return sameRequirement && sameVersion && sameTicket
}

import { memo, useEffect, useRef, useState, type FormEvent } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import {
  requirementStages,
  type Requirement,
  type RequirementStage,
  type ConversationMessage,
  type WorkbenchSnapshot,
} from "../domain/workbench";
import {
  projectConversation,
  workbenchQueryKeys,
  type ConversationProjection,
} from "../store/workbench-query";
import { partitionStreamingMarkdown } from "../store/streaming-markdown";

// 本文件只负责展示和收集输入；状态迁移与副作用统一留在服务端用例层。
export const stageLabels: Record<RequirementStage, string> = {
  "project-adaptation": "项目适配",
  "intent-drafting": "需求澄清",
  "spec-drafting": "整理规格",
  "awaiting-spec-confirmation": "确认规格",
  "ticket-drafting": "拆分执行票",
  "awaiting-boundary-confirmation": "边界确认",
  implementation: "候选实现",
  verification: "验证",
  "awaiting-product-acceptance": "产品验收",
  accepted: "已验收",
  failed: "失败",
  cancelled: "已取消",
};

export function PanelHeader({
  index,
  title,
  hint,
}: {
  index: string;
  title: string;
  hint: string;
}) {
  return (
    <header className="panel-header">
      <span>{index}</span>
      <div>
        <h2>{title}</h2>
        <p>{hint}</p>
      </div>
    </header>
  );
}

export function RequirementForm({
  busy,
  ready,
  onSubmit,
}: {
  busy: boolean;
  ready: boolean;
  onSubmit: (data: {
    title: string;
    targetPage: string;
    requestedOutcome: string;
  }) => Promise<void>;
}) {
  // 表单只提交产品事实，不允许用户提供命令、仓库根目录等执行参数。
  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    await onSubmit({
      title: String(form.get("title") ?? ""),
      targetPage: String(form.get("targetPage") ?? ""),
      requestedOutcome: String(form.get("requestedOutcome") ?? ""),
    });
  }
  return (
    <form className="requirement-form" onSubmit={handleSubmit}>
      <label>
        <span>需求标题</span>
        <input name="title" placeholder="例如：优化赛事空状态文案" required />
      </label>
      <label>
        <span>目标页面</span>
        <input name="targetPage" defaultValue="/" required />
      </label>
      <label>
        <span>期望结果</span>
        <textarea
          name="requestedOutcome"
          placeholder="描述用户最终应该看到或做到什么，而不是指定代码怎么改。"
          rows={5}
          required
        />
      </label>
      <button
        className="primary-button"
        disabled={busy || !ready}
        type="submit"
      >
        {!ready ? "正在连接本地服务…" : busy ? "正在记录…" : "建立产品需求"}
      </button>
      <p className="form-note">输入只作为数据保存，不会拼接为 Shell 命令。</p>
    </form>
  );
}

/** 历史导航只切换页面读取的完整聚合；服务端 current requirement 不会被改写。 */
export function RequirementHistory({
  history,
  selectedId,
  onSelect,
}: {
  history: Requirement[];
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  if (!history.length) return null;
  return (
    <section className="requirement-history" aria-label="需求历史">
      <div className="section-label">Requirement History</div>
      {history.map((item) => (
        <button
          aria-current={selectedId === item.id ? "true" : undefined}
          key={item.id}
          onClick={() => onSelect(item.id)}
          type="button"
        >
          <span>
            <strong>{item.title}</strong>
            <small>
              {item.specId} · {stageLabels[item.stage]}
            </small>
          </span>
          <time>{new Date(item.updatedAt).toLocaleString("zh-CN")}</time>
        </button>
      ))}
    </section>
  );
}

/** 补齐普通工作面板不会重复展示的历史字段，确保完整快照可以被人阅读。 */
export function HistoricalRequirementRecord({
  requirement,
}: {
  requirement: Requirement;
}) {
  return (
    <section className="historical-record" aria-label="历史需求完整记录">
      <div className="section-label">Read-only Snapshot</div>
      <dl>
        <div>
          <dt>Requirement</dt>
          <dd>{requirement.id}</dd>
        </div>
        <div>
          <dt>创建时间</dt>
          <dd>{new Date(requirement.createdAt).toLocaleString("zh-CN")}</dd>
        </div>
        <div>
          <dt>最后更新</dt>
          <dd>{new Date(requirement.updatedAt).toLocaleString("zh-CN")}</dd>
        </div>
        <div>
          <dt>目标项目</dt>
          <dd>{requirement.targetSnapshot.name}</dd>
        </div>
        <div>
          <dt>目标范围</dt>
          <dd>{requirement.targetSnapshot.modificationScope}</dd>
        </div>
        <div>
          <dt>Session</dt>
          <dd>{requirement.codex.sessionId ?? "未建立"}</dd>
        </div>
        <div>
          <dt>会话转接</dt>
          <dd>{requirement.agentSessionHandoff?.path ?? "未生成"}</dd>
        </div>
      </dl>
      <div className="historical-grilling">
        <strong>Grilling 共同理解</strong>
        {requirement.grilling.questions.map((question) => (
          <div key={question.id}>
            <span>{question.title}</span>
            <p>{question.answer ?? "未回答"}</p>
          </div>
        ))}
      </div>
    </section>
  );
}

export function RequirementSummary({
  requirement,
  busyAction,
  onConfirm,
  onConfirmBoundary,
}: {
  requirement: Requirement;
  busyAction: string | null;
  onConfirm: () => void;
  onConfirmBoundary: () => void;
}) {
  return (
    <div className="requirement-summary">
      <div className="requirement-heading">
        <span>{requirement.id}</span>
        <h3>{requirement.title}</h3>
        <p>{requirement.requestedOutcome}</p>
        <code>{requirement.targetPage}</code>
      </div>
      <ol className="stage-rail">
        {requirementStages
          .filter((stage) => stage !== "failed" && stage !== "cancelled")
          .map((stage, index) => {
            // 轨道状态完全由领域阶段的顺序计算，组件不维护第二份流程状态。
            const currentIndex = requirementStages.indexOf(requirement.stage);
            const stageIndex = requirementStages.indexOf(stage);
            const state =
              stage === requirement.stage
                ? "current"
                : stageIndex < currentIndex
                  ? "done"
                  : "waiting";
            return (
              <li key={stage} data-state={state}>
                <span>{String(index + 1).padStart(2, "0")}</span>
                {stageLabels[stage]}
              </li>
            );
          })}
      </ol>
      {requirement.spec && requirement.specVersions.length === 0 ? (
        <PlanReview requirement={requirement} />
      ) : null}
      {requirement.stage === "awaiting-spec-confirmation" &&
      requirement.specVersions.length === 0 ? (
        <button
          className="primary-button"
          disabled={busyAction !== null}
          onClick={onConfirm}
          type="button"
        >
          确认 Spec 与执行票
        </button>
      ) : null}
      {requirement.stage === "awaiting-boundary-confirmation" ? (
        <button
          className="risk-button"
          disabled={busyAction !== null}
          onClick={onConfirmBoundary}
          type="button"
        >
          确认工程边界
        </button>
      ) : null}
      {requirement.stage === "implementation" ? (
        <div className="ready-callout">
          规格边界已确认，可以创建安全需求分支并执行。
        </div>
      ) : null}
      <CollaborationMetrics requirement={requirement} />
      {requirement.rules.length ? (
        <RuleManifest requirement={requirement} />
      ) : null}
    </div>
  );
}

/** 统一对话外壳：阶段差异只改变门禁和 composer 状态，不切换成另一套 Grilling 表单。 */
export function ConversationPanel({
  requirement,
  busyAction,
  readOnly = false,
  onReply,
  onConfirm,
  onRetry,
  onCancel,
}: {
  requirement: Requirement;
  busyAction: string | null;
  readOnly?: boolean;
  onReply: (reply: string) => Promise<void>;
  onConfirm: () => void;
  onRetry: () => void;
  onCancel: () => void;
}) {
  const conversation = requirement.conversation;
  const copy = conversationPhaseCopy[conversation.phase];
  const canCompose = copy.composerStatuses.includes(conversation.status);
  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    const reply = String(data.get("reply") ?? "").trim();
    if (!reply) return;
    await onReply(reply);
    form.reset();
  }
  return (
    <section className="conversation-panel" aria-label="产品 Agent 对话">
      <ConversationPhaseRail phase={conversation.phase} />
      <ConversationTimeline requirement={requirement} />
      {readOnly ? (
        <div className="conversation-readonly">历史对话只读</div>
      ) : null}
      {!readOnly && canCompose ? (
        <form className="conversation-composer" onSubmit={handleSubmit}>
          <textarea
            aria-label={copy.composerLabel}
            name="reply"
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                event.currentTarget.form?.requestSubmit();
              }
            }}
            placeholder={copy.composerPlaceholder}
            rows={3}
            required
          />
          <div>
            <span>Enter 发送 · Shift + Enter 换行</span>
            <button
              className="primary-button"
              disabled={busyAction !== null}
              type="submit"
            >
              {busyAction?.endsWith("-reply") ? "发送中…" : "发送"}
            </button>
          </div>
        </form>
      ) : null}
      {!readOnly && conversation.status === "awaiting-confirmation" ? (
        <div className="conversation-gate">
          <p>{copy.confirmationText}</p>
          <button
            className="primary-button"
            disabled={busyAction !== null}
            onClick={onConfirm}
            type="button"
          >
            {copy.confirmationLabel}
          </button>
        </div>
      ) : null}
      {!readOnly && ["failed", "cancelled"].includes(conversation.status) ? (
        <div className="conversation-gate error">
          <p>
            {conversation.error ??
              `当前 ${copy.turnName} Turn 已取消，可以从保存的上下文重试。`}
          </p>
          <button
            className="primary-button"
            disabled={busyAction !== null}
            onClick={onRetry}
            type="button"
          >
            重试当前阶段
          </button>
        </div>
      ) : null}
      {!readOnly && conversation.status === "running" ? (
        <div className="conversation-running">
          <span>{copy.runningText}</span>
          <button
            className="secondary-button"
            disabled={busyAction?.endsWith("-cancel")}
            onClick={onCancel}
            type="button"
          >
            停止 Turn
          </button>
        </div>
      ) : null}
    </section>
  );
}

const conversationPhaseCopy: Record<
  Requirement["conversation"]["phase"],
  {
    turnName: string;
    composerStatuses: Requirement["conversation"]["status"][];
    composerLabel: string;
    composerPlaceholder: string;
    confirmationText: string;
    confirmationLabel: string;
    runningText: string;
  }
> = {
  grilling: {
    turnName: "Grilling",
    composerStatuses: ["awaiting-user"],
    composerLabel: "回复 Grilling",
    composerPlaceholder: "回答本轮所有编号问题…",
    confirmationText: "设计树 frontier 已清空。确认后才会进入 Spec。",
    confirmationLabel: "确认共同理解",
    runningText: "Agent 正在整理当前 frontier…",
  },
  "to-spec": {
    turnName: "Spec",
    composerStatuses: ["awaiting-confirmation"],
    composerLabel: "回复 Spec",
    composerPlaceholder: "提出 Spec 修订要求…",
    confirmationText: "确认当前有效版本后才会进入 Tickets。",
    confirmationLabel: "确认当前 Spec",
    runningText: "Agent 正在整理 Spec…",
  },
  "to-tickets": {
    turnName: "Tickets",
    composerStatuses: ["idle"],
    composerLabel: "修改共同理解",
    composerPlaceholder: "修改共同理解会使下游 Spec 与 Tickets 失效…",
    confirmationText: "",
    confirmationLabel: "",
    runningText: "Agent 正在整理执行票…",
  },
  implement: {
    turnName: "Implement",
    composerStatuses: [],
    composerLabel: "继续实现",
    composerPlaceholder: "",
    confirmationText: "",
    confirmationLabel: "",
    runningText: "Agent 正在执行…",
  },
};

const phases = [
  ["grilling", "Grilling"],
  ["to-spec", "Spec"],
  ["to-tickets", "Tickets"],
  ["implement", "Implement"],
] as const;

/** 四阶段轨道只读取 Requirement 中的 phase，不自行推断或推进工作流。 */
function ConversationPhaseRail({
  phase,
}: {
  phase: Requirement["conversation"]["phase"];
}) {
  const active = phases.findIndex(([id]) => id === phase);
  return (
    <ol className="conversation-phase-rail" aria-label="对话工作流">
      {phases.map(([id, label], index) => (
        <li
          data-state={
            index < active ? "done" : index === active ? "current" : "waiting"
          }
          key={id}
        >
          <span>{index + 1}</span>
          {label}
        </li>
      ))}
    </ol>
  );
}

/**
 * 订阅规范化消息 ID；用户仍在底部时跟随新内容，主动上滚后尊重阅读位置。
 * stickToBottom 放在 ref 中，滚动事件不会为每个流式 delta 触发整棵组件树更新。
 */
function ConversationTimeline({ requirement }: { requirement: Requirement }) {
  const queryClient = useQueryClient();
  const conversationQuery = useQuery({
    queryKey: workbenchQueryKeys.conversation(requirement.id),
    queryFn: () => Promise.resolve(projectConversation(requirement)),
    initialData: () => projectConversation(
      requirement,
      queryClient.getQueryData(workbenchQueryKeys.conversation(requirement.id)),
    ),
    enabled: false,
  });
  const messageIds = conversationQuery.data.messageIds;
  const viewport = useRef<HTMLDivElement>(null);
  const stickToBottom = useRef(true);
  const [showReturn, setShowReturn] = useState(false);

  useEffect(() => {
    if (!stickToBottom.current) return;
    requestAnimationFrame(() =>
      viewport.current?.scrollTo({
        top: viewport.current.scrollHeight,
        behavior: "smooth",
      }),
    );
  }, [conversationQuery.data]);

  function handleScroll() {
    const node = viewport.current;
    if (!node) return;
    const atBottom =
      node.scrollHeight - node.scrollTop - node.clientHeight < 48;
    stickToBottom.current = atBottom;
    setShowReturn(!atBottom);
  }

  function returnToBottom() {
    stickToBottom.current = true;
    setShowReturn(false);
    viewport.current?.scrollTo({
      top: viewport.current.scrollHeight,
      behavior: "smooth",
    });
  }

  return (
    <div className="conversation-viewport-wrap">
      <div
        className="conversation-timeline"
        onScroll={handleScroll}
        ref={viewport}
      >
        {messageIds.map((id) => (
          <ConversationMessageRow id={id} key={id} requirementId={requirement.id} />
        ))}
      </div>
      {showReturn ? (
        <button
          className="return-to-bottom"
          onClick={returnToBottom}
          type="button"
        >
          回到底部 ↓
        </button>
      ) : null}
    </div>
  );
}

// 每行使用按 ID selector 并 memo；当前 assistant 更新时，已完成历史行保持引用不变。
const ConversationMessageRow = memo(function ConversationMessageRow({
  id,
  requirementId,
}: {
  id: string;
  requirementId: string;
}) {
  const { data: message } = useQuery({
    queryKey: workbenchQueryKeys.conversation(requirementId),
    queryFn: disabledConversationQuery,
    enabled: false,
    select: (conversation: ConversationProjection) => conversation.messagesById[id],
  });
  if (!message) return null;
  return (
    <article
      className="conversation-message"
      data-role={message.role}
      data-status={message.status}
    >
      <header>
        <strong>
          {message.role === "user"
            ? "你"
            : message.role === "assistant"
              ? "Agent"
              : "工作流"}
        </strong>
        <time>{new Date(message.recordedAt).toLocaleTimeString("zh-CN")}</time>
      </header>
      {message.segments.map((segment) => (
        <ConversationSegmentView
          key={segment.id}
          requirementId={requirementId}
          segment={segment}
          streaming={message.status === "streaming"}
        />
      ))}
      {message.status === "streaming" ? (
        <span className="streaming-caret" aria-label="正在生成" />
      ) : null}
    </article>
  );
});

function ConversationSegmentView({
  requirementId,
  segment,
  streaming,
}: {
  requirementId: string;
  segment: ConversationMessage["segments"][number];
  streaming: boolean;
}) {
  if (segment.type === "text")
    return (
      <StreamingMarkdown content={segment.content} streaming={streaming} />
    );
  if (segment.type === "gate")
    return (
      <div className="conversation-system">
        <strong>{segment.label}</strong>
        <span>{segment.detail}</span>
      </div>
    );
  if (segment.type === "spec")
    return <SpecArtifactCard requirementId={requirementId} versionId={segment.versionId} />;
  return (
    <details className="conversation-activity">
      <summary>
        {segment.label}
        <span>{segment.status}</span>
      </summary>
      <pre>{segment.detail}</pre>
    </details>
  );
}

/** Spec artifact 只订阅自己的版本；确认状态变化不会重绘整条历史时间线。 */
function SpecArtifactCard({
  requirementId,
  versionId,
}: {
  requirementId: string;
  versionId: string;
}) {
  const { data: version } = useQuery({
    queryKey: workbenchQueryKeys.conversation(requirementId),
    queryFn: disabledConversationQuery,
    enabled: false,
    select: (conversation: ConversationProjection) => conversation.specVersionsById[versionId],
  });
  if (!version)
    return <div className="spec-artifact missing">Spec artifact 不可用</div>;
  const sections: Array<[string, string | string[]]> = [
    ["问题", version.document.problem],
    ["方案", version.document.solution],
    ["用户故事", version.document.userStories],
    ["实现决定", version.document.implementationDecisions],
    ["测试决定", version.document.testingDecisions],
    ["非目标", version.document.outOfScope],
    ["补充说明", version.document.furtherNotes],
  ];
  return (
    <article className="spec-artifact" data-status={version.status}>
      <header>
        <strong>Spec v{version.version}</strong>
        <span>{specStatusLabel(version.status)}</span>
      </header>
      <code>{version.path}</code>
      {sections.map(([title, content], index) => (
        <details key={title} open={index < 2}>
          <summary>{title}</summary>
          {Array.isArray(content) ? (
            <ol>
              {content.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ol>
          ) : (
            <p>{content}</p>
          )}
        </details>
      ))}
    </article>
  );
}

function disabledConversationQuery(): Promise<ConversationProjection> {
  return Promise.reject(new Error('Conversation Query 尚未水合'));
}

function specStatusLabel(
  status: Requirement["specVersions"][number]["status"],
) {
  return status === "draft"
    ? "待确认"
    : status === "confirmed"
      ? "已确认"
      : "已失效";
}

const markdownPlugins = [remarkGfm];

/** 流式期间只让稳定 Markdown 进入解析器；未闭合代码围栏保留为纯文本直到完成。 */
const StreamingMarkdown = memo(function StreamingMarkdown({
  content,
  streaming,
}: {
  content: string;
  streaming: boolean;
}) {
  const { stable, streamingCode } = partitionStreamingMarkdown(
    content,
    streaming,
  );
  return (
    <div className="conversation-text markdown-body">
      {stable ? (
        <ReactMarkdown remarkPlugins={markdownPlugins}>{stable}</ReactMarkdown>
      ) : null}
      {streamingCode ? (
        <pre className="streaming-code">{streamingCode}</pre>
      ) : null}
    </div>
  );
});

export function AgentConsole({
  requirement,
  busyAction,
  readOnly = false,
  onExecute,
  onCancel,
  onHandoff,
}: {
  requirement: Requirement;
  busyAction: string | null;
  readOnly?: boolean;
  onExecute: () => void;
  onCancel: () => void;
  onHandoff: (focus: string) => Promise<void> | void;
}) {
  // completed 后必须先走验证，不能再次执行；failed/cancelled 则允许在同一需求上恢复。
  const canExecute =
    !readOnly &&
    requirement.stage === "implementation" &&
    requirement.codex.status !== "running" &&
    requirement.codex.status !== "completed";
  return (
    <div className="agent-console">
      <div className="agent-placeholder">
        <span className="agent-mark">A</span>
        <div>
          <strong>连续 Codex Session</strong>
          <p data-testid="codex-status">
            {requirement.codex.status} ·{" "}
            {requirement.codex.sessionId ?? "尚未建立 session"}
          </p>
        </div>
      </div>
      <div className="ticket-stack">
        {requirement.tickets.map((ticket) => (
          <div
            className="ticket-row"
            data-status={ticket.status}
            key={ticket.id}
          >
            <code>{ticket.id}</code>
            <span>{ticket.title}</span>
            <strong>{ticket.status}</strong>
          </div>
        ))}
      </div>
      {requirement.refinements.length ? (
        <div className="refinement-history" aria-label="返修记录">
          {requirement.refinements.map((round) => (
            <article key={round.id}>
              <header>
                <strong>{round.id}</strong>
                <span>{round.status}</span>
              </header>
              <p>{round.requestedOutcome}</p>
              <small>
                {round.ticketId} · 上一版证据 {round.baselineValidation.length}{" "}
                项
              </small>
            </article>
          ))}
        </div>
      ) : null}
      {requirement.branch ? (
        <div
          className={
            requirement.branch.blockedReason ? "git-facts blocked" : "git-facts"
          }
        >
          <div>
            <span>Base</span>
            <code>
              {requirement.branch.baseBranch} ·{" "}
              {requirement.branch.baseRevision.slice(0, 8)}
            </code>
          </div>
          <div>
            <span>Branch</span>
            <code>{requirement.branch.requirementBranch ?? "未创建"}</code>
          </div>
          {requirement.branch.blockedReason ? (
            <p>{requirement.branch.blockedReason}</p>
          ) : null}
          {requirement.branch.startingWorkingTree.slice(0, 8).map((entry) => (
            <small key={`${entry.status}-${entry.path}`}>
              {entry.status} {entry.path}
            </small>
          ))}
        </div>
      ) : null}
      <div className="agent-actions">
        {canExecute ? (
          <button
            className="primary-button"
            disabled={busyAction !== null}
            onClick={onExecute}
            type="button"
          >
            {busyAction === "execute"
              ? "正在检查 Git 并启动…"
              : requirement.codex.status === "failed"
                ? "重新检查并执行"
                : requirement.branch?.requirementBranch ||
                    requirement.codex.status === "cancelled"
                  ? "继续当前 Session"
                  : "创建分支并执行"}
          </button>
        ) : null}
        {!readOnly && requirement.codex.status === "running" ? (
          <button
            className="risk-button"
            disabled={busyAction !== null}
            onClick={onCancel}
            type="button"
          >
            停止当前 Turn
          </button>
        ) : null}
      </div>
      {requirement.codex.error ? (
        <div className="agent-error" role="alert">
          <strong>未能开始执行</strong>
          <span>{requirement.codex.error}</span>
        </div>
      ) : null}
      <div className="agent-events" aria-live="polite">
        {requirement.codex.events
          .slice()
          .reverse()
          .map((agentEvent) => (
            <article key={agentEvent.id}>
              <span>{agentEvent.type}</span>
              <p>{agentEvent.message}</p>
              <time>
                {new Date(agentEvent.recordedAt).toLocaleTimeString("zh-CN")}
              </time>
            </article>
          ))}
      </div>
      {requirement.codex.changedFiles.length ? (
        <div className="changed-files" data-testid="changed-files">
          <strong>Changed files</strong>
          {requirement.codex.changedFiles.map((file) => (
            <code key={file}>{file}</code>
          ))}
        </div>
      ) : null}
      {!readOnly &&
      requirement.codex.sessionId &&
      requirement.codex.status !== "running" ? (
        <AgentHandoffForm
          requirement={requirement}
          busy={busyAction !== null}
          onSubmit={onHandoff}
        />
      ) : null}
    </div>
  );
}

function AgentHandoffForm({
  requirement,
  busy,
  onSubmit,
}: {
  requirement: Requirement;
  busy: boolean;
  onSubmit: (focus: string) => Promise<void> | void;
}) {
  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await onSubmit(
      String(new FormData(event.currentTarget).get("focus") ?? ""),
    );
  }
  return (
    <form className="agent-handoff-form" onSubmit={handleSubmit}>
      <strong>Agent 会话转接</strong>
      <p>
        调用本地 $handoff Skill 写入系统临时目录，不进入 Frontend Delivery
        Handoff commit。
      </p>
      <input
        name="focus"
        defaultValue="继续当前产品需求"
        maxLength={1000}
        aria-label="会话转接焦点"
      />
      <button disabled={busy} type="submit">
        {busy ? "正在生成…" : "生成 Agent 会话转接"}
      </button>
      {requirement.agentSessionHandoff ? (
        <small data-testid="agent-handoff-path">
          临时文件：{requirement.agentSessionHandoff.path}
        </small>
      ) : null}
    </form>
  );
}

export function ValidationPanel({
  requirement,
  busyAction,
  onRerun,
  onAccept,
  onRefine,
}: {
  requirement: Requirement;
  busyAction: string | null;
  onRerun: () => void;
  onAccept: () => void;
  onRefine: (requestedOutcome: string) => Promise<void> | void;
}) {
  // UI 只根据持久化的矩阵渲染，不通过日志文案推断验证结果。
  // disabled 是交互反馈；真正的验收门禁在 acceptCurrentRequirement 服务端函数中。
  // 开发服务热更时，已经运行的旧 executeQueue 可能只写入 completed，没有触发新增的验证转移。
  // 这个稳定的持久化组合提供显式恢复入口，避免在 GET 轮询中暗中执行验证命令。
  const canStartVerification =
    requirement.stage === "implementation" &&
    requirement.codex.status === "completed" &&
    requirement.tickets.length > 0 &&
    requirement.tickets.every((ticket) => ticket.status === "completed");
  if (
    !canStartVerification &&
    !requirement.validation.length &&
    !["verification", "awaiting-product-acceptance", "accepted"].includes(
      requirement.stage,
    )
  )
    return null;
  const hasBlockingRecord = requirement.validation.some(
    (item) => item.required && item.result !== "verified",
  );
  return (
    <section className="validation-panel" aria-label="验证与产品验收">
      <div className="section-label">Verification Matrix</div>
      <p className="acceptance-note" data-testid="candidate-status">
        候选实现状态：
        {requirement.stage === "accepted"
          ? "已验收"
          : hasBlockingRecord
            ? "验证受阻"
            : requirement.stage === "awaiting-product-acceptance"
              ? "等待产品验收"
              : "正在验证"}
      </p>
      <div className="validation-list">
        {requirement.validation.map((item) => (
          <article data-result={item.result} key={item.id}>
            <header>
              <strong>
                {item.criterionId ? `${item.criterionId} · ` : ""}
                {item.criterion}
              </strong>
              <span>{item.result}</span>
            </header>
            <p>{item.detail}</p>
            <small>
              来源：{item.source} · {item.required ? "必要门禁" : "非机器门禁"}
            </small>
            {item.command ? (
              <code>
                {item.command} · exit {item.exitCode}
              </code>
            ) : null}
            {item.notRunReason ? <small>{item.notRunReason}</small> : null}
            {item.browserEvidence ? (
              <details>
                <summary>agent-browser 证据</summary>
                <small>
                  {item.browserEvidence.targetUrl} ·{" "}
                  {item.browserEvidence.viewport.width}×
                  {item.browserEvidence.viewport.height} · refs{" "}
                  {item.browserEvidence.semanticRefs.join(", ") || "无"}
                </small>
                <pre>{item.browserEvidence.accessibilityTree}</pre>
                {item.browserEvidence.consoleErrors.length ? (
                  <pre>{item.browserEvidence.consoleErrors.join("\n")}</pre>
                ) : null}
              </details>
            ) : null}
            <time>
              {new Date(item.startedAt).toLocaleString("zh-CN")} →{" "}
              {new Date(item.completedAt).toLocaleString("zh-CN")}
            </time>
          </article>
        ))}
      </div>
      {canStartVerification || requirement.stage === "verification" ? (
        <button
          className="primary-button"
          disabled={busyAction !== null}
          onClick={onRerun}
          type="button"
        >
          {busyAction === "verify"
            ? "正在验证…"
            : canStartVerification
              ? "开始验证"
              : "重跑验证"}
        </button>
      ) : null}
      {requirement.stage === "awaiting-product-acceptance" ? (
        <>
          <p className="acceptance-note">
            验收代表候选实现符合产品意图，不代表生产合并或发布批准。
          </p>
          <button
            className="primary-button"
            disabled={busyAction !== null || hasBlockingRecord}
            onClick={onAccept}
            type="button"
          >
            {busyAction === "accept" ? "正在创建稳定提交…" : "通过产品验收"}
          </button>
          <RefinementForm busy={busyAction !== null} onSubmit={onRefine} />
        </>
      ) : null}
      {requirement.delivery ? (
        <div className="delivery-card" data-testid="delivery-card">
          <strong>Frontend Delivery Handoff</strong>
          <dl>
            <div>
              <dt>Branch</dt>
              <dd>
                <code>{requirement.delivery.branch}</code>
              </dd>
            </div>
            <div>
              <dt>Revision</dt>
              <dd>
                <code>{requirement.delivery.revision}</code>
              </dd>
            </div>
            <div>
              <dt>Changed</dt>
              <dd>{requirement.delivery.changedFiles.length} files</dd>
            </div>
            <div>
              <dt>Artifacts</dt>
              <dd>
                {requirement.delivery.artifactPaths.map((source) => (
                  <code key={source}>{source}</code>
                ))}
              </dd>
            </div>
          </dl>
          <pre>{requirement.delivery.handoff}</pre>
        </div>
      ) : null}
    </section>
  );
}

function RefinementForm({
  busy,
  onSubmit,
}: {
  busy: boolean;
  onSubmit: (requestedOutcome: string) => Promise<void> | void;
}) {
  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const requestedOutcome = String(
      new FormData(form).get("requestedOutcome") ?? "",
    );
    await onSubmit(requestedOutcome);
    form.reset();
  }
  // textarea 的内容只作为返修领域数据提交；执行命令仍来自受信任的项目配置。
  return (
    <form className="refinement-form" onSubmit={handleSubmit}>
      <label>
        <span>候选实现不符合预期？填写具体返修结果</span>
        <textarea
          name="requestedOutcome"
          placeholder="例如：按钮文案改为“立即创建”，并保持原来的点击行为。"
          maxLength={2000}
          rows={3}
          required
        />
      </label>
      <button className="risk-button" disabled={busy} type="submit">
        {busy ? "正在提交…" : "继续返修"}
      </button>
    </form>
  );
}

/** 只展示已采集事实；缺失值明确显示 uncollected，不推算虚构的效率百分比。 */
function CollaborationMetrics({ requirement }: { requirement: Requirement }) {
  const firstPreviewLead = durationBetween(
    requirement.createdAt,
    requirement.metrics.firstPreviewAt,
  );
  const acceptanceLead = durationBetween(
    requirement.createdAt,
    requirement.metrics.acceptedAt,
  );
  return (
    <section className="metrics-card" aria-label="协作指标">
      <div className="section-label">Collaboration Metrics</div>
      <dl>
        <div>
          <dt>需求建立</dt>
          <dd>{formatMetricTime(requirement.createdAt)}</dd>
        </div>
        <div>
          <dt>首次预览</dt>
          <dd>
            {formatMetricTime(requirement.metrics.firstPreviewAt)}
            {firstPreviewLead ? ` · lead ${firstPreviewLead}` : ""}
          </dd>
        </div>
        <div>
          <dt>产品验收</dt>
          <dd>
            {formatMetricTime(requirement.metrics.acceptedAt)}
            {acceptanceLead ? ` · lead ${acceptanceLead}` : ""}
          </dd>
        </div>
        <div>
          <dt>完成修改轮次</dt>
          <dd data-testid="modification-rounds">
            {requirement.modificationRounds}
          </dd>
        </div>
      </dl>
    </section>
  );
}

function formatMetricTime(value: string | null) {
  return value ? new Date(value).toLocaleString("zh-CN") : "uncollected";
}

function durationBetween(start: string, end: string | null) {
  if (!end) return null;
  const milliseconds = new Date(end).getTime() - new Date(start).getTime();
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return null;
  const seconds = Math.round(milliseconds / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  return `${Math.round(minutes / 60)}h`;
}

function PlanReview({ requirement }: { requirement: Requirement }) {
  const exploration = requirement.spec?.exploration;
  return (
    <div className="plan-review">
      <div className="section-label">候选 Spec</div>
      <h4>{requirement.spec?.title}</h4>
      <p>{requirement.spec?.desiredBehavior}</p>
      <dl>
        <div>
          <dt>拟修改范围</dt>
          <dd>{requirement.spec?.proposedScope}</dd>
        </div>
        <div>
          <dt>执行票</dt>
          <dd>{requirement.tickets.length} 张，串行执行</dd>
        </div>
        <div>
          <dt>已检查代码</dt>
          <dd>{exploration?.inspectedFileCount ?? 0} 个文件</dd>
        </div>
      </dl>
      {exploration?.candidateCodeLocations.length ? (
        <ul className="criteria-list">
          {exploration.candidateCodeLocations.map((source) => (
            <li key={source}>
              <code>{source}</code>
            </li>
          ))}
        </ul>
      ) : null}
      <ul className="criteria-list">
        {requirement.spec?.acceptanceCriteria.map((criterion) => (
          <li key={criterion}>{criterion}</li>
        ))}
      </ul>
      {requirement.boundaryItems.length ? (
        <div className="boundary-list">
          {requirement.boundaryItems.map((item) => (
            <p key={item.id}>⚠ {item.reason}</p>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function RuleManifest({ requirement }: { requirement: Requirement }) {
  // 工作流步骤解释 SDD 进度；目标项目 Skills 才是执行 Agent 可读取的仓库能力。
  return (
    <div className="rule-manifest">
      <div className="section-label">生效规则</div>
      {requirement.rules.map((rule) => (
        <details key={rule.source} className="rule-card">
          <summary>
            <span className="active-badge">生效</span>
            <strong>{rule.source}</strong>
          </summary>
          <p>{rule.summary}</p>
          <small>{rule.activationReason}</small>
          <pre>{rule.content}</pre>
        </details>
      ))}
      <SkillGroup title="工作流步骤" skills={requirement.workflow} />
      <SkillGroup title="目标项目 Skills" skills={requirement.skills} />
    </div>
  );
}

function SkillGroup({
  title,
  skills,
}: {
  title: string;
  skills: Requirement["skills"] | Requirement["workflow"];
}) {
  return (
    <div className="skill-group">
      <h5>{title}</h5>
      {skills.map((skill) => {
        const completed =
          skill.name === "grilling" &&
          skill.activationReason === "共同理解已确认";
        return (
          <div className="skill-row" key={`${title}-${skill.name}`}>
            <span
              className={
                skill.active || completed ? "active-badge" : "available-badge"
              }
            >
              {completed ? "完成" : skill.active ? "生效" : "可用"}
            </span>
            <div>
              <strong>{skill.name}</strong>
              <p>{skill.activationReason}</p>
            </div>
          </div>
        );
      })}
    </div>
  );
}

export function EvidenceTimeline({
  requirement,
}: {
  requirement: Requirement | null;
}) {
  if (!requirement)
    return (
      <div className="empty-copy">
        建立产品需求后，这里会出现可核查的阶段证据。
      </div>
    );
  // 存储按时间正序追加，界面倒序显示，让最近动作保持在最上方。
  return (
    <div className="evidence-list">
      {requirement.evidence
        .slice()
        .reverse()
        .map((record, index) => (
          <article key={`${record.recordedAt}-${index}`}>
            <header>
              <span>{stageLabels[record.stage]}</span>
              <time>
                {new Date(record.recordedAt).toLocaleTimeString("zh-CN")}
              </time>
            </header>
            <dl>
              <div>
                <dt>输入</dt>
                <dd>{record.input}</dd>
              </div>
              <div>
                <dt>动作</dt>
                <dd>{record.action}</dd>
              </div>
              <div>
                <dt>输出</dt>
                <dd>{record.output}</dd>
              </div>
            </dl>
          </article>
        ))}
    </div>
  );
}

export function RuntimeToolbar({
  snapshot,
  busyAction,
  previewUrl,
  targetPage,
  onStart,
  onStop,
  onRefresh,
}: {
  snapshot: WorkbenchSnapshot;
  busyAction: string | null;
  previewUrl: string | null;
  targetPage?: string;
  onStart: () => void;
  onStop: () => void;
  onRefresh: () => void;
}) {
  // ready 代表健康检查和预览网关都已就绪；starting 时同样允许用户主动停止。
  const running = ["starting", "ready"].includes(snapshot.process.status);
  return (
    <div className="runtime-toolbar">
      <button
        disabled={busyAction !== null || running || !snapshot.target}
        onClick={onStart}
        type="button"
      >
        {busyAction === "start" ? "启动中…" : "启动项目"}
      </button>
      <button
        disabled={busyAction !== null || !running}
        onClick={onStop}
        type="button"
      >
        停止
      </button>
      <button disabled={!previewUrl} onClick={onRefresh} type="button">
        刷新
      </button>
      {previewUrl ? (
        <a
          href={`${snapshot.process.previewUrl}${targetPage ?? snapshot.requirement?.targetPage ?? "/"}`}
          rel="noreferrer"
          target="_blank"
        >
          外部打开 ↗
        </a>
      ) : null}
    </div>
  );
}

export function RuntimeFacts({ snapshot }: { snapshot: WorkbenchSnapshot }) {
  return (
    <dl className="runtime-facts">
      <div>
        <dt>状态</dt>
        <dd data-testid="process-status">{snapshot.process.status}</dd>
      </div>
      <div>
        <dt>所有权</dt>
        <dd data-testid="process-ownership">{snapshot.process.ownership}</dd>
      </div>
      <div>
        <dt>PID</dt>
        <dd>{snapshot.process.pid ?? "—"}</dd>
      </div>
      <div>
        <dt>命令</dt>
        <dd>{snapshot.process.command || "—"}</dd>
      </div>
      <div>
        <dt>健康地址</dt>
        <dd>{snapshot.process.healthUrl || "—"}</dd>
      </div>
    </dl>
  );
}

import { randomUUID } from "node:crypto";

import type {
  AgentEvent,
  GrillingUnderstanding,
  Requirement,
  WorkbenchConversation,
} from "../domain/workbench";
import { resolveWorkbenchSkillPath } from "./workbench-skills";

export const GRILLING_READY_MARKER = "[WORKBENCH:READY_FOR_CONFIRMATION]";
export const GRILLING_UNDERSTANDING_START = "[WORKBENCH:UNDERSTANDING]";
export const GRILLING_UNDERSTANDING_END = "[/WORKBENCH:UNDERSTANDING]";

/** 创建一条持久化逻辑对话；系统事件属于对话内容，不在面板外复制阶段详情。 */
export function createWorkbenchConversation(
  requirementId: string,
  recordedAt = new Date().toISOString(),
): WorkbenchConversation {
  return {
    id: `CONVERSATION-${requirementId}`,
    phase: "grilling",
    status: "idle",
    planningThreadId: null,
    implementationThreadId: null,
    activeSkill: "grilling",
    understanding: null,
    pendingUnderstandingRevision: null,
    turnReadyForConfirmation: false,
    error: null,
    messages: [
      {
        id: randomUUID(),
        role: "system",
        phase: "grilling",
        status: "completed",
        recordedAt,
        segments: [
          {
            id: randomUUID(),
            type: "gate",
            label: "进入 Grilling",
            detail: "只读规划 · 等待 Agent 建立设计树",
          },
        ],
      },
    ],
  };
}

/** Grilling 的首轮 prompt 固定权限、Skill 入口和机器可判定的确认门禁协议。 */
export function buildGrillingPrompt(requirement: Requirement) {
  return [
    "你正在 Product Agent Workbench 的 grilling 阶段。",
    `完整读取并严格执行 Skill：${resolveWorkbenchSkillPath("grilling")}`,
    "运行权限为只读。不要修改目标项目，不要生成 Spec、Tickets 或代码。事实问题由你读取仓库解决，只向用户询问决策。",
    "每轮必须询问当前 design tree 的完整 frontier，并保持 Skill 规定的编号问题与推荐答案格式。",
    `产品需求：${requirement.title}`,
    `目标页面：${requirement.targetPage}`,
    `初始期望：${requirement.requestedOutcome}`,
    `可信目标项目：${requirement.targetSnapshot.name}`,
    `配置修改范围：${requirement.targetSnapshot.modificationScope}`,
    `当且仅当 frontier 为空时，先输出共同理解摘要，再输出 ${GRILLING_UNDERSTANDING_START}{"summary":"...","desiredOutcome":"...","changeBoundary":"...","acceptanceSignal":"..."}${GRILLING_UNDERSTANDING_END}，最后单独输出 ${GRILLING_READY_MARKER}。changeBoundary 描述产品界面或能力边界（例如“只修改 /sign-in，不修改 /sign-up”），不是仓库目录；仓库写入授权始终来自可信目标配置。四个 JSON 字段必须是非空字符串；否则不得输出确认标记。`,
  ].join("\n\n");
}

/** 把用户本轮回答交给同一规划 Thread，让 Agent 重新计算 design tree frontier。 */
export function buildGrillingReplyPrompt(reply: string) {
  return [
    "以下是用户对上一轮 frontier 的回答：",
    reply,
    "按 grilling Skill 重新计算 design tree。若还有已解锁决策，询问下一轮完整 frontier；若已为空，输出共同理解摘要和规定的确认标记。不要执行后续阶段。",
  ].join("\n\n");
}

/** Thread ID 缺失时用完整指令与持久化对话重建语境，不依赖上一个进程的内存。 */
export function buildGrillingRecoveryPrompt(requirement: Requirement) {
  const transcript = requirement.conversation.messages
    .flatMap((message) => {
      const text = message.segments
        .filter(
          (segment): segment is Extract<typeof segment, { type: "text" }> =>
            segment.type === "text",
        )
        .map((segment) => segment.content)
        .join("\n");
      return text ? [`${message.role.toUpperCase()}: ${text}`] : [];
    })
    .join("\n\n");
  return `${buildGrillingPrompt(requirement)}\n\n以下是已持久化的对话记录，请从未完成的 frontier 继续：\n\n${transcript || "（尚无文本回合）"}`;
}

/** 追加用户消息时只更新持久化领域状态，不在浏览器维护第二份对话真值。 */
export function appendUserMessage(
  conversation: WorkbenchConversation,
  content: string,
  recordedAt = new Date().toISOString(),
): WorkbenchConversation {
  return {
    ...conversation,
    status: "running",
    error: null,
    messages: [
      ...conversation.messages,
      {
        id: randomUUID(),
        role: "user",
        phase: conversation.phase,
        status: "completed",
        recordedAt,
        segments: [{ id: randomUUID(), type: "text", content }],
      },
    ],
  };
}

/** 为新 Turn 预建唯一的流式 assistant 消息，后续 SDK item 都折叠进这条消息。 */
export function beginAssistantTurn(
  conversation: WorkbenchConversation,
  recordedAt = new Date().toISOString(),
): WorkbenchConversation {
  return {
    ...conversation,
    status: "running",
    turnReadyForConfirmation: false,
    error: null,
    messages: [
      ...conversation.messages,
      {
        id: randomUUID(),
        role: "assistant",
        phase: conversation.phase,
        status: "streaming",
        recordedAt,
        segments: [],
      },
    ],
  };
}

/**
 * 把 SDK 归一化事件折叠进当前 assistant 消息。消息事件代表该 item 的当前正文，因此文本
 * 使用替换而非追加；活动则按事件 ID 去重追加，避免重复的工具更新污染时间线。当前 exec
 * 传输通常只在 item.completed 给出完整 assistant 正文，这里也兼容未来或测试中的重复快照。
 */
export function applyPlanningEvent(
  conversation: WorkbenchConversation,
  agentEvent: AgentEvent,
  sessionId: string | null,
): WorkbenchConversation {
  const messages = [...conversation.messages];
  const activeIndex = findStreamingAssistant(messages);
  if (activeIndex < 0)
    return {
      ...conversation,
      planningThreadId: sessionId ?? conversation.planningThreadId,
    };
  const active = messages[activeIndex];

  if (agentEvent.type === "message") {
    const understanding = parseUnderstanding(agentEvent.message);
    const ready =
      agentEvent.message.includes(GRILLING_READY_MARKER) &&
      understanding !== null;
    const content = stripControlProtocol(agentEvent.message).trim();
    const textIndex = active.segments.findIndex(
      (segment) => segment.type === "text" && segment.id === agentEvent.id,
    );
    const segments = [...active.segments];
    const text = { id: agentEvent.id, type: "text" as const, content };
    if (textIndex >= 0) segments[textIndex] = text;
    else segments.push(text);
    messages[activeIndex] = {
      ...active,
      segments,
    };
    return {
      ...conversation,
      messages,
      planningThreadId: sessionId ?? conversation.planningThreadId,
      turnReadyForConfirmation: conversation.turnReadyForConfirmation || ready,
      understanding: ready ? understanding : conversation.understanding,
      status: "running",
    };
  }

  if (agentEvent.type === "tool" || agentEvent.type === "file") {
    const activity = {
      id: agentEvent.id,
      type: "activity" as const,
      label: agentEvent.type === "file" ? "文件检查" : "Agent 活动",
      detail: agentEvent.message,
      status: agentEvent.activityStatus ?? "completed",
    };
    const existingIndex = active.segments.findIndex(
      (segment) => segment.type === "activity" && segment.id === agentEvent.id,
    );
    const segments = [...active.segments];
    if (existingIndex >= 0) segments[existingIndex] = activity;
    else segments.push(activity);
    messages[activeIndex] = { ...active, segments };
    return {
      ...conversation,
      messages,
      planningThreadId: sessionId ?? conversation.planningThreadId,
    };
  }

  if (agentEvent.type === "failed") {
    messages[activeIndex] = {
      ...active,
      status: "failed",
      segments: [
        ...active.segments,
        {
          id: agentEvent.id,
          type: "activity",
          label: `${conversation.phase === "to-spec" ? "Spec" : "Grilling"} 失败`,
          detail: agentEvent.message,
          status: "failed",
        },
      ],
    };
    return {
      ...conversation,
      messages,
      planningThreadId: sessionId ?? conversation.planningThreadId,
      status: "failed",
      error: agentEvent.message,
    };
  }

  if (agentEvent.type === "completed") {
    messages[activeIndex] = { ...active, status: "completed" };
    return {
      ...conversation,
      messages,
      planningThreadId: sessionId ?? conversation.planningThreadId,
      status: conversation.turnReadyForConfirmation
        ? "awaiting-confirmation"
        : "awaiting-user",
    };
  }

  return {
    ...conversation,
    planningThreadId: sessionId ?? conversation.planningThreadId,
  };
}

/**
 * 从 Agent 文本中的隐藏控制块提取结构化共同理解。
 * 四个必填字段任一缺失或 JSON 非法都返回 null，使 READY 文本无法越过服务端门禁。
 */
function parseUnderstanding(message: string): GrillingUnderstanding | null {
  const start = message.indexOf(GRILLING_UNDERSTANDING_START);
  const end = message.indexOf(
    GRILLING_UNDERSTANDING_END,
    start + GRILLING_UNDERSTANDING_START.length,
  );
  if (start < 0 || end < 0) return null;
  try {
    const value = JSON.parse(
      message.slice(start + GRILLING_UNDERSTANDING_START.length, end),
    ) as Record<string, unknown>;
    const fields = [
      "summary",
      "desiredOutcome",
      "changeBoundary",
      "acceptanceSignal",
    ] as const;
    if (
      !fields.every(
        (field) => typeof value[field] === "string" && value[field].trim(),
      )
    )
      return null;
    return Object.fromEntries(
      fields.map((field) => [field, (value[field] as string).trim()]),
    ) as GrillingUnderstanding;
  } catch {
    return null;
  }
}

/**
 * 删除只供状态机消费的 JSON 控制块与 READY 标记。
 * 返回值才进入 Markdown 渲染，用户不会看到工作台内部协议。
 */
function stripControlProtocol(message: string) {
  const start = message.indexOf(GRILLING_UNDERSTANDING_START);
  const end = message.indexOf(
    GRILLING_UNDERSTANDING_END,
    start + GRILLING_UNDERSTANDING_START.length,
  );
  const withoutUnderstanding =
    start >= 0 && end >= 0
      ? `${message.slice(0, start)}${message.slice(end + GRILLING_UNDERSTANDING_END.length)}`
      : message;
  return withoutUnderstanding.replace(GRILLING_READY_MARKER, "");
}

/** 从尾部寻找当前 Turn 的 assistant 容器，避免旧消息被新事件误改。 */
function findStreamingAssistant(messages: WorkbenchConversation["messages"]) {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    if (
      messages[index].role === "assistant" &&
      messages[index].status === "streaming"
    )
      return index;
  }
  return -1;
}

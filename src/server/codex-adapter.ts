import { Codex, type ThreadEvent } from "@openai/codex-sdk";

import type { AgentEvent } from "../domain/workbench";

type ActiveCodexTurn = { controller: AbortController };
// 单进程工作台同一时刻只允许一个 SDK Turn；globalThis 可跨开发期模块热更新保留取消句柄。
const globalCodex = globalThis as typeof globalThis & {
  __workbenchCodexTurn?: ActiveCodexTurn | null;
};

export type CodexTurnResult = { exitCode: number; sessionId: string | null };

/**
 * 通过官方 SDK 创建或恢复受限 Thread，并把易变事件压缩为应用稳定事件。
 * 默认用于实现阶段；规划阶段必须显式传 read-only。审批始终由工作台门禁控制。
 */
export async function runCodexTurn(input: {
  workspaceRoot: string;
  prompt: string;
  sessionId: string | null;
  signal?: AbortSignal;
  sandboxMode?: "read-only" | "workspace-write";
  skipGitRepositoryCheck?: boolean;
  ephemeral?: boolean;
  onEvent: (event: AgentEvent, sessionId: string | null) => Promise<void>;
}): Promise<CodexTurnResult> {
  if (globalCodex.__workbenchCodexTurn)
    throw new Error("已有 Codex turn 正在运行");
  if (input.signal?.aborted)
    return { exitCode: 130, sessionId: input.sessionId };

  const controller = new AbortController();
  const abort = () => controller.abort();
  input.signal?.addEventListener("abort", abort, { once: true });
  globalCodex.__workbenchCodexTurn = { controller };
  let sessionId = input.sessionId;
  let failed = false;
  const turnEventScope = crypto.randomUUID();

  try {
    const configuredCommand = process.env.WORKBENCH_CODEX_COMMAND?.trim();
    const codex = new Codex({
      ...(configuredCommand ? { codexPathOverride: configuredCommand } : {}),
      env: stringEnvironment(codexProcessEnvironment()),
    });
    const threadOptions = {
      workingDirectory: input.workspaceRoot,
      sandboxMode: input.sandboxMode ?? "workspace-write",
      approvalPolicy: "never" as const,
      skipGitRepoCheck: input.skipGitRepositoryCheck,
      networkAccessEnabled: false,
      threadSource: "product-agent-workbench",
    };
    // SDK 暂未公开 ephemeral Thread；临时转接仍不写入 Requirement，外部行为边界不变。
    void input.ephemeral;
    // Requirement 持久化 thread id；刷新或服务函数重建后仍能继续同一规划/实现上下文。
    const thread = sessionId
      ? codex.resumeThread(sessionId, threadOptions)
      : codex.startThread(threadOptions);
    const { events } = await thread.runStreamed(input.prompt, {
      signal: controller.signal,
    });
    for await (const sdkEvent of events) {
      console.log("SDK Event:", sdkEvent);
      if (sdkEvent.type === "thread.started") sessionId = sdkEvent.thread_id;
      if (sdkEvent.type === "turn.failed" || sdkEvent.type === "error")
        failed = true;
      await input.onEvent(
        normalizeCodexEvent(sdkEvent, JSON.stringify(sdkEvent), turnEventScope),
        sessionId,
      );
    }
    return { exitCode: failed ? 1 : 0, sessionId: thread.id ?? sessionId };
  } catch (error) {
    if (controller.signal.aborted) return { exitCode: 130, sessionId };
    const detail = error instanceof Error ? error.message : String(error);
    if (/ENOENT|spawn .*not found/i.test(detail))
      throw new Error(`Codex SDK 无法启动：${detail}`);
    await input.onEvent(event("failed", detail), sessionId);
    return { exitCode: 1, sessionId };
  } finally {
    input.signal?.removeEventListener("abort", abort);
    if (globalCodex.__workbenchCodexTurn?.controller === controller)
      globalCodex.__workbenchCodexTurn = null;
  }
}

/** 清除宿主开发模式，避免目标项目构建误以 development 运行。 */
export function codexProcessEnvironment(
  source: NodeJS.ProcessEnv = process.env,
): NodeJS.ProcessEnv {
  const environment = { ...source };
  delete environment.NODE_ENV;
  return environment;
}

/** 只中止工作台当前持有的 SDK Turn，不扫描或终止任何外部 Codex 进程。 */
export function cancelCodexTurn() {
  const active = globalCodex.__workbenchCodexTurn;
  if (!active || active.controller.signal.aborted) return false;
  active.controller.abort();
  return true;
}

/** 供重启恢复逻辑区分“仍在执行”与“只剩持久化 running 状态”。 */
export function hasActiveCodexTurn() {
  return Boolean(
    globalCodex.__workbenchCodexTurn &&
    !globalCodex.__workbenchCodexTurn.controller.signal.aborted,
  );
}

/** 把 SDK ThreadEvent 压缩为页面和持久层共享的稳定事件类型。 */
export function normalizeCodexEvent(
  value: ThreadEvent | unknown,
  raw: string,
  eventScope = "",
): AgentEvent {
  if (!value || typeof value !== "object") return event("unknown", raw);
  const record = value as Record<string, unknown>;
  const rawType = String(record.type ?? "unknown");
  const item =
    record.item && typeof record.item === "object"
      ? (record.item as Record<string, unknown>)
      : null;
  const itemType = String(item?.type ?? "");
  const errorValue =
    record.error && typeof record.error === "object"
      ? (record.error as Record<string, unknown>).message
      : null;
  const fileChanges = Array.isArray(item?.changes)
    ? item.changes.flatMap((change) =>
        change &&
        typeof change === "object" &&
        typeof (change as Record<string, unknown>).path === "string"
          ? [(change as Record<string, unknown>).path as string]
          : [],
      )
    : [];
  const fallback = fileChanges.length ? fileChanges.join(", ") : rawType;
  const commandDetail =
    itemType === "command_execution"
      ? [
          item?.command,
          item?.aggregated_output,
          typeof item?.exit_code === "number" ? `exit ${item.exit_code}` : null,
        ]
          .filter(Boolean)
          .join("\n")
      : null;
  const message = String(
    errorValue ??
      record.message ??
      item?.message ??
      item?.text ??
      item?.name ??
      commandDetail ??
      item?.command ??
      fallback,
  );
  const itemId =
    typeof item?.id === "string"
      ? [eventScope, item.id].filter(Boolean).join(":")
      : undefined;
  const activityStatus = normalizeActivityStatus(
    rawType,
    item?.status,
    itemType,
  );
  if (/thread|session/.test(rawType)) return event("session", message);
  if (/message|agent_message/.test(`${rawType} ${itemType}`))
    return event("message", message, itemId);
  if (/tool|command|mcp|web_search|todo/.test(`${rawType} ${itemType}`))
    return event("tool", message, itemId, activityStatus);
  if (/file/.test(`${rawType} ${itemType}`))
    return event("file", message, itemId, activityStatus);
  if (rawType === "turn.completed") return event("completed", message);
  if (rawType === "turn.failed" || rawType === "error")
    return event("failed", message, itemId);
  if (itemType === "error") return event("tool", message, itemId, "failed");
  return event("unknown", message);
}

export function event(
  type: AgentEvent["type"],
  message: string,
  id: string = crypto.randomUUID(),
  activityStatus?: AgentEvent["activityStatus"],
): AgentEvent {
  const safeMessage = redact(message);
  // Assistant 文本是可恢复对话本体，不能沿用活动日志的限长，否则末尾门禁标记会丢失。
  return {
    id,
    type,
    message: type === "message" ? safeMessage : safeMessage.slice(0, 2000),
    activityStatus,
    recordedAt: new Date().toISOString(),
  };
}

function normalizeActivityStatus(
  rawType: string,
  itemStatus: unknown,
  itemType: string,
): AgentEvent["activityStatus"] {
  // 优先信任 SDK item.status；缺失时再从 started/completed 顶层事件推导生命周期。
  if (itemType === "error" || itemStatus === "failed") return "failed";
  if (itemStatus === "completed" || rawType === "item.completed")
    return "completed";
  return "running";
}

function stringEnvironment(source: NodeJS.ProcessEnv): Record<string, string> {
  return Object.fromEntries(
    Object.entries(source).filter(
      (entry): entry is [string, string] => typeof entry[1] === "string",
    ),
  );
}

/** 在事件进入持久层和浏览器之前移除常见凭据形态。 */
function redact(value: string) {
  return value
    .replace(
      /(token|password|secret|authorization)(\s*[:=]\s*)([^\s,;]+)/gi,
      "$1$2[REDACTED]",
    )
    .replace(/Bearer\s+[A-Za-z0-9._~+/-]+=*/gi, "Bearer [REDACTED]");
}

import { Codex, type ThreadEvent } from "@openai/codex-sdk";

import type { AgentEvent } from "../domain/workbench";
import {
  CodexAppServerLaunchError,
  CodexAppServerTurn,
  type AppServerNotification,
} from "./codex-app-server";

type ActiveCodexTurn = {
  cancel: () => boolean;
  isCancelled: () => boolean;
};
// 单进程工作台同一时刻只允许一个 Turn；globalThis 可跨开发期模块热更新保留取消句柄。
const globalCodex = globalThis as typeof globalThis & {
  __workbenchCodexTurn?: ActiveCodexTurn | null;
};

export type CodexTurnResult = { exitCode: number; sessionId: string | null };
export type CodexTextDelta = {
  itemId: string;
  turnId: string;
  delta: string;
  text: string;
};
type CodexTurnInput = {
  workspaceRoot: string;
  prompt: string;
  sessionId: string | null;
  signal?: AbortSignal;
  sandboxMode?: "read-only" | "workspace-write";
  skipGitRepositoryCheck?: boolean;
  ephemeral?: boolean;
  onEvent: (event: AgentEvent, sessionId: string | null) => Promise<void>;
  onTextDelta?: (delta: CodexTextDelta, sessionId: string | null) => Promise<void>;
};

/**
 * 默认通过 app-server 创建或恢复受限 Thread；SDK 仅保留为显式回退。
 * 两种后端都压缩为相同的应用事件，调用方不感知协议差异。
 */
export async function runCodexTurn(
  input: CodexTurnInput,
): Promise<CodexTurnResult> {
  if (globalCodex.__workbenchCodexTurn)
    throw new Error("已有 Codex turn 正在运行");
  if (input.signal?.aborted)
    return { exitCode: 130, sessionId: input.sessionId };

  const backend = process.env.WORKBENCH_CODEX_BACKEND?.trim() || "app-server";
  if (backend === "sdk") return runSdkCodexTurn(input);
  if (backend !== "app-server")
    throw new Error(`未知的 Codex 后端：${backend}`);

  return runAppServerCodexTurn(input);
}

async function runAppServerCodexTurn(
  input: CodexTurnInput,
): Promise<CodexTurnResult> {
  let sessionId = input.sessionId;
  const turnEventScope = crypto.randomUUID();
  const textByItem = new Map<string, string>();
  const turn = new CodexAppServerTurn({
    workspaceRoot: input.workspaceRoot,
    prompt: input.prompt,
    sessionId,
    signal: input.signal,
    sandboxMode: input.sandboxMode ?? "workspace-write",
    ephemeral: input.ephemeral,
    environment: codexProcessEnvironment(),
    commandOverride: process.env.WORKBENCH_CODEX_COMMAND?.trim(),
    onSession: async (nextSessionId) => {
      sessionId = nextSessionId;
      await input.onEvent(event("session", nextSessionId), sessionId);
    },
    onNotification: async (notification) => {
      const textDelta = appServerTextDelta(notification, turnEventScope, textByItem);
      if (textDelta) {
        await input.onTextDelta?.(textDelta, sessionId);
        return;
      }
      const normalized = normalizeAppServerNotification(
        notification,
        turnEventScope,
      );
      if (normalized) await input.onEvent(normalized, sessionId);
    },
  });
  const active: ActiveCodexTurn = {
    cancel: () => turn.cancel(),
    isCancelled: () => turn.cancelled,
  };
  globalCodex.__workbenchCodexTurn = active;

  try {
    const result = await turn.run();
    return { exitCode: result.exitCode, sessionId: result.sessionId || sessionId };
  } catch (error) {
    if (error instanceof CodexAppServerLaunchError) throw error;
    const detail = error instanceof Error ? error.message : String(error);
    if (turn.cancelled) return { exitCode: 130, sessionId };
    await input.onEvent(event("failed", detail), sessionId);
    return { exitCode: 1, sessionId };
  } finally {
    if (globalCodex.__workbenchCodexTurn === active)
      globalCodex.__workbenchCodexTurn = null;
  }
}

function appServerTextDelta(
  notification: AppServerNotification,
  eventScope: string,
  textByItem: Map<string, string>,
): CodexTextDelta | null {
  if (notification.method !== "item/agentMessage/delta") return null;
  const itemId = String(notification.params.itemId ?? "");
  const turnId = String(notification.params.turnId ?? eventScope);
  const delta = String(notification.params.delta ?? "");
  if (!itemId || !delta) return null;
  const scopedItemId = [eventScope, itemId].filter(Boolean).join(":");
  const text = `${textByItem.get(scopedItemId) ?? ""}${delta}`;
  textByItem.set(scopedItemId, text);
  return { itemId: scopedItemId, turnId, delta: redact(delta), text: redact(text) };
}

async function runSdkCodexTurn(
  input: CodexTurnInput,
): Promise<CodexTurnResult> {

  const controller = new AbortController();
  const abort = () => controller.abort();
  input.signal?.addEventListener("abort", abort, { once: true });
  const active: ActiveCodexTurn = {
    cancel: () => {
      if (controller.signal.aborted) return false;
      controller.abort();
      return true;
    },
    isCancelled: () => controller.signal.aborted,
  };
  globalCodex.__workbenchCodexTurn = active;
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
    if (globalCodex.__workbenchCodexTurn === active)
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

/** 只中止工作台当前持有的 Turn，不扫描或终止任何外部 Codex 进程。 */
export function cancelCodexTurn() {
  const active = globalCodex.__workbenchCodexTurn;
  if (!active || active.isCancelled()) return false;
  return active.cancel();
}

/** 供重启恢复逻辑区分“仍在执行”与“只剩持久化 running 状态”。 */
export function hasActiveCodexTurn() {
  return Boolean(
    globalCodex.__workbenchCodexTurn &&
    !globalCodex.__workbenchCodexTurn.isCancelled(),
  );
}

function normalizeAppServerNotification(
  notification: AppServerNotification,
  eventScope: string,
): AgentEvent | null {
  const { method, params, raw } = notification;
  if (method.startsWith("item/agentMessage/delta")) return null;

  if (method.startsWith("item/")) {
    const item =
      params.item && typeof params.item === "object"
        ? normalizeAppServerItem(params.item as Record<string, unknown>)
        : null;
    if (!item) return null;
    return normalizeCodexEvent(
      { type: method.replaceAll("/", "."), item },
      raw,
      eventScope,
    );
  }

  if (method === "turn/completed") {
    const turn =
      params.turn && typeof params.turn === "object"
        ? (params.turn as Record<string, unknown>)
        : {};
    const status = String(turn.status ?? "completed");
    if (status === "failed") {
      const error =
        turn.error && typeof turn.error === "object"
          ? turn.error
          : { message: "Codex turn failed" };
      return normalizeCodexEvent({ type: "turn.failed", error }, raw, eventScope);
    }
    if (["cancelled", "canceled", "aborted", "interrupted"].includes(status))
      return event("cancelled", "Codex turn 已取消");
    return normalizeCodexEvent({ type: "turn.completed" }, raw, eventScope);
  }

  return null;
}

function normalizeAppServerItem(item: Record<string, unknown>) {
  const type = String(item.type ?? "");
  const normalizedType =
    type === "agentMessage"
      ? "agent_message"
      : type === "commandExecution"
        ? "command_execution"
        : type === "fileChange"
          ? "file_change"
          : type;
  return {
    ...item,
    type: normalizedType,
    aggregated_output: item.aggregatedOutput,
    exit_code: item.exitCode,
  };
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

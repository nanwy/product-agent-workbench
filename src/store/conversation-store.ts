import { createStore, type StoreApi } from "zustand/vanilla";

import type { ConversationMessage, SpecVersion } from "../domain/workbench";

type ScheduleFlush = (flush: () => void) => number;

export type ConversationProjection = {
  requirementId: string | null;
  messageIds: string[];
  messagesById: Record<string, ConversationMessage>;
  specVersionsById: Record<string, SpecVersion>;
  hydrate: (input: {
    requirementId: string;
    messages: ConversationMessage[];
    specVersions?: SpecVersion[];
  }) => void;
  appendDelta: (messageId: string, delta: string) => void;
  flush: () => void;
  finishMessage: (
    messageId: string,
    status: Extract<
      ConversationMessage["status"],
      "completed" | "failed" | "cancelled"
    >,
  ) => void;
};

/** 高频 delta 留在闭包缓冲区，每帧最多提交一次；服务端快照仍是权威状态。 */
export function createConversationStore(
  schedule: ScheduleFlush = defaultSchedule,
): StoreApi<ConversationProjection> {
  const pending = new Map<string, string>();
  let scheduled = false;

  return createStore<ConversationProjection>((set, get) => {
    const flush = () => {
      if (!pending.size) {
        scheduled = false;
        return;
      }
      const deltas = new Map(pending);
      // 先清空闭包缓冲再更新 Zustand，保证渲染期间到达的新 delta 会进入下一帧。
      pending.clear();
      scheduled = false;
      set((state) => {
        const messagesById = { ...state.messagesById };
        for (const [messageId, delta] of deltas) {
          const message = messagesById[messageId];
          if (!message || !delta) continue;
          const segments = [...message.segments];
          const textIndex = lastTextSegmentIndex(message);
          const textSegment = textIndex >= 0 ? segments[textIndex] : null;
          if (textSegment?.type === "text")
            segments[textIndex] = {
              ...textSegment,
              content: `${textSegment.content}${delta}`,
            };
          else
            segments.push({
              id: crypto.randomUUID(),
              type: "text",
              content: delta,
            });
          messagesById[messageId] = { ...message, segments };
        }
        return { messagesById };
      });
    };

    return {
      requirementId: null,
      messageIds: [],
      messagesById: {},
      specVersionsById: {},
      hydrate: ({ requirementId, messages, specVersions = [] }) => {
        const previous = get();
        // 切换 Requirement 时旧缓冲绝不能泄漏到另一条逻辑对话。
        if (previous.requirementId !== requirementId) {
          pending.clear();
          scheduled = false;
        }
        const bufferedDeltas: Array<[string, string]> = [];
        const messagesById = Object.fromEntries(
          messages.map((message) => {
            const existing =
              previous.requirementId === requirementId
                ? previous.messagesById[message.id]
                : undefined;
            const buffered = existing
              ? bufferableSnapshot(
                  existing,
                  message,
                  pending.get(message.id) ?? "",
                )
              : null;
            if (buffered) {
              if (buffered.delta)
                bufferedDeltas.push([message.id, buffered.delta]);
              return [message.id, buffered.message];
            }
            pending.delete(message.id);
            return [
              message.id,
              existing && sameMessage(existing, message) ? existing : message,
            ];
          }),
        );
        set({
          requirementId,
          messageIds: messages.map(({ id }) => id),
          messagesById,
          specVersionsById: Object.fromEntries(specVersions.map((version) => [version.id, version])),
        });
        for (const [messageId, delta] of bufferedDeltas)
          get().appendDelta(messageId, delta);
      },
      appendDelta: (messageId, delta) => {
        if (!delta) return;
        pending.set(messageId, `${pending.get(messageId) ?? ""}${delta}`);
        if (scheduled) return;
        scheduled = true;
        schedule(flush);
      },
      flush,
      finishMessage: (messageId, status) => {
        get().flush();
        set((state) => {
          const message = state.messagesById[messageId];
          if (!message) return state;
          return {
            messagesById: {
              ...state.messagesById,
              [messageId]: { ...message, status },
            },
          };
        });
      },
    };
  });
}

function bufferableSnapshot(
  existing: ConversationMessage,
  incoming: ConversationMessage,
  pendingDelta: string,
): { message: ConversationMessage; delta: string } | null {
  // hydrate 收到的是服务端完整消息快照。只有新文本确实以客户端已显示内容开头时，才能安全转换成增量；
  // 编辑历史或结构变化时返回 null，让 hydrate 直接采用完整权威快照。
  if (existing.status !== "streaming" || incoming.status !== "streaming")
    return null;
  const existingTextIndex = lastTextSegmentIndex(existing);
  const incomingTextIndex = lastTextSegmentIndex(incoming);
  if (existingTextIndex < 0 || incomingTextIndex < 0) return null;
  const existingSegment = existing.segments[existingTextIndex];
  const incomingSegment = incoming.segments[incomingTextIndex];
  if (existingSegment.type !== "text" || incomingSegment.type !== "text")
    return null;

  const effectiveContent = `${existingSegment.content}${pendingDelta}`;
  if (!incomingSegment.content.startsWith(effectiveContent)) return null;
  const segments = [...incoming.segments];
  segments[incomingTextIndex] = {
    ...incomingSegment,
    content: existingSegment.content,
  };
  return {
    message: { ...incoming, segments },
    delta: incomingSegment.content.slice(effectiveContent.length),
  };
}

/** 找到消息中实际承载流式正文的 segment；activity 可能排在正文之后。 */
function lastTextSegmentIndex(message: ConversationMessage) {
  for (let index = message.segments.length - 1; index >= 0; index -= 1) {
    if (message.segments[index].type === "text") return index;
  }
  return -1;
}

/** 相同历史消息复用对象引用，使按消息订阅的 React 组件跳过无关重渲染。 */
function sameMessage(left: ConversationMessage, right: ConversationMessage) {
  if (left === right) return true;
  if (
    left.status !== right.status ||
    left.segments.length !== right.segments.length
  )
    return false;
  return left.segments.every(
    (segment, index) =>
      JSON.stringify(segment) === JSON.stringify(right.segments[index]),
  );
}

/** 浏览器按动画帧合并；SSR/测试环境退化到约 24ms 的同等节流窗口。 */
function defaultSchedule(flush: () => void) {
  if (typeof requestAnimationFrame === "function")
    return requestAnimationFrame(flush);
  return setTimeout(flush, 24) as unknown as number;
}

export const conversationStore = createConversationStore();
// selector 按消费用途拆分，避免当前流式消息变化时整条时间线一起订阅。
export const conversationSelectors = {
  requirementId: (state: ConversationProjection) => state.requirementId,
  messageIds: (state: ConversationProjection) => state.messageIds,
  messageById: (id: string) => (state: ConversationProjection) =>
    state.messagesById[id],
  specVersionById: (id: string) => (state: ConversationProjection) =>
    state.specVersionsById[id],
};

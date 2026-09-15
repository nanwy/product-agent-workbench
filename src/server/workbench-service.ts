import { randomUUID } from "node:crypto";
import path from "node:path";

import {
  assertCreateRequirementInput,
  assertContinueRefinementInput,
  type CreateRequirementInput,
  type ContinueRefinementInput,
  type EvidenceRecord,
  type GrillingQuestion,
  type Requirement,
  type WorkbenchSnapshot,
} from "../domain/workbench";
import { createAgentSessionHandoff } from "./agent-handoff";
import { publishAgentEvent } from "./agent-events";
import { cancelCodexTurn, runCodexTurn } from "./codex-adapter";
import { loadTargetProjectConfig } from "./config";
import { workflowSteps } from "./exploration";
import { requirementStore } from "./requirement-store";
import {
  appendUserMessage,
  applyPlanningEvent,
  beginAssistantTurn,
  buildGrillingPrompt,
  buildGrillingRecoveryPrompt,
  buildGrillingReplyPrompt,
  createWorkbenchConversation,
} from "./workbench-conversation";
import { reconcileInterruptedExecution } from "./workbench-execution";
import {
  buildSpecPrompt,
  confirmSpecVersion,
  parseSpecArtifact,
  parseRevisedUnderstanding,
  nextSpecVersionIdentity,
  recordSpecVersion,
  reviseConfirmedUnderstanding,
  renderSpecMarkdown,
  stripSpecProtocol,
} from "./workbench-spec";
import { commitAcceptedCandidate } from "./git-workspace";
import {
  unstageDeliveryArtifacts,
  writeDeliveryArtifacts,
} from "./delivery-artifacts";
import {
  getTargetProcessSnapshot,
  startTargetProject,
  stopTargetProject,
} from "./target-runtime";

// 开发期热更新后仍保留规划 Turn 生命周期，避免取消/重试落到不同模块实例而产生并发 Turn。
const globalPlanning = globalThis as typeof globalThis & {
  __workbenchPlanningTurn?: Promise<void> | null;
};

/**
 * 组合当前工作台的权威读取模型。
 *
 * 并行读取 Requirement 与目标配置，再从进程管理器取得易失运行态。
 * 路由和 UI 应读取这个快照，不应分别访问底层模块后自行拼装状态。
 */
export async function getWorkbenchSnapshot(): Promise<WorkbenchSnapshot> {
  // 首次读取也承担服务重启恢复：不会重放写动作，只把失去进程的 running 变为可继续状态。
  await reconcileInterruptedExecution();
  const [requirement, history, target] = await Promise.all([
    requirementStore.load(),
    requirementStore.list(),
    loadTargetProjectConfig().catch(() => null),
  ]);

  return {
    requirement,
    history,
    target,
    // 进程是易失的运行时事实，每次读取快照时都以进程管理器为准。
    process: getTargetProcessSnapshot(target ?? undefined),
  };
}

/**
 * 创建新的 Requirement 聚合并原子保存。
 *
 * 初始化 Grilling 问题、工作流步骤、目标配置快照、Codex 空闲态和首批审计证据。
 * 保存成功后自动启动只读规划 Thread；此时不会探索目标代码或创建 Git 分支。
 */
export async function createRequirement(
  rawInput: CreateRequirementInput,
): Promise<WorkbenchSnapshot> {
  const input = assertCreateRequirementInput(rawInput);
  const target = await loadTargetProjectConfig();
  const now = new Date().toISOString();
  const requirementId = `REQ-${now.slice(0, 10).replaceAll("-", "")}-${randomUUID().slice(0, 8)}`;
  const processFacts = getTargetProcessSnapshot(target);
  // Requirement 是工作台的聚合根：一次需求从澄清到交付的事实都挂在它下面。
  const requirement: Requirement = {
    id: requirementId,
    specId: `SPEC-${now.slice(0, 10).replaceAll("-", "")}-${randomUUID().slice(0, 8)}`,
    title: input.title,
    targetPage: input.targetPage,
    requestedOutcome: input.requestedOutcome,
    stage: "intent-drafting",
    createdAt: now,
    updatedAt: now,
    modificationRounds: 0,
    metrics: { firstPreviewAt: null, acceptedAt: null },
    refinements: [],
    grilling: {
      status: "pending",
      confirmedAt: null,
      questions: createGrillingQuestions(input),
    },
    conversation: createWorkbenchConversation(requirementId, now),
    spec: null,
    specVersions: [],
    activeSpecVersionId: null,
    tickets: [],
    rules: [],
    workflow: workflowSteps(false),
    skills: [],
    boundaryItems: [],
    targetSnapshot: target,
    processFacts,
    branch: null,
    codex: {
      status: "idle",
      sessionId: null,
      activeTicketId: null,
      changedFiles: [],
      events: [],
      error: null,
    },
    validation: [],
    delivery: null,
    agentSessionHandoff: null,
    evidence: [
      evidence(
        "project-adaptation",
        `固定目标配置：${target.name}`,
        "校验仓库根目录、启动命令、健康地址和预览地址",
        `目标项目已适配，拟修改范围为 ${target.modificationScope}`,
      ),
      evidence(
        "intent-drafting",
        `${input.title} · ${input.targetPage}`,
        "把产品输入保存为产品需求，而不是作为可执行命令",
        input.requestedOutcome,
      ),
    ],
  };
  // UI 只显示一个表单不是安全边界；服务端同样禁止 API 覆盖仍可继续的活动需求。
  await requirementStore.update((current) => {
    assertCanCreateRequirement(current);
    return requirement;
  });
  const started = await claimGrillingTurn(requirement.id, ["idle"]);
  return runGrillingConversationTurn(started, buildGrillingPrompt(started));
}

/** 把用户回答写入同一逻辑对话，并恢复只读规划 Thread 计算下一轮 frontier。 */
export async function replyToCurrentGrilling(
  rawReply: string,
): Promise<WorkbenchSnapshot> {
  const reply = rawReply.trim();
  if (!reply) throw new Error("请输入对当前 Grilling 问题的回答");
  if (reply.length > 4000) throw new Error("Grilling 回答不能超过 4000 个字符");
  const requirement = await requirementStore.update((current) => {
    if (!current) throw new Error("请先创建产品需求");
    if (
      current.grilling.status !== "pending" ||
      current.conversation.phase !== "grilling"
    )
      throw new Error("当前需求不在 Grilling 阶段");
    if (current.conversation.status !== "awaiting-user")
      throw new Error("当前没有等待回答的 Grilling frontier");
    const conversation = beginAssistantTurn(
      appendUserMessage(current.conversation, reply),
    );
    return { ...current, conversation, updatedAt: new Date().toISOString() };
  });
  const prompt = requirement.conversation.planningThreadId
    ? buildGrillingReplyPrompt(reply)
    : buildGrillingRecoveryPrompt(requirement);
  return runGrillingConversationTurn(requirement, prompt);
}

/** 重试因 SDK 失败或取消而中断的当前 Grilling Turn，不创建第二套表单状态。 */
export async function retryCurrentGrilling(): Promise<WorkbenchSnapshot> {
  await globalPlanning.__workbenchPlanningTurn?.catch(() => undefined);
  const requirement = await claimGrillingTurn(undefined, [
    "failed",
    "cancelled",
  ]);
  const prompt = requirement.conversation.planningThreadId
    ? "继续刚才中断的 Grilling 回合。重新计算当前 design tree frontier；不要跨越共同理解确认门禁。"
    : buildGrillingRecoveryPrompt(requirement);
  return runGrillingConversationTurn(requirement, prompt);
}

/** 取消活动的规划 Turn；SDK AbortSignal 会负责回收其子进程。 */
export async function cancelCurrentGrilling(): Promise<WorkbenchSnapshot> {
  const now = new Date().toISOString();
  let shouldCancel = false;
  await requirementStore.update((current) => {
    if (!current) throw new Error("请先创建产品需求");
    if (
      current.conversation.phase !== "grilling" ||
      current.conversation.status !== "running"
    )
      return current;
    shouldCancel = true;
    return {
      ...current,
      conversation: {
        ...current.conversation,
        status: "cancelled",
        messages: current.conversation.messages.map(
          (message, index, messages) =>
            index === messages.length - 1 && message.status === "streaming"
              ? { ...message, status: "cancelled" as const }
              : message,
        ),
      },
      updatedAt: now,
    };
  });
  if (!shouldCancel) return getWorkbenchSnapshot();
  cancelCodexTurn();
  // 取消按钮立即返回已持久化的终态；真正重试会在 claim 前等待旧 Turn finally，因此无竞态也不阻塞 UI。
  return getWorkbenchSnapshot();
}

/** 在同一对话中提交 Spec 修订要求；成功结果产生新版本，旧草稿仍保留。 */
export async function replyToCurrentSpec(
  rawReply: string,
): Promise<WorkbenchSnapshot> {
  const reply = rawReply.trim();
  if (!reply) throw new Error("请输入 Spec 修订要求");
  if (reply.length > 4000) throw new Error("Spec 修订要求不能超过 4000 个字符");
  const requirement = await requirementStore.update((current) => {
    if (!current) throw new Error("请先创建产品需求");
    if (current.conversation.phase !== "to-spec")
      throw new Error("当前需求不在 to-spec 阶段");
    if (current.conversation.status !== "awaiting-confirmation")
      throw new Error("当前 Spec 不在可修订状态");
    return {
      ...current,
      conversation: beginAssistantTurn(
        appendUserMessage(current.conversation, reply),
      ),
      updatedAt: new Date().toISOString(),
    };
  });
  return runSpecConversationTurn(
    requirement,
    buildSpecPrompt(
      requirement,
      nextSpecVersionIdentity(requirement).path,
      reply,
    ),
  );
}

/** 从已持久化上下文重试失败或取消的 Spec Turn。 */
export async function retryCurrentSpec(): Promise<WorkbenchSnapshot> {
  await globalPlanning.__workbenchPlanningTurn?.catch(() => undefined);
  const requirement = await claimSpecTurn(["failed", "cancelled"]);
  const understandingRevision =
    requirement.conversation.pendingUnderstandingRevision;
  return runSpecConversationTurn(
    requirement,
    buildSpecPrompt(
      requirement,
      nextSpecVersionIdentity(requirement).path,
      understandingRevision
        ? `继续修改已确认的共同理解：${understandingRevision}。必须在 artifact 中返回更新后的 understanding。`
        : "重试刚才中断的 Spec Turn；保留既有版本，仅在成功后创建新版本。",
    ),
    Boolean(understandingRevision),
  );
}

/** 取消当前 Spec Turn；已经写入的历史版本不会被回滚或删除。 */
export async function cancelCurrentSpec(): Promise<WorkbenchSnapshot> {
  return cancelCurrentPlanningPhase("to-spec");
}

/** 只确认调用方看到的当前 Spec 版本；版本过期时拒绝写入。 */
export async function confirmCurrentSpec(
  versionId: string,
): Promise<WorkbenchSnapshot> {
  await requirementStore.update((current) => {
    if (!current) throw new Error("请先创建产品需求");
    return confirmSpecVersion(current, versionId);
  });
  return getWorkbenchSnapshot();
}

/** 从后续阶段修改共同理解：先失效下游，再由只读 to-spec Turn 形成新理解和新 Spec。 */
export async function reviseCurrentUnderstanding(
  rawReply: string,
): Promise<WorkbenchSnapshot> {
  const reply = rawReply.trim();
  if (!reply) throw new Error("请输入共同理解的修改内容");
  if (reply.length > 4000) throw new Error("共同理解修改不能超过 4000 个字符");
  const requirement = await requirementStore.update((current) => {
    if (!current) throw new Error("请先创建产品需求");
    if (
      current.conversation.phase !== "to-tickets" ||
      current.conversation.status !== "idle"
    )
      throw new Error("当前阶段不能修改已确认的共同理解");
    const reset = {
      ...current.conversation,
      phase: "to-spec" as const,
      activeSkill: "to-spec" as const,
      status: "idle" as const,
      turnReadyForConfirmation: false,
      pendingUnderstandingRevision: reply,
      error: null,
    };
    return {
      ...current,
      stage: "spec-drafting",
      spec: null,
      tickets: [],
      boundaryItems: [],
      activeSpecVersionId: null,
      specVersions: current.specVersions.map((version) =>
        version.status === "superseded"
          ? version
          : { ...version, status: "superseded" as const, confirmedAt: null },
      ),
      conversation: beginAssistantTurn(appendUserMessage(reset, reply)),
      updatedAt: new Date().toISOString(),
    };
  });
  return runSpecConversationTurn(
    requirement,
    buildSpecPrompt(
      requirement,
      nextSpecVersionIdentity(requirement).path,
      `用户正在修改已确认的共同理解：${reply}。必须在 artifact 中返回更新后的 understanding。`,
    ),
    true,
  );
}

/** 执行只读 to-spec Turn，并在完整协议到达后发布本地版本文件。 */
async function runSpecConversationTurn(
  requirement: Requirement,
  prompt: string,
  requireUnderstandingRevision = false,
): Promise<WorkbenchSnapshot> {
  const requirementId = requirement.id;
  let pendingDocument: ReturnType<typeof parseSpecArtifact> = null;
  let pendingUnderstanding: ReturnType<typeof parseRevisedUnderstanding> = null;
  const turn = (async () => {
    try {
      await runCodexTurn({
        workspaceRoot: requirement.targetSnapshot.repositoryRoot,
        prompt,
        sessionId: requirement.conversation.planningThreadId,
        sandboxMode: "read-only",
        onEvent: async (incomingEvent, sessionId) => {
          console.log("Spec Turn Event:", incomingEvent);
          let agentEvent = incomingEvent;
          if (agentEvent.type === "message") {
            pendingDocument =
              parseSpecArtifact(agentEvent.message) ?? pendingDocument;
            pendingUnderstanding =
              parseRevisedUnderstanding(agentEvent.message) ??
              pendingUnderstanding;
            agentEvent = {
              ...agentEvent,
              message: stripSpecProtocol(agentEvent.message),
            };
          }
          await requirementStore.update(async (current) => {
            if (!current || current.id !== requirementId)
              throw new Error("Spec 期间活动需求已变化");
            if (current.conversation.status === "cancelled") return current;
            if (agentEvent.type === "completed") {
              if (
                !pendingDocument ||
                (requireUnderstandingRevision && !pendingUnderstanding)
              ) {
                return {
                  ...current,
                  conversation: applyPlanningEvent(
                    current.conversation,
                    {
                      id: randomUUID(),
                      type: "failed",
                      message: !pendingDocument
                        ? "Spec Turn 未返回完整的结构化 artifact"
                        : "共同理解修订 Turn 未返回更新后的 understanding",
                      recordedAt: new Date().toISOString(),
                    },
                    sessionId,
                  ),
                };
              }
              let base = current;
              if (
                pendingUnderstanding &&
                JSON.stringify(pendingUnderstanding) !==
                  JSON.stringify(current.conversation.understanding)
              ) {
                assertProductChangeBoundary(
                  pendingUnderstanding.changeBoundary,
                );
                base = reviseConfirmedUnderstanding(
                  current,
                  pendingUnderstanding,
                );
              }
              const identity = nextSpecVersionIdentity(base);
              const artifactPath = await requirementStore.writeSpecArtifact(
                base.id,
                identity.version,
                renderSpecMarkdown(base, pendingDocument),
              );
              return recordSpecVersion(base, pendingDocument, {
                ...identity,
                path: artifactPath,
              });
            }
            return {
              ...current,
              conversation: applyPlanningEvent(
                current.conversation,
                agentEvent,
                sessionId,
              ),
              updatedAt: new Date().toISOString(),
            };
          });
          publishAgentEvent(agentEvent);
        },
      });
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      await requirementStore.update((current) => {
        if (!current || current.id !== requirementId)
          return current ?? requirement;
        if (current.conversation.status === "cancelled") return current;
        return {
          ...current,
          conversation: applyPlanningEvent(
            current.conversation,
            {
              id: randomUUID(),
              type: "failed",
              message: reason,
              recordedAt: new Date().toISOString(),
            },
            current.conversation.planningThreadId,
          ),
          updatedAt: new Date().toISOString(),
        };
      });
    }
  })();
  globalPlanning.__workbenchPlanningTurn = turn;
  await turn.finally(() => {
    if (globalPlanning.__workbenchPlanningTurn === turn)
      globalPlanning.__workbenchPlanningTurn = null;
  });
  return getWorkbenchSnapshot();
}

async function claimSpecTurn(
  allowedStatuses: Requirement["conversation"]["status"][],
) {
  return requirementStore.update((current) => {
    if (!current) throw new Error("请先创建产品需求");
    if (
      current.grilling.status !== "confirmed" ||
      current.conversation.phase !== "to-spec"
    )
      throw new Error("当前需求不在 to-spec 阶段");
    if (!allowedStatuses.includes(current.conversation.status))
      throw new Error("当前 Spec Turn 已在运行或无需重试");
    return {
      ...current,
      conversation: beginAssistantTurn(current.conversation),
      updatedAt: new Date().toISOString(),
    };
  });
}

async function cancelCurrentPlanningPhase(
  phase: Requirement["conversation"]["phase"],
) {
  const now = new Date().toISOString();
  let shouldCancel = false;
  await requirementStore.update((current) => {
    if (!current) throw new Error("请先创建产品需求");
    if (
      current.conversation.phase !== phase ||
      current.conversation.status !== "running"
    )
      return current;
    shouldCancel = true;
    return {
      ...current,
      conversation: {
        ...current.conversation,
        status: "cancelled",
        messages: current.conversation.messages.map(
          (message, index, messages) =>
            index === messages.length - 1 && message.status === "streaming"
              ? { ...message, status: "cancelled" as const }
              : message,
        ),
      },
      updatedAt: now,
    };
  });
  if (shouldCancel) cancelCodexTurn();
  return getWorkbenchSnapshot();
}

/**
 * 执行一个 Grilling Codex Turn，并把每个归一化事件原子折叠到 Requirement。
 * Promise 保存在 globalThis，取消可以立即反馈，而重试会等待旧 Turn 完成 finally 后再启动。
 */
async function runGrillingConversationTurn(
  requirement: Requirement,
  prompt: string,
): Promise<WorkbenchSnapshot> {
  const requirementId = requirement.id;
  const turn = (async () => {
    let result: Awaited<ReturnType<typeof runCodexTurn>>;
    try {
      result = await runCodexTurn({
        workspaceRoot: requirement.targetSnapshot.repositoryRoot,
        prompt,
        sessionId: requirement.conversation.planningThreadId,
        sandboxMode: "read-only",
        onEvent: async (agentEvent, sessionId) => {
          await requirementStore.update((current) => {
            if (!current || current.id !== requirementId)
              throw new Error("Grilling 期间活动需求已变化");
            if (current.conversation.status === "cancelled") return current;
            return {
              ...current,
              conversation: applyPlanningEvent(
                current.conversation,
                agentEvent,
                sessionId,
              ),
              updatedAt: new Date().toISOString(),
            };
          });
          publishAgentEvent(agentEvent);
        },
      });
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      await requirementStore.update((current) => {
        if (!current || current.id !== requirementId)
          return current ?? requirement;
        const failedEvent = {
          id: randomUUID(),
          type: "failed" as const,
          message: reason,
          recordedAt: new Date().toISOString(),
        };
        return {
          ...current,
          conversation: applyPlanningEvent(
            current.conversation,
            failedEvent,
            current.conversation.planningThreadId,
          ),
          updatedAt: new Date().toISOString(),
        };
      });
      return;
    }
    if (result.exitCode !== 0) {
      await requirementStore.update((current) => {
        if (
          !current ||
          current.id !== requirementId ||
          ["failed", "cancelled"].includes(current.conversation.status)
        )
          return current ?? requirement;
        const reason = `Grilling Turn 未完成（${result.exitCode}）`;
        return {
          ...current,
          conversation: {
            ...current.conversation,
            status: "failed",
            error: reason,
          },
          updatedAt: new Date().toISOString(),
        };
      });
    }
  })();
  globalPlanning.__workbenchPlanningTurn = turn;
  await turn.finally(() => {
    if (globalPlanning.__workbenchPlanningTurn === turn)
      globalPlanning.__workbenchPlanningTurn = null;
  });
  return getWorkbenchSnapshot();
}

/**
 * 原子认领 Grilling Turn：在同一次 store update 中校验阶段并创建 assistant 占位消息。
 * 这样双击或两个页面同时回复时，只有第一个请求能从允许状态进入 running。
 */
async function claimGrillingTurn(
  requirementId: string | undefined,
  allowedStatuses: Requirement["conversation"]["status"][],
) {
  return requirementStore.update((current) => {
    if (!current) throw new Error("请先创建产品需求");
    if (requirementId && current.id !== requirementId)
      throw new Error("Grilling 需求已变化");
    if (
      current.grilling.status !== "pending" ||
      current.conversation.phase !== "grilling"
    )
      throw new Error("当前需求不在 Grilling 阶段");
    if (!allowedStatuses.includes(current.conversation.status))
      throw new Error("当前 Grilling 回合已在运行或无需重试");
    return {
      ...current,
      conversation: beginAssistantTurn(current.conversation),
      updatedAt: new Date().toISOString(),
    };
  });
}

/**
 * 确认 Grilling 回答并冻结本轮共同理解。
 *
 * 要求所有问题都有答案，并验证用户确认的写入目录没有扩大配置授权范围。
 */
export async function confirmCurrentGrilling(): Promise<WorkbenchSnapshot> {
  const requirement = await requireCurrentRequirement();
  if (requirement.grilling.status === "confirmed")
    return getWorkbenchSnapshot();
  if (requirement.conversation.status !== "awaiting-confirmation")
    throw new Error("Grilling design tree 尚未达到共同理解");
  const understanding = requirement.conversation.understanding;
  if (!understanding) throw new Error("Grilling 未产生可确认的结构化共同理解");
  // 旧三题仅是下游兼容投影；值来自已确认的结构化共同理解，不再回填默认推荐。
  const answers = {
    "desired-outcome": understanding.desiredOutcome,
    "change-boundary": understanding.changeBoundary,
    "acceptance-signal": understanding.acceptanceSignal,
  };
  const questions = confirmGrillingAnswers(
    requirement.grilling.questions,
    answers,
  );
  const confirmedScope =
    questions.find((question) => question.id === "change-boundary")?.answer ??
    "";
  // 这里只确认产品表面边界；文件写入授权始终取自不可由对话改写的 targetSnapshot。
  assertProductChangeBoundary(confirmedScope);
  const now = new Date().toISOString();
  await requirementStore.save({
    ...requirement,
    stage: "spec-drafting",
    grilling: { status: "confirmed", questions, confirmedAt: now },
    conversation: {
      ...requirement.conversation,
      phase: "to-spec",
      status: "idle",
      activeSkill: "to-spec",
      turnReadyForConfirmation: false,
      error: null,
      messages: [
        ...requirement.conversation.messages,
        {
          id: randomUUID(),
          role: "system",
          phase: "grilling",
          status: "completed",
          recordedAt: now,
          segments: [
            {
              id: randomUUID(),
              type: "gate",
              label: "共同理解已确认",
              detail: "Grilling 完成 · 等待进入 to-spec",
            },
          ],
        },
      ],
    },
    workflow: workflowSteps(true),
    updatedAt: now,
    evidence: [
      ...requirement.evidence,
      evidence(
        "intent-drafting",
        understanding.summary,
        "完成 grilling 设计树并确认共同理解",
        "需求可以进入只读探索与 Spec 生成",
      ),
    ],
  });
  const claimed = await claimSpecTurn(["idle"]);
  return runSpecConversationTurn(
    claimed,
    buildSpecPrompt(claimed, nextSpecVersionIdentity(claimed).path),
  );
}

/**
 * 确认候选 Spec 与执行票。
 *
 * 无边界风险时直接开放实现；有未确认边界时先进入边界确认阶段。
 */
export async function confirmCurrentPlan(): Promise<WorkbenchSnapshot> {
  const requirement = await requireCurrentRequirement();
  assertLegacyPlanConfirmationAllowed(requirement);
  if (requirement.stage !== "awaiting-spec-confirmation") {
    throw new Error("只有待确认的 Spec 和执行票可以被确认");
  }
  const requiresBoundary = requirement.boundaryItems.some(
    (item) => !item.confirmed,
  );
  // 普通需求直接进入实现；发现跨边界影响时插入一次显式产品确认。
  const stage = requiresBoundary
    ? "awaiting-boundary-confirmation"
    : "implementation";
  const now = new Date().toISOString();
  const nextPhase = requiresBoundary ? "to-tickets" : "implement";
  await requirementStore.save({
    ...requirement,
    stage,
    conversation: advanceConversationPhase(
      requirement.conversation,
      nextPhase,
      requiresBoundary
        ? "Spec 与 Tickets 已确认 · 等待边界确认"
        : "Spec 与 Tickets 已确认 · 可以开始实现",
      now,
    ),
    updatedAt: now,
    workflow: activateImplementationStep(
      requirement.workflow,
      !requiresBoundary,
      requiresBoundary ? "等待边界确认" : "Spec 与执行票已确认",
    ),
    evidence: [
      ...requirement.evidence,
      evidence(
        stage,
        "产品确认 Spec 与执行票",
        requiresBoundary ? "暂停执行并请求边界确认" : "开放候选实现阶段",
        requiresBoundary ? "存在未确认的工程边界" : "需求已准备进入执行",
      ),
    ],
  });
  return getWorkbenchSnapshot();
}

/** 旧的 Spec+Tickets 一键确认只服务旧快照，版本化 Spec 必须按版本 ID 确认。 */
export function assertLegacyPlanConfirmationAllowed(
  requirement: Pick<Requirement, "specVersions">,
) {
  if (requirement.specVersions.length > 0)
    throw new Error(
      "版本化 Spec 必须通过当前版本门禁确认，不能调用旧计划确认入口",
    );
}

/**
 * 确认全部工程边界项并开放候选实现。
 *
 * 这里只记录产品决定和激活 implement 工作流步骤，不会直接修改目标仓库。
 */
export async function confirmCurrentBoundary(): Promise<WorkbenchSnapshot> {
  const requirement = await requireCurrentRequirement();
  if (requirement.stage !== "awaiting-boundary-confirmation") {
    throw new Error("当前产品需求不在边界确认阶段");
  }
  const now = new Date().toISOString();
  await requirementStore.save({
    ...requirement,
    stage: "implementation",
    conversation: advanceConversationPhase(
      requirement.conversation,
      "implement",
      "工程边界已确认 · 可以开始实现",
      now,
    ),
    updatedAt: now,
    boundaryItems: requirement.boundaryItems.map((item) => ({
      ...item,
      confirmed: true,
    })),
    workflow: activateImplementationStep(
      requirement.workflow,
      true,
      "Spec、执行票与工程边界均已确认",
    ),
    evidence: [
      ...requirement.evidence,
      evidence(
        "implementation",
        "产品确认全部工程边界",
        "记录边界确认并开放候选实现阶段",
        "需求已准备进入执行",
      ),
    ],
  });
  return getWorkbenchSnapshot();
}

/**
 * 按受信任配置启动目标开发服务并保存运行证据。
 *
 * 实际进程生命周期由 target-runtime 管理，本函数负责把结果接回 Requirement 聚合。
 */
export async function startConfiguredTarget(): Promise<WorkbenchSnapshot> {
  const config = await loadTargetProjectConfig();
  const processFacts = await startTargetProject(config);
  await saveProcessFacts(
    processFacts,
    "启动目标项目",
    `运行状态：${processFacts.status}，PID：${processFacts.pid ?? "—"}`,
  );
  return getWorkbenchSnapshot();
}

/**
 * 停止工作台拥有的目标进程和预览网关，并保存停止证据。
 */
export async function stopConfiguredTarget(): Promise<WorkbenchSnapshot> {
  const config = await loadTargetProjectConfig();
  const processFacts = await stopTargetProject(config);
  await saveProcessFacts(
    processFacts,
    "停止目标项目",
    "已回收工作台拥有的目标进程与预览网关",
  );
  return getWorkbenchSnapshot();
}

/**
 * 在同一 Requirement、Git 分支和 Codex session 中追加一轮产品返修。
 *
 * 产品文本只进入 Ticket 和后续 Codex prompt，不参与 Shell 命令构造。提交时保存当前
 * 验证矩阵作为上一版候选证据，然后恢复 implementation；真正执行仍由原执行队列负责。
 */
export async function continueCurrentRefinement(
  rawInput: ContinueRefinementInput,
): Promise<WorkbenchSnapshot> {
  const input = assertContinueRefinementInput(rawInput);
  const requirement = await requireCurrentRequirement();
  if (requirement.stage !== "awaiting-product-acceptance")
    throw new Error("只有待产品验收的候选实现可以继续返修");
  if (!requirement.branch?.requirementBranch || !requirement.codex.sessionId)
    throw new Error("返修必须复用已建立的需求分支和 Codex session");
  const now = new Date().toISOString();
  const nextNumber =
    requirement.tickets.reduce((largest, ticket) => {
      const value = Number(ticket.id.match(/(\d+)$/)?.[1] ?? 0);
      return Math.max(largest, value);
    }, 0) + 1;
  const ticketId = `TICKET-${String(nextNumber).padStart(2, "0")}`;
  await requirementStore.save({
    ...requirement,
    stage: "implementation",
    updatedAt: now,
    delivery: null,
    tickets: [
      ...requirement.tickets,
      {
        id: ticketId,
        title:
          input.requestedOutcome.length > 32
            ? `${input.requestedOutcome.slice(0, 31)}…`
            : input.requestedOutcome,
        status: "queued",
        acceptanceCriteria: [
          `在 ${requirement.targetPage} 验证返修结果：${input.requestedOutcome}`,
          "继续遵守原 Spec、修改范围、生效规则和生效 Skill",
        ],
      },
    ],
    // sessionId、branch、Requirement ID、Spec ID、Spec 与规则均通过展开原聚合保持不变。
    codex: {
      ...requirement.codex,
      status: "idle",
      activeTicketId: null,
      error: null,
    },
    refinements: [
      ...requirement.refinements,
      {
        id: `REFINEMENT-${String(requirement.refinements.length + 1).padStart(2, "0")}`,
        ticketId,
        requestedOutcome: input.requestedOutcome,
        requestedAt: now,
        status: "queued",
        baselineValidation: requirement.validation,
      },
    ],
    evidence: [
      ...requirement.evidence,
      evidence(
        "implementation",
        input.requestedOutcome,
        `追加返修执行票 ${ticketId}`,
        "保留原分支、Session、范围、规则和上一版验证证据",
      ),
    ],
  });
  return getWorkbenchSnapshot();
}

/** 生成并记录临时的 Agent 会话转接；它不属于前端交付 commit。 */
export async function generateCurrentAgentHandoff(
  focus: string,
): Promise<WorkbenchSnapshot> {
  const requirement = await requireCurrentRequirement();
  if (requirement.codex.status === "running")
    throw new Error("Codex 执行中不能生成会话转接");
  const handoff = await createAgentSessionHandoff(requirement, focus);
  await requirementStore.update((current) => {
    if (!current || current.id !== requirement.id)
      throw new Error("生成转接期间活动需求已变化");
    if (current.codex.status === "running")
      throw new Error("生成转接期间 Codex 已开始执行");
    return {
      ...current,
      agentSessionHandoff: handoff,
      updatedAt: handoff.createdAt,
      evidence: [
        ...current.evidence,
        evidence(
          current.stage,
          focus,
          "调用本地 $handoff Skill 生成 Agent 会话转接",
          handoff.path,
        ),
      ],
    };
  });
  return getWorkbenchSnapshot();
}

/**
 * 对通过自动验证的候选实现执行产品验收。
 *
 * 服务端重新执行阶段、失败项、分支和修改范围门禁；全部通过后创建稳定 commit，
 * 将产品验证项标记为 verified，并生成交付 Handoff。不会 push、merge 或发布。
 */
export async function acceptCurrentRequirement(): Promise<WorkbenchSnapshot> {
  const requirement = await requireCurrentRequirement();
  // 第一层门禁：只有验证编排已结束并转入待验收阶段，按钮才能生效。
  // 第二层门禁：服务端再次检查 failed，不信任前端的 disabled 状态。
  if (requirement.stage !== "awaiting-product-acceptance")
    throw new Error("只有自动验证完成的候选实现可以验收");
  if (
    requirement.validation.some(
      (item) => item.required && item.result !== "verified",
    )
  ) {
    throw new Error("存在失败或未运行的必要验证项，不能验收");
  }
  const configuredTarget = await loadTargetProjectConfig();
  const target = {
    ...configuredTarget,
    modificationScope:
      requirement.spec?.proposedScope ?? configuredTarget.modificationScope,
  };
  const acceptedAt = new Date().toISOString();
  const validation = requirement.validation.map((item) =>
    item.source === "product" && item.result === "not-run"
      ? {
          ...item,
          result: "verified" as const,
          detail:
            item.id === "product-acceptance"
              ? "产品已在工作台显式确认候选实现"
              : "产品已结合真实预览与 agent-browser 证据确认该验收条件",
          notRunReason: null,
          completedAt: acceptedAt,
        }
      : item,
  );
  const acceptedCandidate = {
    ...requirement,
    validation,
    metrics: { ...requirement.metrics, acceptedAt },
  };
  // 先生成可审查文件，再与业务代码一起暂存提交。文件中的 revision 使用“承载本文件的
  // commit”语义，避免把未知 hash 写进决定该 hash 的 tree 而形成自引用。
  const artifacts = await writeDeliveryArtifacts(acceptedCandidate, target);
  let committed: Awaited<ReturnType<typeof commitAcceptedCandidate>>;
  try {
    committed = await commitAcceptedCandidate(acceptedCandidate, target);
  } catch (error) {
    // Git commit 失败时精确恢复交付文件，并保持 Requirement 在待验收阶段。
    await unstageDeliveryArtifacts(target, artifacts.paths);
    await artifacts.rollback();
    throw error;
  }
  // Handoff 是前端审查的摘要，不是 PR 或发布单；所以只记录可审查事实，不执行 push/merge。
  const handoff = [
    `# Frontend Delivery Handoff · ${requirement.specId}`,
    "",
    `- 需求：${requirement.title}`,
    `- 分支：${committed.branch}`,
    `- 提交：${committed.revision}`,
    `- 改动文件：${committed.changedFiles.join(", ")}`,
    `- 跟踪文件：${artifacts.paths.join(", ")}`,
    `- 验证：${validation.map((item) => `${item.criterion}=${item.result}`).join("；")}`,
    "- 责任边界：产品验收已完成；生产合并与发布仍由前端工程师负责。",
  ].join("\n");
  await requirementStore.save({
    ...requirement,
    stage: "accepted",
    validation,
    delivery: {
      ...committed,
      acceptedAt,
      handoff,
      artifactPaths: artifacts.paths,
    },
    updatedAt: acceptedAt,
    metrics: { ...requirement.metrics, acceptedAt },
    evidence: [
      ...requirement.evidence,
      evidence(
        "accepted",
        "产品显式验收候选实现",
        "创建稳定提交并生成前端交付 Handoff",
        `${committed.branch} @ ${committed.revision.slice(0, 8)}`,
      ),
    ],
  });
  return getWorkbenchSnapshot();
}

/** 读取当前 Requirement；没有活动需求时统一抛出领域错误。 */
async function requireCurrentRequirement() {
  const requirement = await requirementStore.load();
  if (!requirement) throw new Error("请先创建产品需求");
  return requirement;
}

/** 创建一条只追加的阶段证据记录。 */
function evidence(
  stage: EvidenceRecord["stage"],
  input: string,
  action: string,
  output: string,
): EvidenceRecord {
  return { stage, input, action, output, recordedAt: new Date().toISOString() };
}

/** 切换 implement 工作流步骤的激活状态，同时保留其他步骤原样。 */
function activateImplementationStep(
  workflow: Requirement["workflow"],
  active: boolean,
  activationReason: string,
) {
  return workflow.map((step) =>
    step.name === "implement" ? { ...step, active, activationReason } : step,
  );
}

/** 把易失的目标进程快照同步进 Requirement，并追加对应审计证据。 */
async function saveProcessFacts(
  processFacts: Requirement["processFacts"],
  action: string,
  output: string,
) {
  // 目标进程可以脱离需求存在；有需求时再把当时的运行事实同步进审计记录。
  const requirement = await requirementStore.load();
  if (!requirement) return;
  await requirementStore.update((current) => {
    if (!current) throw new Error("产品需求已不存在");
    return {
      ...current,
      processFacts,
      metrics: {
        ...current.metrics,
        firstPreviewAt:
          processFacts.status === "ready"
            ? (current.metrics.firstPreviewAt ?? new Date().toISOString())
            : current.metrics.firstPreviewAt,
      },
      updatedAt: new Date().toISOString(),
      evidence: [
        ...current.evidence,
        evidence("project-adaptation", processFacts.command, action, output),
      ],
    };
  });
}

/** 根据产品输入建立旧快照兼容用的三道 Grilling 投影。 */
function createGrillingQuestions(input: CreateRequirementInput) {
  // 真正的追问由 Agent 决策树完成；这里保存结果、产品边界和验收信号的兼容投影。
  return [
    {
      id: "desired-outcome",
      title: "成功结果",
      prompt: "用户最终应该观察到什么结果？",
      recommendation: input.requestedOutcome,
      answer: null,
    },
    {
      id: "change-boundary",
      title: "修改边界",
      prompt: "哪些页面、交互或能力属于本次修改，哪些必须保持不变？",
      recommendation: `只修改 ${input.targetPage} 对应的产品表面，其他页面和交互保持不变`,
      answer: null,
    },
    {
      id: "acceptance-signal",
      title: "验收信号",
      prompt: "用什么可观察证据判断需求完成？",
      recommendation: `真实预览中可以验证“${input.requestedOutcome}”，且没有新增控制台错误`,
      answer: null,
    },
  ];
}

/** 阶段推进时写入系统门禁消息，使轨道状态和可恢复时间线来自同一领域事实。 */
function advanceConversationPhase(
  conversation: Requirement["conversation"],
  phase: Requirement["conversation"]["phase"],
  detail: string,
  recordedAt: string,
): Requirement["conversation"] {
  if (conversation.phase === phase) return conversation;
  return {
    ...conversation,
    phase,
    activeSkill: phase,
    status: "idle",
    messages: [
      ...conversation.messages,
      {
        id: randomUUID(),
        role: "system",
        phase,
        status: "completed",
        recordedAt,
        segments: [
          {
            id: randomUUID(),
            type: "gate",
            label: `进入 ${phase}`,
            detail,
          },
        ],
      },
    ],
  };
}

/** 阻止新请求覆盖仍可继续的唯一活动 Requirement；accepted 是唯一可替换终态。 */
export function assertCanCreateRequirement(
  current: Pick<Requirement, "stage"> | null,
) {
  if (current && current.stage !== "accepted") {
    throw new Error("当前产品需求尚未验收，不能建立第二条活动需求");
  }
}

/** 校验每道 Grilling 问题都有非空回答，并返回已去除首尾空白的确认结果。 */
export function confirmGrillingAnswers(
  questions: GrillingQuestion[],
  answers: Record<string, string>,
) {
  return questions.map((question) => {
    const answer = answers[question.id]?.trim();
    if (!answer) throw new Error(`请回答：${question.title}`);
    return { ...question, answer };
  });
}

/** 校验确认范围只能等于或位于配置范围之下，并阻止绝对路径与目录穿越。 */
export function assertConfirmedScope(
  scope: string,
  target: Requirement["targetSnapshot"],
) {
  // resolve 后再比较，防止 `../` 等路径穿越绕过仓库相对目录限制。
  const configuredRoot = path.resolve(
    target.repositoryRoot,
    target.modificationScope,
  );
  const confirmedRoot = path.resolve(target.repositoryRoot, scope);
  if (
    !scope ||
    path.isAbsolute(scope) ||
    (confirmedRoot !== configuredRoot &&
      !confirmedRoot.startsWith(`${configuredRoot}${path.sep}`))
  ) {
    throw new Error(`修改边界必须是 ${target.modificationScope} 或其子目录`);
  }
}

/** 产品对页面/能力的修改边界，与目标仓库的文件写入授权是两个不同概念。 */
export function assertProductChangeBoundary(boundary: string) {
  const value = boundary.trim();
  if (!value) throw new Error("请确认产品修改边界");
  if (value.length > 1000) throw new Error("产品修改边界不能超过 1000 个字符");
}

import { createFileRoute } from "@tanstack/react-router";
import { useServerFn } from "@tanstack/react-start";
import { useEffect, useMemo, useState } from "react";

import {
  AgentConsole,
  EvidenceTimeline,
  ConversationPanel,
  HistoricalRequirementRecord,
  PanelHeader,
  RequirementForm,
  RequirementHistory,
  RequirementSummary,
  RuntimeFacts,
  RuntimeToolbar,
  ValidationPanel,
  stageLabels,
} from "../components/workbench-panels";
import type { ConversationPhase, WorkbenchSnapshot } from "../domain/workbench";
import {
  acceptRequirement,
  cancelExecution,
  cancelGrilling,
  cancelSpec,
  confirmBoundary,
  confirmGrilling,
  confirmSpec,
  confirmPlan,
  continueRefinement,
  getWorkbench,
  generateAgentHandoff,
  replyGrilling,
  replySpec,
  retryGrilling,
  retrySpec,
  reviseUnderstanding,
  rerunVerification,
  executeRequirement,
  startTarget,
  stopTarget,
  submitRequirement,
} from "../server/functions";
import { conversationStore } from "../store/conversation-store";

// loader 负责 SSR 首屏；页面挂载后再通过轮询和 SSE 追赶后台执行产生的新状态。
export const Route = createFileRoute("/")({
  loader: () => getWorkbench(),
  component: WorkbenchPage,
});

function WorkbenchPage() {
  // 该组件是前端组合根：绑定服务端动作并把同一份快照分发给三个面板。
  const initialSnapshot = Route.useLoaderData();
  const [snapshot, setSnapshot] = useState<WorkbenchSnapshot>(initialSnapshot);
  const [hydrated, setHydrated] = useState(false);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [previewRevision, setPreviewRevision] = useState(0);
  // null 始终代表服务端当前需求；选择历史 ID 只改变本页读取视图，不修改持久状态。
  const [selectedRequirementId, setSelectedRequirementId] = useState<
    string | null
  >(null);
  const refresh = useServerFn(getWorkbench);
  const create = useServerFn(submitRequirement);
  const confirmGrill = useServerFn(confirmGrilling);
  const replyToGrill = useServerFn(replyGrilling);
  const retryGrill = useServerFn(retryGrilling);
  const stopGrill = useServerFn(cancelGrilling);
  const replyToSpec = useServerFn(replySpec);
  const retryCurrentSpec = useServerFn(retrySpec);
  const stopSpec = useServerFn(cancelSpec);
  const confirmCurrentSpec = useServerFn(confirmSpec);
  const reviseUnderstandingFromTickets = useServerFn(reviseUnderstanding);
  const confirm = useServerFn(confirmPlan);
  const confirmRisk = useServerFn(confirmBoundary);
  const start = useServerFn(startTarget);
  const stop = useServerFn(stopTarget);
  const execute = useServerFn(executeRequirement);
  const cancel = useServerFn(cancelExecution);
  const rerun = useServerFn(rerunVerification);
  const accept = useServerFn(acceptRequirement);
  const refine = useServerFn(continueRefinement);
  const handoff = useServerFn(generateAgentHandoff);

  useEffect(() => setHydrated(true), []);
  // 轮询是最终收敛通道：即使 SSE 短暂断线，页面最多三秒后仍能恢复一致。
  useEffect(() => {
    const timer = window.setInterval(() => {
      void refresh()
        .then(setSnapshot)
        .catch(() => undefined);
    }, 3000);
    return () => window.clearInterval(timer);
  }, [refresh]);
  // SSE 只发送“状态已变化”的通知，收到后仍从服务端拉取完整权威快照。
  useEffect(() => {
    const events = new EventSource("/api/events");
    events.onmessage = () =>
      void refresh()
        .then(setSnapshot)
        .catch(() => undefined);
    return () => events.close();
  }, [refresh]);

  async function runAction(
    name: string,
    action: (idempotencyKey: string) => Promise<WorkbenchSnapshot>,
  ) {
    // 所有按钮共用一条动作管线：互斥忙碌态、幂等键、错误展示和快照替换。
    setBusyAction(name);
    setError(null);
    try {
      const nextSnapshot = await action(crypto.randomUUID());
      setSnapshot(nextSnapshot);
      setSelectedRequirementId(null);
      if (
        name === "execute" &&
        nextSnapshot.requirement?.codex.status === "failed"
      ) {
        setError(nextSnapshot.requirement.codex.error ?? "执行启动失败");
      }
    } catch (actionError) {
      setError(
        actionError instanceof Error
          ? actionError.message
          : String(actionError),
      );
    } finally {
      setBusyAction(null);
    }
  }

  const currentRequirement = snapshot.requirement;
  const requirement = selectedRequirementId
    ? (snapshot.history.find(
        (candidate) => candidate.id === selectedRequirementId,
      ) ?? currentRequirement)
    : currentRequirement;
  const viewingHistory = Boolean(
    requirement &&
    currentRequirement &&
    requirement.id !== currentRequirement.id,
  );
  useEffect(() => {
    if (requirement)
      conversationStore
        .getState()
        .hydrate({
          requirementId: requirement.id,
          messages: requirement.conversation.messages,
          specVersions: requirement.specVersions,
        });
  }, [requirement]);
  const previewUrl = useMemo(() => {
    if (snapshot.process.status !== "ready") return null;
    // iframe 访问工作台自己的网关；外部打开才使用目标项目的原始地址。
    return `${snapshot.process.gatewayUrl}${requirement?.targetPage ?? "/"}`;
  }, [requirement?.targetPage, snapshot.process]);

  const unavailable = async () => {
    throw new Error("当前阶段没有可用的对话动作");
  };
  const conversationActions: Record<
    ConversationPhase,
    {
      reply: (reply: string) => Promise<void>;
      confirm: () => Promise<void>;
      retry: () => Promise<void>;
      cancel: () => Promise<void>;
    }
  > | null = requirement
    ? {
        grilling: {
          reply: (reply) =>
            runAction(
              "grilling-reply",
              (idempotencyKey) =>
                replyToGrill({
                  data: { idempotencyKey, reply },
                }) as Promise<WorkbenchSnapshot>,
            ),
          confirm: () =>
            runAction(
              "grilling-confirm",
              (idempotencyKey) =>
                confirmGrill({
                  data: { idempotencyKey },
                }) as Promise<WorkbenchSnapshot>,
            ),
          retry: () =>
            runAction(
              "grilling-retry",
              (idempotencyKey) =>
                retryGrill({
                  data: { idempotencyKey },
                }) as Promise<WorkbenchSnapshot>,
            ),
          cancel: () =>
            runAction(
              "grilling-cancel",
              (idempotencyKey) =>
                stopGrill({
                  data: { idempotencyKey },
                }) as Promise<WorkbenchSnapshot>,
            ),
        },
        "to-spec": {
          reply: (reply) =>
            runAction(
              "spec-reply",
              (idempotencyKey) =>
                replyToSpec({
                  data: { idempotencyKey, reply },
                }) as Promise<WorkbenchSnapshot>,
            ),
          confirm: () =>
            runAction(
              "spec-confirm",
              (idempotencyKey) =>
                confirmCurrentSpec({
                  data: {
                    idempotencyKey,
                    versionId: requirement.activeSpecVersionId ?? "",
                  },
                }) as Promise<WorkbenchSnapshot>,
            ),
          retry: () =>
            runAction(
              "spec-retry",
              (idempotencyKey) =>
                retryCurrentSpec({
                  data: { idempotencyKey },
                }) as Promise<WorkbenchSnapshot>,
            ),
          cancel: () =>
            runAction(
              "spec-cancel",
              (idempotencyKey) =>
                stopSpec({
                  data: { idempotencyKey },
                }) as Promise<WorkbenchSnapshot>,
            ),
        },
        "to-tickets": {
          reply: (reply) =>
            runAction(
              "understanding-reply",
              (idempotencyKey) =>
                reviseUnderstandingFromTickets({
                  data: { idempotencyKey, reply },
                }) as Promise<WorkbenchSnapshot>,
            ),
          confirm: unavailable,
          retry: unavailable,
          cancel: unavailable,
        },
        implement: {
          reply: unavailable,
          confirm: unavailable,
          retry: unavailable,
          cancel: unavailable,
        },
      }
    : null;

  return (
    <main className="workbench-shell">
      <header className="topbar" aria-label="工作台上下文">
        <div className="product-brand">
          <span className="product-mark" aria-hidden="true">
            P
          </span>
          <div>
            <h1>Product Agent Workbench</h1>
            <p>产品意图 → 可验收代码</p>
          </div>
        </div>
        <div className="workbench-context" aria-label="当前工作上下文">
          <span className="context-label">
            {viewingHistory ? "历史需求" : "当前需求"}
          </span>
          <strong>{requirement?.title ?? "尚未建立需求"}</strong>
          {requirement ? <code>{stageLabels[requirement.stage]}</code> : null}
        </div>
        <div className="target-chip">
          <span className="status-dot" data-status={snapshot.process.status} />
          <span>{snapshot.target?.name ?? "目标项目未配置"}</span>
        </div>
      </header>
      {error ? (
        <div className="error-banner" role="alert">
          {error}
        </div>
      ) : null}

      <div className="workbench-grid">
        {/* 左：产品状态机；中：Agent、验证和证据；右：真实目标项目运行态。 */}
        <section
          className="panel workflow-panel"
          aria-label="产品需求与 SDD 状态"
        >
          <PanelHeader index="01" title="产品需求" hint="Intent & SDD" />
          {requirement ? (
            <>
              <RequirementSummary
                requirement={requirement}
                busyAction={busyAction}
                onConfirm={() =>
                  runAction(
                    "confirm",
                    (idempotencyKey) =>
                      confirm({
                        data: { idempotencyKey },
                      }) as Promise<WorkbenchSnapshot>,
                  )
                }
                onConfirmBoundary={() =>
                  runAction(
                    "boundary",
                    (idempotencyKey) =>
                      confirmRisk({
                        data: { idempotencyKey },
                      }) as Promise<WorkbenchSnapshot>,
                  )
                }
              />
              {!viewingHistory && requirement.stage === "accepted" ? (
                <section className="next-requirement">
                  <div className="section-label">Next Requirement</div>
                  <RequirementForm
                    busy={busyAction === "create"}
                    ready={hydrated}
                    onSubmit={(nextRequirement) =>
                      runAction(
                        "create",
                        (idempotencyKey) =>
                          create({
                            data: {
                              idempotencyKey,
                              requirement: nextRequirement,
                            },
                          }) as Promise<WorkbenchSnapshot>,
                      )
                    }
                  />
                </section>
              ) : null}
            </>
          ) : (
            <RequirementForm
              busy={busyAction === "create"}
              ready={hydrated}
              onSubmit={(requirement) =>
                runAction(
                  "create",
                  (idempotencyKey) =>
                    create({
                      data: { idempotencyKey, requirement },
                    }) as Promise<WorkbenchSnapshot>,
                )
              }
            />
          )}
          <RequirementHistory
            history={snapshot.history}
            selectedId={requirement?.id ?? null}
            onSelect={(id) =>
              setSelectedRequirementId(
                id === currentRequirement?.id ? null : id,
              )
            }
          />
        </section>

        <section
          className="panel evidence-panel"
          aria-label="执行 Agent 与证据"
        >
          <PanelHeader
            index="02"
            title="执行 Agent"
            hint="Context & Evidence"
          />
          {requirement ? (
            <>
              <ConversationPanel
                requirement={requirement}
                busyAction={busyAction}
                readOnly={viewingHistory}
                onReply={
                  conversationActions![requirement.conversation.phase].reply
                }
                onConfirm={
                  conversationActions![requirement.conversation.phase].confirm
                }
                onRetry={
                  conversationActions![requirement.conversation.phase].retry
                }
                onCancel={
                  conversationActions![requirement.conversation.phase].cancel
                }
              />
              {requirement.conversation.phase === "implement" ? (
                <AgentConsole
                  requirement={requirement}
                  busyAction={busyAction}
                  readOnly={viewingHistory}
                  onExecute={() =>
                    runAction(
                      "execute",
                      (idempotencyKey) =>
                        execute({
                          data: { idempotencyKey },
                        }) as Promise<WorkbenchSnapshot>,
                    )
                  }
                  onCancel={() =>
                    runAction(
                      "cancel",
                      (idempotencyKey) =>
                        cancel({
                          data: { idempotencyKey },
                        }) as Promise<WorkbenchSnapshot>,
                    )
                  }
                  onHandoff={(focus) =>
                    runAction(
                      "handoff",
                      (idempotencyKey) =>
                        handoff({
                          data: { idempotencyKey, handoff: { focus } },
                        }) as Promise<WorkbenchSnapshot>,
                    )
                  }
                />
              ) : null}
            </>
          ) : (
            <div className="empty-copy">
              建立产品需求后，这里会进入 grilling 需求追问。
            </div>
          )}
          {viewingHistory && requirement ? (
            <HistoricalRequirementRecord requirement={requirement} />
          ) : null}
          {requirement ? (
            <ValidationPanel
              requirement={requirement}
              busyAction={busyAction}
              onRerun={() =>
                runAction(
                  "verify",
                  (idempotencyKey) =>
                    rerun({
                      data: { idempotencyKey },
                    }) as Promise<WorkbenchSnapshot>,
                )
              }
              onAccept={() =>
                runAction(
                  "accept",
                  (idempotencyKey) =>
                    accept({
                      data: { idempotencyKey },
                    }) as Promise<WorkbenchSnapshot>,
                )
              }
              onRefine={(requestedOutcome) =>
                runAction(
                  "refine",
                  (idempotencyKey) =>
                    refine({
                      data: {
                        idempotencyKey,
                        refinement: { requestedOutcome },
                      },
                    }) as Promise<WorkbenchSnapshot>,
                )
              }
            />
          ) : null}
          <EvidenceTimeline requirement={requirement} />
        </section>

        <section className="panel preview-panel" aria-label="目标页面预览">
          <PanelHeader index="03" title="真实预览" hint="Runtime" />
          <RuntimeToolbar
            snapshot={snapshot}
            busyAction={busyAction}
            previewUrl={previewUrl}
            targetPage={requirement?.targetPage}
            onStart={() =>
              runAction(
                "start",
                (idempotencyKey) =>
                  start({
                    data: { idempotencyKey },
                  }) as Promise<WorkbenchSnapshot>,
              )
            }
            onStop={() =>
              runAction(
                "stop",
                (idempotencyKey) =>
                  stop({
                    data: { idempotencyKey },
                  }) as Promise<WorkbenchSnapshot>,
              )
            }
            onRefresh={() => setPreviewRevision((value) => value + 1)}
          />
          <div className="preview-stage">
            {previewUrl ? (
              <iframe
                key={previewRevision}
                className="preview-frame"
                src={previewUrl}
                title="目标页面预览"
              />
            ) : (
              <div className="preview-empty">
                <div className="preview-window" aria-hidden="true">
                  <span />
                  <span />
                  <span />
                </div>
                <strong>启动目标项目后显示真实页面</strong>
                <p>工作台只连接显式配置的地址，不从终端输出猜测端口。</p>
              </div>
            )}
          </div>
          <details className="runtime-details">
            <summary>运行详情</summary>
            <RuntimeFacts snapshot={snapshot} />
          </details>
        </section>
      </div>
    </main>
  );
}

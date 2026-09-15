import { createFileRoute } from '@tanstack/react-router'
import { useServerFn } from '@tanstack/react-start'
import { useEffect, useMemo, useState } from 'react'

import {
  AgentConsole,
  EvidenceTimeline,
  GrillingPanel,
  PanelHeader,
  RequirementForm,
  RequirementSummary,
  RuntimeFacts,
  RuntimeToolbar,
  ValidationPanel,
} from '../components/workbench-panels'
import type { WorkbenchSnapshot } from '../domain/workbench'
import {
  analyzeRequirement,
  acceptRequirement,
  cancelExecution,
  confirmBoundary,
  confirmGrilling,
  confirmPlan,
  continueRefinement,
  getWorkbench,
  generateAgentHandoff,
  rerunVerification,
  executeRequirement,
  startTarget,
  stopTarget,
  submitRequirement,
} from '../server/functions'

// loader 负责 SSR 首屏；页面挂载后再通过轮询和 SSE 追赶后台执行产生的新状态。
export const Route = createFileRoute('/')({
  loader: () => getWorkbench(),
  component: WorkbenchPage,
})

function WorkbenchPage() {
  // 该组件是前端组合根：绑定服务端动作并把同一份快照分发给三个面板。
  const initialSnapshot = Route.useLoaderData()
  const [snapshot, setSnapshot] = useState<WorkbenchSnapshot>(initialSnapshot)
  const [hydrated, setHydrated] = useState(false)
  const [busyAction, setBusyAction] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [previewRevision, setPreviewRevision] = useState(0)
  const refresh = useServerFn(getWorkbench)
  const create = useServerFn(submitRequirement)
  const analyze = useServerFn(analyzeRequirement)
  const confirmGrill = useServerFn(confirmGrilling)
  const confirm = useServerFn(confirmPlan)
  const confirmRisk = useServerFn(confirmBoundary)
  const start = useServerFn(startTarget)
  const stop = useServerFn(stopTarget)
  const execute = useServerFn(executeRequirement)
  const cancel = useServerFn(cancelExecution)
  const rerun = useServerFn(rerunVerification)
  const accept = useServerFn(acceptRequirement)
  const refine = useServerFn(continueRefinement)
  const handoff = useServerFn(generateAgentHandoff)

  useEffect(() => setHydrated(true), [])
  // 轮询是最终收敛通道：即使 SSE 短暂断线，页面最多三秒后仍能恢复一致。
  useEffect(() => {
    const timer = window.setInterval(() => {
      void refresh().then(setSnapshot).catch(() => undefined)
    }, 3000)
    return () => window.clearInterval(timer)
  }, [refresh])
  // SSE 只发送“状态已变化”的通知，收到后仍从服务端拉取完整权威快照。
  useEffect(() => {
    const events = new EventSource('/api/events')
    events.onmessage = () => void refresh().then(setSnapshot).catch(() => undefined)
    return () => events.close()
  }, [refresh])

  async function runAction(name: string, action: (idempotencyKey: string) => Promise<WorkbenchSnapshot>) {
    // 所有按钮共用一条动作管线：互斥忙碌态、幂等键、错误展示和快照替换。
    setBusyAction(name)
    setError(null)
    try {
      const nextSnapshot = await action(crypto.randomUUID())
      setSnapshot(nextSnapshot)
      if (name === 'execute' && nextSnapshot.requirement?.codex.status === 'failed') {
        setError(nextSnapshot.requirement.codex.error ?? '执行启动失败')
      }
    } catch (actionError) {
      setError(actionError instanceof Error ? actionError.message : String(actionError))
    } finally {
      setBusyAction(null)
    }
  }

  const requirement = snapshot.requirement
  const previewUrl = useMemo(() => {
    if (snapshot.process.status !== 'ready') return null
    // iframe 访问工作台自己的网关；外部打开才使用目标项目的原始地址。
    return `${snapshot.process.gatewayUrl}${requirement?.targetPage ?? '/'}`
  }, [requirement?.targetPage, snapshot.process])

  return (
    <main className="workbench-shell">
      <header className="topbar">
        <div><p className="eyebrow">Product Agent Workbench</p><h1>让产品意图、执行和证据在同一个页面里发生</h1></div>
        <div className="target-chip"><span className="status-dot" data-status={snapshot.process.status} /><span>{snapshot.target?.name ?? '目标项目未配置'}</span></div>
      </header>
      {error ? <div className="error-banner" role="alert">{error}</div> : null}

      <div className="workbench-grid">
        {/* 左：产品状态机；中：Agent、验证和证据；右：真实目标项目运行态。 */}
        <section className="panel workflow-panel" aria-label="产品需求与 SDD 状态">
          <PanelHeader index="01" title="产品需求" hint="Intent & SDD" />
          {requirement ? (
            <><RequirementSummary
              requirement={requirement}
              busyAction={busyAction}
              onAnalyze={() => runAction('analyze', (idempotencyKey) => analyze({ data: { idempotencyKey } }) as Promise<WorkbenchSnapshot>)}
              onConfirm={() => runAction('confirm', (idempotencyKey) => confirm({ data: { idempotencyKey } }) as Promise<WorkbenchSnapshot>)}
              onConfirmBoundary={() => runAction('boundary', (idempotencyKey) => confirmRisk({ data: { idempotencyKey } }) as Promise<WorkbenchSnapshot>)}
            />
            {requirement.stage === 'accepted' ? <section className="next-requirement"><div className="section-label">Next Requirement</div><RequirementForm
              busy={busyAction === 'create'}
              ready={hydrated}
              onSubmit={(nextRequirement) => runAction('create', (idempotencyKey) => create({ data: { idempotencyKey, requirement: nextRequirement } }) as Promise<WorkbenchSnapshot>)}
            /></section> : null}</>
          ) : (
            <RequirementForm
              busy={busyAction === 'create'}
              ready={hydrated}
              onSubmit={(requirement) => runAction('create', (idempotencyKey) => create({ data: { idempotencyKey, requirement } }) as Promise<WorkbenchSnapshot>)}
            />
          )}
        </section>

        <section className="panel evidence-panel" aria-label="执行 Agent 与证据">
          <PanelHeader index="02" title="执行 Agent" hint="Context & Evidence" />
          {requirement?.grilling.status === 'pending' ? (
            <GrillingPanel requirement={requirement} busy={busyAction === 'grilling'} onConfirm={(answers) => runAction('grilling', (idempotencyKey) => confirmGrill({ data: { idempotencyKey, answers } }) as Promise<WorkbenchSnapshot>)} />
          ) : requirement ? (
            <AgentConsole
              requirement={requirement}
              busyAction={busyAction}
              onExecute={() => runAction('execute', (idempotencyKey) => execute({ data: { idempotencyKey } }) as Promise<WorkbenchSnapshot>)}
              onCancel={() => runAction('cancel', (idempotencyKey) => cancel({ data: { idempotencyKey } }) as Promise<WorkbenchSnapshot>)}
              onHandoff={(focus) => runAction('handoff', (idempotencyKey) => handoff({ data: { idempotencyKey, handoff: { focus } } }) as Promise<WorkbenchSnapshot>)}
            />
          ) : <div className="empty-copy">建立产品需求后，这里会进入 grilling 需求追问。</div>}
          {requirement ? <ValidationPanel
            requirement={requirement}
            busyAction={busyAction}
            onRerun={() => runAction('verify', (idempotencyKey) => rerun({ data: { idempotencyKey } }) as Promise<WorkbenchSnapshot>)}
            onAccept={() => runAction('accept', (idempotencyKey) => accept({ data: { idempotencyKey } }) as Promise<WorkbenchSnapshot>)}
            onRefine={(requestedOutcome) => runAction('refine', (idempotencyKey) => refine({ data: { idempotencyKey, refinement: { requestedOutcome } } }) as Promise<WorkbenchSnapshot>)}
          /> : null}
          <EvidenceTimeline requirement={requirement} />
        </section>

        <section className="panel preview-panel" aria-label="目标页面预览">
          <PanelHeader index="03" title="真实预览" hint="Runtime" />
          <RuntimeToolbar
            snapshot={snapshot}
            busyAction={busyAction}
            previewUrl={previewUrl}
            onStart={() => runAction('start', (idempotencyKey) => start({ data: { idempotencyKey } }) as Promise<WorkbenchSnapshot>)}
            onStop={() => runAction('stop', (idempotencyKey) => stop({ data: { idempotencyKey } }) as Promise<WorkbenchSnapshot>)}
            onRefresh={() => setPreviewRevision((value) => value + 1)}
          />
          {previewUrl ? <iframe key={previewRevision} className="preview-frame" src={previewUrl} title="目标页面预览" /> : (
            <div className="preview-empty"><div className="preview-window" aria-hidden="true"><span /><span /><span /></div><strong>启动目标项目后显示真实页面</strong><p>工作台只连接显式配置的地址，不从终端输出猜测端口。</p></div>
          )}
          <RuntimeFacts snapshot={snapshot} />
        </section>
      </div>
    </main>
  )
}

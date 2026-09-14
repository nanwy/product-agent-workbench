import { createFileRoute } from '@tanstack/react-router'
import { useServerFn } from '@tanstack/react-start'
import { useEffect, useMemo, useState } from 'react'

import {
  EvidenceTimeline,
  PanelHeader,
  RequirementForm,
  RequirementSummary,
  RuntimeFacts,
  RuntimeToolbar,
} from '../components/workbench-panels'
import type { WorkbenchSnapshot } from '../domain/workbench'
import {
  analyzeRequirement,
  confirmBoundary,
  confirmPlan,
  getWorkbench,
  startTarget,
  stopTarget,
  submitRequirement,
} from '../server/functions'

export const Route = createFileRoute('/')({
  loader: () => getWorkbench(),
  component: WorkbenchPage,
})

function WorkbenchPage() {
  const initialSnapshot = Route.useLoaderData()
  const [snapshot, setSnapshot] = useState<WorkbenchSnapshot>(initialSnapshot)
  const [hydrated, setHydrated] = useState(false)
  const [busyAction, setBusyAction] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [previewRevision, setPreviewRevision] = useState(0)
  const refresh = useServerFn(getWorkbench)
  const create = useServerFn(submitRequirement)
  const analyze = useServerFn(analyzeRequirement)
  const confirm = useServerFn(confirmPlan)
  const confirmRisk = useServerFn(confirmBoundary)
  const start = useServerFn(startTarget)
  const stop = useServerFn(stopTarget)

  useEffect(() => setHydrated(true), [])
  useEffect(() => {
    const timer = window.setInterval(() => {
      void refresh().then(setSnapshot).catch(() => undefined)
    }, 3000)
    return () => window.clearInterval(timer)
  }, [refresh])

  async function runAction(name: string, action: () => Promise<WorkbenchSnapshot>) {
    setBusyAction(name)
    setError(null)
    try {
      setSnapshot(await action())
    } catch (actionError) {
      setError(actionError instanceof Error ? actionError.message : String(actionError))
    } finally {
      setBusyAction(null)
    }
  }

  const requirement = snapshot.requirement
  const previewUrl = useMemo(() => {
    if (snapshot.process.status !== 'ready') return null
    return `${snapshot.process.gatewayUrl}${requirement?.targetPage ?? '/'}`
  }, [requirement?.targetPage, snapshot.process])

  return (
    <main className="workbench-shell">
      <header className="topbar">
        <div><p className="eyebrow">Product Agent Workbench</p><h1>让产品意图、执行和证据在同一个页面里发生</h1></div>
        <div className="target-chip"><span className="status-dot" data-status={snapshot.process.status} /><span>{snapshot.target?.name ?? '目标项目未配置'}</span></div>
      </header>
      {error ? <div className="error-banner">{error}</div> : null}

      <div className="workbench-grid">
        <section className="panel workflow-panel" aria-label="产品需求与 SDD 状态">
          <PanelHeader index="01" title="产品需求" hint="Intent & SDD" />
          {requirement ? (
            <RequirementSummary
              requirement={requirement}
              busyAction={busyAction}
              onAnalyze={() => runAction('analyze', () => analyze() as Promise<WorkbenchSnapshot>)}
              onConfirm={() => runAction('confirm', () => confirm() as Promise<WorkbenchSnapshot>)}
              onConfirmBoundary={() => runAction('boundary', () => confirmRisk() as Promise<WorkbenchSnapshot>)}
            />
          ) : (
            <RequirementForm
              busy={busyAction === 'create'}
              ready={hydrated}
              onSubmit={(data) => runAction('create', () => create({ data }) as Promise<WorkbenchSnapshot>)}
            />
          )}
        </section>

        <section className="panel evidence-panel" aria-label="执行 Agent 与证据">
          <PanelHeader index="02" title="执行 Agent" hint="Context & Evidence" />
          <div className="agent-placeholder"><span className="agent-mark">A</span><div><strong>Codex 会话将在规格确认后接入</strong><p>前三张票先把输入、规则、预览与可核查证据准备好。</p></div></div>
          <EvidenceTimeline requirement={requirement} />
        </section>

        <section className="panel preview-panel" aria-label="目标页面预览">
          <PanelHeader index="03" title="真实预览" hint="Runtime" />
          <RuntimeToolbar
            snapshot={snapshot}
            busyAction={busyAction}
            previewUrl={previewUrl}
            onStart={() => runAction('start', () => start() as Promise<WorkbenchSnapshot>)}
            onStop={() => runAction('stop', () => stop() as Promise<WorkbenchSnapshot>)}
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

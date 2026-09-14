import { createFileRoute } from '@tanstack/react-router'
import { useServerFn } from '@tanstack/react-start'
import { useEffect, useMemo, useState, type FormEvent } from 'react'

import {
  requirementStages,
  type Requirement,
  type RequirementStage,
  type WorkbenchSnapshot,
} from '../domain/workbench'
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

const stageLabels: Record<RequirementStage, string> = {
  'project-adaptation': '项目适配',
  'intent-drafting': '需求澄清',
  'awaiting-spec-confirmation': '确认规格',
  'awaiting-boundary-confirmation': '边界确认',
  implementation: '候选实现',
  verification: '验证',
  'awaiting-product-acceptance': '产品验收',
  accepted: '已验收',
  failed: '失败',
  cancelled: '已取消',
}

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

  useEffect(() => {
    setHydrated(true)
  }, [])

  useEffect(() => {
    const timer = window.setInterval(() => {
      void refresh().then(setSnapshot).catch(() => undefined)
    }, 3000)
    return () => window.clearInterval(timer)
  }, [refresh])

  async function runAction(
    name: string,
    action: () => Promise<WorkbenchSnapshot>,
  ) {
    setBusyAction(name)
    setError(null)
    try {
      setSnapshot(await action())
    } catch (actionError) {
      setError(
        actionError instanceof Error ? actionError.message : String(actionError),
      )
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
        <div>
          <p className="eyebrow">Product Agent Workbench</p>
          <h1>让产品意图、执行和证据在同一个页面里发生</h1>
        </div>
        <div className="target-chip">
          <span className="status-dot" data-status={snapshot.process.status} />
          <span>{snapshot.target?.name ?? '目标项目未配置'}</span>
        </div>
      </header>

      {error ? <div className="error-banner">{error}</div> : null}

      <div className="workbench-grid">
        <section className="panel workflow-panel" aria-label="产品需求与 SDD 状态">
          <PanelHeader index="01" title="产品需求" hint="Intent & SDD" />
          {requirement ? (
            <RequirementSummary
              requirement={requirement}
              busyAction={busyAction}
              onAnalyze={() =>
                runAction('analyze', () => analyze() as Promise<WorkbenchSnapshot>)
              }
              onConfirm={() =>
                runAction('confirm', () => confirm() as Promise<WorkbenchSnapshot>)
              }
              onConfirmBoundary={() =>
                runAction(
                  'boundary',
                  () => confirmRisk() as Promise<WorkbenchSnapshot>,
                )
              }
            />
          ) : (
            <RequirementForm
              busy={busyAction === 'create'}
              ready={hydrated}
              onSubmit={(data) =>
                runAction('create', () =>
                  create({ data }) as Promise<WorkbenchSnapshot>,
                )
              }
            />
          )}
        </section>

        <section className="panel evidence-panel" aria-label="执行 Agent 与证据">
          <PanelHeader index="02" title="执行 Agent" hint="Context & Evidence" />
          <div className="agent-placeholder">
            <span className="agent-mark">A</span>
            <div>
              <strong>Codex 会话将在规格确认后接入</strong>
              <p>前三张票先把输入、规则、预览与可核查证据准备好。</p>
            </div>
          </div>
          <EvidenceTimeline requirement={requirement} />
        </section>

        <section className="panel preview-panel" aria-label="目标页面预览">
          <PanelHeader index="03" title="真实预览" hint="Runtime" />
          <RuntimeToolbar
            snapshot={snapshot}
            busyAction={busyAction}
            previewUrl={previewUrl}
            onStart={() =>
              runAction('start', () => start() as Promise<WorkbenchSnapshot>)
            }
            onStop={() =>
              runAction('stop', () => stop() as Promise<WorkbenchSnapshot>)
            }
            onRefresh={() => setPreviewRevision((value) => value + 1)}
          />
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
          <RuntimeFacts snapshot={snapshot} />
        </section>
      </div>
    </main>
  )
}

function PanelHeader({ index, title, hint }: { index: string; title: string; hint: string }) {
  return (
    <header className="panel-header">
      <span>{index}</span>
      <div>
        <h2>{title}</h2>
        <p>{hint}</p>
      </div>
    </header>
  )
}

function RequirementForm({
  busy,
  ready,
  onSubmit,
}: {
  busy: boolean
  ready: boolean
  onSubmit: (data: {
    title: string
    targetPage: string
    requestedOutcome: string
  }) => Promise<void>
}) {
  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    await onSubmit({
      title: String(form.get('title') ?? ''),
      targetPage: String(form.get('targetPage') ?? ''),
      requestedOutcome: String(form.get('requestedOutcome') ?? ''),
    })
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
      <button className="primary-button" disabled={busy || !ready} type="submit">
        {!ready ? '正在连接本地服务…' : busy ? '正在记录…' : '建立产品需求'}
      </button>
      <p className="form-note">输入只作为数据保存，不会拼接为 Shell 命令。</p>
    </form>
  )
}

function RequirementSummary({
  requirement,
  busyAction,
  onAnalyze,
  onConfirm,
  onConfirmBoundary,
}: {
  requirement: Requirement
  busyAction: string | null
  onAnalyze: () => void
  onConfirm: () => void
  onConfirmBoundary: () => void
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
        {requirementStages.slice(0, 7).map((stage, index) => {
          const currentIndex = requirementStages.indexOf(requirement.stage)
          const stageIndex = requirementStages.indexOf(stage)
          const state = stage === requirement.stage ? 'current' : stageIndex < currentIndex ? 'done' : 'waiting'
          return (
            <li key={stage} data-state={state}>
              <span>{String(index + 1).padStart(2, '0')}</span>
              {stageLabels[stage]}
            </li>
          )
        })}
      </ol>

      {requirement.stage === 'intent-drafting' ? (
        <button className="primary-button" disabled={busyAction !== null} onClick={onAnalyze} type="button">
          {busyAction === 'analyze' ? '只读探索中…' : '开始只读探索'}
        </button>
      ) : null}

      {requirement.spec ? <PlanReview requirement={requirement} /> : null}

      {requirement.stage === 'awaiting-spec-confirmation' ? (
        <button className="primary-button" disabled={busyAction !== null} onClick={onConfirm} type="button">
          确认 Spec 与执行票
        </button>
      ) : null}

      {requirement.stage === 'awaiting-boundary-confirmation' ? (
        <button className="risk-button" disabled={busyAction !== null} onClick={onConfirmBoundary} type="button">
          确认工程边界
        </button>
      ) : null}

      {requirement.stage === 'implementation' ? (
        <div className="ready-callout">规格边界已确认，等待执行 Agent。</div>
      ) : null}

      {requirement.rules.length ? <RuleManifest requirement={requirement} /> : null}
    </div>
  )
}

function PlanReview({ requirement }: { requirement: Requirement }) {
  return (
    <div className="plan-review">
      <div className="section-label">候选 Spec</div>
      <h4>{requirement.spec?.title}</h4>
      <p>{requirement.spec?.desiredBehavior}</p>
      <dl>
        <div><dt>拟修改范围</dt><dd>{requirement.spec?.proposedScope}</dd></div>
        <div><dt>执行票</dt><dd>{requirement.tickets.length} 张，串行执行</dd></div>
      </dl>
      <ul className="criteria-list">
        {requirement.spec?.acceptanceCriteria.map((criterion) => <li key={criterion}>{criterion}</li>)}
      </ul>
      {requirement.boundaryItems.length ? (
        <div className="boundary-list">
          {requirement.boundaryItems.map((item) => <p key={item.id}>⚠ {item.reason}</p>)}
        </div>
      ) : null}
    </div>
  )
}

function RuleManifest({ requirement }: { requirement: Requirement }) {
  const workflowSkills = requirement.skills.filter((skill) => skill.kind === 'workflow')
  const targetSkills = requirement.skills.filter((skill) => skill.kind === 'target-project')
  return (
    <div className="rule-manifest">
      <div className="section-label">生效规则</div>
      {requirement.rules.map((rule) => (
        <details key={rule.source} className="rule-card">
          <summary>
            <span className={rule.active ? 'active-badge' : 'available-badge'}>{rule.active ? '生效' : '可用'}</span>
            <strong>{rule.source}</strong>
          </summary>
          <p>{rule.summary}</p>
          <small>{rule.activationReason}</small>
          <pre>{rule.content}</pre>
        </details>
      ))}
      <SkillGroup title="工作流 Skills" skills={workflowSkills} />
      <SkillGroup title="目标项目 Skills" skills={targetSkills} />
    </div>
  )
}

function SkillGroup({ title, skills }: { title: string; skills: Requirement['skills'] }) {
  return (
    <div className="skill-group">
      <h5>{title}</h5>
      {skills.map((skill) => (
        <div className="skill-row" key={`${skill.kind}-${skill.name}`}>
          <span className={skill.active ? 'active-badge' : 'available-badge'}>{skill.active ? '生效' : '可用'}</span>
          <div><strong>{skill.name}</strong><p>{skill.activationReason}</p></div>
        </div>
      ))}
    </div>
  )
}

function EvidenceTimeline({ requirement }: { requirement: Requirement | null }) {
  if (!requirement) return <div className="empty-copy">建立产品需求后，这里会出现可核查的阶段证据。</div>
  return (
    <div className="evidence-list">
      {requirement.evidence.slice().reverse().map((record, index) => (
        <article key={`${record.recordedAt}-${index}`}>
          <header><span>{stageLabels[record.stage]}</span><time>{new Date(record.recordedAt).toLocaleTimeString('zh-CN')}</time></header>
          <dl>
            <div><dt>输入</dt><dd>{record.input}</dd></div>
            <div><dt>动作</dt><dd>{record.action}</dd></div>
            <div><dt>输出</dt><dd>{record.output}</dd></div>
          </dl>
        </article>
      ))}
    </div>
  )
}

function RuntimeToolbar({ snapshot, busyAction, previewUrl, onStart, onStop, onRefresh }: {
  snapshot: WorkbenchSnapshot
  busyAction: string | null
  previewUrl: string | null
  onStart: () => void
  onStop: () => void
  onRefresh: () => void
}) {
  const running = ['starting', 'ready'].includes(snapshot.process.status)
  return (
    <div className="runtime-toolbar">
      <button disabled={busyAction !== null || running || !snapshot.target} onClick={onStart} type="button">
        {busyAction === 'start' ? '启动中…' : '启动项目'}
      </button>
      <button disabled={busyAction !== null || !running} onClick={onStop} type="button">停止</button>
      <button disabled={!previewUrl} onClick={onRefresh} type="button">刷新</button>
      {previewUrl ? <a href={`${snapshot.process.previewUrl}${snapshot.requirement?.targetPage ?? '/'}`} rel="noreferrer" target="_blank">外部打开 ↗</a> : null}
    </div>
  )
}

function RuntimeFacts({ snapshot }: { snapshot: WorkbenchSnapshot }) {
  return (
    <dl className="runtime-facts">
      <div><dt>状态</dt><dd data-testid="process-status">{snapshot.process.status}</dd></div>
      <div><dt>PID</dt><dd>{snapshot.process.pid ?? '—'}</dd></div>
      <div><dt>命令</dt><dd>{snapshot.process.command || '—'}</dd></div>
      <div><dt>健康地址</dt><dd>{snapshot.process.healthUrl || '—'}</dd></div>
    </dl>
  )
}

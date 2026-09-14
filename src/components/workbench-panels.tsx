import type { FormEvent } from 'react'

import {
  requirementStages,
  type Requirement,
  type RequirementStage,
  type WorkbenchSnapshot,
} from '../domain/workbench'

export const stageLabels: Record<RequirementStage, string> = {
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

export function PanelHeader({ index, title, hint }: { index: string; title: string; hint: string }) {
  return <header className="panel-header"><span>{index}</span><div><h2>{title}</h2><p>{hint}</p></div></header>
}

export function RequirementForm({ busy, ready, onSubmit }: {
  busy: boolean
  ready: boolean
  onSubmit: (data: { title: string; targetPage: string; requestedOutcome: string }) => Promise<void>
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
      <label><span>需求标题</span><input name="title" placeholder="例如：优化赛事空状态文案" required /></label>
      <label><span>目标页面</span><input name="targetPage" defaultValue="/" required /></label>
      <label><span>期望结果</span><textarea name="requestedOutcome" placeholder="描述用户最终应该看到或做到什么，而不是指定代码怎么改。" rows={5} required /></label>
      <button className="primary-button" disabled={busy || !ready} type="submit">
        {!ready ? '正在连接本地服务…' : busy ? '正在记录…' : '建立产品需求'}
      </button>
      <p className="form-note">输入只作为数据保存，不会拼接为 Shell 命令。</p>
    </form>
  )
}

export function RequirementSummary({ requirement, busyAction, onAnalyze, onConfirm, onConfirmBoundary }: {
  requirement: Requirement
  busyAction: string | null
  onAnalyze: () => void
  onConfirm: () => void
  onConfirmBoundary: () => void
}) {
  return (
    <div className="requirement-summary">
      <div className="requirement-heading"><span>{requirement.id}</span><h3>{requirement.title}</h3><p>{requirement.requestedOutcome}</p><code>{requirement.targetPage}</code></div>
      <ol className="stage-rail">
        {requirementStages.slice(0, 7).map((stage, index) => {
          const currentIndex = requirementStages.indexOf(requirement.stage)
          const stageIndex = requirementStages.indexOf(stage)
          const state = stage === requirement.stage ? 'current' : stageIndex < currentIndex ? 'done' : 'waiting'
          return <li key={stage} data-state={state}><span>{String(index + 1).padStart(2, '0')}</span>{stageLabels[stage]}</li>
        })}
      </ol>
      {requirement.stage === 'intent-drafting' && requirement.grilling.status === 'confirmed' ? <button className="primary-button" disabled={busyAction !== null} onClick={onAnalyze} type="button">{busyAction === 'analyze' ? '只读探索中…' : '开始只读探索'}</button> : null}
      {requirement.spec ? <PlanReview requirement={requirement} /> : null}
      {requirement.stage === 'awaiting-spec-confirmation' ? <button className="primary-button" disabled={busyAction !== null} onClick={onConfirm} type="button">确认 Spec 与执行票</button> : null}
      {requirement.stage === 'awaiting-boundary-confirmation' ? <button className="risk-button" disabled={busyAction !== null} onClick={onConfirmBoundary} type="button">确认工程边界</button> : null}
      {requirement.stage === 'implementation' ? <div className="ready-callout">规格边界已确认，可以创建安全需求分支并执行。</div> : null}
      {requirement.rules.length ? <RuleManifest requirement={requirement} /> : null}
    </div>
  )
}

export function GrillingPanel({ requirement, busy, onConfirm }: {
  requirement: Requirement
  busy: boolean
  onConfirm: (answers: Record<string, string>) => Promise<void>
}) {
  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    await onConfirm(Object.fromEntries(requirement.grilling.questions.map((question) => [question.id, String(data.get(question.id) ?? '')])))
  }
  return (
    <form className="grilling-panel" onSubmit={handleSubmit}>
      <div className="section-label">Grilling · 共同理解门禁</div>
      <p>回答当前设计树的全部前沿问题，确认后才会生成 Spec。</p>
      {requirement.grilling.questions.map((question, index) => (
        <label key={question.id}>
          <strong>Q{index + 1} · {question.title}</strong>
          <span>{question.prompt}</span>
          <textarea name={question.id} defaultValue={question.answer ?? question.recommendation} rows={3} required />
        </label>
      ))}
      <button className="primary-button" disabled={busy} type="submit">{busy ? '正在确认…' : '共同理解已达成'}</button>
    </form>
  )
}

export function AgentConsole({ requirement, busyAction, onExecute, onCancel }: {
  requirement: Requirement
  busyAction: string | null
  onExecute: () => void
  onCancel: () => void
}) {
  const canExecute = requirement.stage === 'implementation' && requirement.codex.status !== 'running' && requirement.codex.status !== 'completed'
  return (
    <div className="agent-console">
      <div className="agent-placeholder"><span className="agent-mark">A</span><div><strong>连续 Codex Session</strong><p data-testid="codex-status">{requirement.codex.status} · {requirement.codex.sessionId ?? '尚未建立 session'}</p></div></div>
      <div className="ticket-stack">{requirement.tickets.map((ticket) => <div className="ticket-row" data-status={ticket.status} key={ticket.id}><code>{ticket.id}</code><span>{ticket.title}</span><strong>{ticket.status}</strong></div>)}</div>
      {requirement.branch ? (
        <div className={requirement.branch.blockedReason ? 'git-facts blocked' : 'git-facts'}>
          <div><span>Base</span><code>{requirement.branch.baseBranch} · {requirement.branch.baseRevision.slice(0, 8)}</code></div>
          <div><span>Branch</span><code>{requirement.branch.requirementBranch ?? '未创建'}</code></div>
          {requirement.branch.blockedReason ? <p>{requirement.branch.blockedReason}</p> : null}
          {requirement.branch.startingWorkingTree.slice(0, 8).map((entry) => <small key={`${entry.status}-${entry.path}`}>{entry.status} {entry.path}</small>)}
        </div>
      ) : null}
      <div className="agent-actions">
        {canExecute ? <button className="primary-button" disabled={busyAction !== null} onClick={onExecute} type="button">{requirement.codex.status === 'cancelled' ? '继续当前 Session' : '创建分支并执行'}</button> : null}
        {requirement.codex.status === 'running' ? <button className="risk-button" disabled={busyAction !== null} onClick={onCancel} type="button">停止当前 Turn</button> : null}
      </div>
      {requirement.codex.error ? <div className="agent-error">{requirement.codex.error}</div> : null}
      <div className="agent-events" aria-live="polite">
        {requirement.codex.events.slice().reverse().map((agentEvent) => <article key={agentEvent.id}><span>{agentEvent.type}</span><p>{agentEvent.message}</p><time>{new Date(agentEvent.recordedAt).toLocaleTimeString('zh-CN')}</time></article>)}
      </div>
      {requirement.codex.changedFiles.length ? <div className="changed-files" data-testid="changed-files"><strong>Changed files</strong>{requirement.codex.changedFiles.map((file) => <code key={file}>{file}</code>)}</div> : null}
    </div>
  )
}

function PlanReview({ requirement }: { requirement: Requirement }) {
  const exploration = requirement.spec?.exploration
  return (
    <div className="plan-review">
      <div className="section-label">候选 Spec</div><h4>{requirement.spec?.title}</h4><p>{requirement.spec?.desiredBehavior}</p>
      <dl>
        <div><dt>拟修改范围</dt><dd>{requirement.spec?.proposedScope}</dd></div>
        <div><dt>执行票</dt><dd>{requirement.tickets.length} 张，串行执行</dd></div>
        <div><dt>已检查代码</dt><dd>{exploration?.inspectedFileCount ?? 0} 个文件</dd></div>
      </dl>
      {exploration?.candidateCodeLocations.length ? <ul className="criteria-list">{exploration.candidateCodeLocations.map((source) => <li key={source}><code>{source}</code></li>)}</ul> : null}
      <ul className="criteria-list">{requirement.spec?.acceptanceCriteria.map((criterion) => <li key={criterion}>{criterion}</li>)}</ul>
      {requirement.boundaryItems.length ? <div className="boundary-list">{requirement.boundaryItems.map((item) => <p key={item.id}>⚠ {item.reason}</p>)}</div> : null}
    </div>
  )
}

function RuleManifest({ requirement }: { requirement: Requirement }) {
  const workflowSkills = requirement.skills.filter((skill) => skill.kind === 'workflow')
  const targetSkills = requirement.skills.filter((skill) => skill.kind === 'target-project')
  return (
    <div className="rule-manifest">
      <div className="section-label">生效规则</div>
      {requirement.rules.map((rule) => <details key={rule.source} className="rule-card"><summary><span className="active-badge">生效</span><strong>{rule.source}</strong></summary><p>{rule.summary}</p><small>{rule.activationReason}</small><pre>{rule.content}</pre></details>)}
      <SkillGroup title="工作流 Skills" skills={workflowSkills} />
      <SkillGroup title="目标项目 Skills" skills={targetSkills} />
    </div>
  )
}

function SkillGroup({ title, skills }: { title: string; skills: Requirement['skills'] }) {
  return <div className="skill-group"><h5>{title}</h5>{skills.map((skill) => <div className="skill-row" key={`${skill.kind}-${skill.name}`}><span className={skill.active ? 'active-badge' : 'available-badge'}>{skill.active ? '生效' : '可用'}</span><div><strong>{skill.name}</strong><p>{skill.activationReason}</p></div></div>)}</div>
}

export function EvidenceTimeline({ requirement }: { requirement: Requirement | null }) {
  if (!requirement) return <div className="empty-copy">建立产品需求后，这里会出现可核查的阶段证据。</div>
  return <div className="evidence-list">{requirement.evidence.slice().reverse().map((record, index) => <article key={`${record.recordedAt}-${index}`}><header><span>{stageLabels[record.stage]}</span><time>{new Date(record.recordedAt).toLocaleTimeString('zh-CN')}</time></header><dl><div><dt>输入</dt><dd>{record.input}</dd></div><div><dt>动作</dt><dd>{record.action}</dd></div><div><dt>输出</dt><dd>{record.output}</dd></div></dl></article>)}</div>
}

export function RuntimeToolbar({ snapshot, busyAction, previewUrl, onStart, onStop, onRefresh }: {
  snapshot: WorkbenchSnapshot
  busyAction: string | null
  previewUrl: string | null
  onStart: () => void
  onStop: () => void
  onRefresh: () => void
}) {
  const running = ['starting', 'ready'].includes(snapshot.process.status)
  return <div className="runtime-toolbar"><button disabled={busyAction !== null || running || !snapshot.target} onClick={onStart} type="button">{busyAction === 'start' ? '启动中…' : '启动项目'}</button><button disabled={busyAction !== null || !running} onClick={onStop} type="button">停止</button><button disabled={!previewUrl} onClick={onRefresh} type="button">刷新</button>{previewUrl ? <a href={`${snapshot.process.previewUrl}${snapshot.requirement?.targetPage ?? '/'}`} rel="noreferrer" target="_blank">外部打开 ↗</a> : null}</div>
}

export function RuntimeFacts({ snapshot }: { snapshot: WorkbenchSnapshot }) {
  return <dl className="runtime-facts"><div><dt>状态</dt><dd data-testid="process-status">{snapshot.process.status}</dd></div><div><dt>PID</dt><dd>{snapshot.process.pid ?? '—'}</dd></div><div><dt>命令</dt><dd>{snapshot.process.command || '—'}</dd></div><div><dt>健康地址</dt><dd>{snapshot.process.healthUrl || '—'}</dd></div></dl>
}

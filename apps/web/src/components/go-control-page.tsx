import type { FormEvent } from 'react'
import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'

import { WorkbenchApiError } from '../api/client'
import { useGoTaskEventStream } from '../api/events'
import {
  useCancelTaskRunMutation,
  useConfirmGrillingMutation,
  useConfirmSpecMutation,
  useConfirmTicketsMutation,
  useCreateRequirementMutation,
  useGoHealthQuery,
  useGoPlanningQuery,
  useGoRequirementQuery,
  useGoTaskRunQuery,
  useGoTargetRuntimeQuery,
  useGoVerificationQuery,
  useGenerateSpecMutation,
  useGenerateTicketsMutation,
  useReconcileTaskRunsMutation,
  useStartTargetRuntimeMutation,
  useStopTargetRuntimeMutation,
  useVerifyRequirementMutation,
} from '../api/queries'
import type {
  SpecDocumentResponse,
  TicketResponse,
  TaskRunResponse,
  VerificationReportResponse,
} from '../api/contracts'

/**
 * 步骤 21B 页面：在 19B 控制面的基础上加入 Grilling 与版本化 Spec。
 *
 * 前端视角：
 * - React Query 保存 Requirement、TaskRun、Verification 这些服务端状态；
 * - useState 只保存输入框和“当前看哪个 ID”这类瞬时 UI 状态；
 * - mutation 的响应由 queries.ts 写回缓存，组件不复制一份 response state。
 */
export function GoControlPage({ standalone = false }: { standalone?: boolean }) {
  // 第一步：这些值来自用户正在填写的表单，不是服务端事实，因此适合放 useState。
  const [projectId, setProjectId] = useState('product-agent-workbench')
  const [title, setTitle] = useState('')
  const [requirementId, setRequirementId] = useState('')
  const [ticketVersionId, setTicketVersionId] = useState('')
  const [taskRunId, setTaskRunId] = useState('')
  const [summary, setSummary] = useState('')
  const [desiredOutcome, setDesiredOutcome] = useState('')
  const [changeBoundary, setChangeBoundary] = useState('')
  const [acceptanceSignal, setAcceptanceSignal] = useState('')
  const [specInstruction, setSpecInstruction] = useState('')
  const [ticketInstruction, setTicketInstruction] = useState('')
  const [previewPath, setPreviewPath] = useState('/')
  const [previewRevision, setPreviewRevision] = useState(0)

  // 第二步：所有服务端事实从 Query Cache 读取。空 ID 时 hook 会保持 disabled。
  const queryClient = useQueryClient()
  const eventStream = useGoTaskEventStream(queryClient)
  const health = useGoHealthQuery()
  const requirement = useGoRequirementQuery(requirementId.trim())
  const planning = useGoPlanningQuery(requirementId.trim())
  const taskRun = useGoTaskRunQuery(taskRunId.trim(), eventStream.status === 'connected')
  const verification = useGoVerificationQuery(
    requirementId.trim(),
    ticketVersionId.trim(),
  )
  const targetRuntime = useGoTargetRuntimeQuery()

  // 第三步：按钮调用 mutation。成功回调只改变“当前选择”，数据本身由 mutation 写缓存。
  const createRequirement = useCreateRequirementMutation()
  const reconcileTaskRuns = useReconcileTaskRunsMutation()
  const cancelTaskRun = useCancelTaskRunMutation()
  const verifyRequirement = useVerifyRequirementMutation()
  const confirmGrilling = useConfirmGrillingMutation()
  const generateSpec = useGenerateSpecMutation()
  const confirmSpec = useConfirmSpecMutation()
  const generateTickets = useGenerateTicketsMutation()
  const confirmTickets = useConfirmTicketsMutation()
  const startTargetRuntime = useStartTargetRuntimeMutation()
  const stopTargetRuntime = useStopTargetRuntimeMutation()

  function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    createRequirement.mutate(
      { projectId: projectId.trim(), title: title.trim() },
      {
        onSuccess: (created) => {
          setRequirementId(created.id)
          setTitle('')
        },
      },
    )
  }

  function handleReconcile(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    reconcileTaskRuns.mutate(
      { requirementId: requirementId.trim(), ticketVersionId: ticketVersionId.trim() },
      {
        onSuccess: (result) => {
          const firstRun = result.enqueued[0]
          if (firstRun) setTaskRunId(firstRun.id)
        },
      },
    )
  }

  function handleConfirmGrilling(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!requirement.data) return
    confirmGrilling.mutate({
      requirementId: requirement.data.id,
      request: {
        expectedRevision: requirement.data.revision,
        summary,
        desiredOutcome,
        changeBoundary,
        acceptanceSignal,
      },
    })
  }

  function handleGenerateSpec(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!requirement.data) return
    generateSpec.mutate({
      requirementId: requirement.data.id,
      request: {
        expectedRevision: requirement.data.revision,
        instruction: specInstruction,
      },
    })
  }

  function handleGenerateTickets(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!requirement.data) return
    generateTickets.mutate({
      requirementId: requirement.data.id,
      request: {
        expectedRevision: requirement.data.revision,
        instruction: ticketInstruction,
      },
    })
  }

  function handleConfirmTickets(versionId: string) {
    if (!requirement.data) return
    confirmTickets.mutate({
      requirementId: requirement.data.id,
      versionId,
      expectedRevision: requirement.data.revision,
    }, {
      onSuccess: () => setTicketVersionId(versionId),
    })
  }

  const activeSpec = planning.data?.specVersions.find((version) => version.status === 'draft')
  const latestSpec = planning.data?.specVersions[planning.data.specVersions.length - 1]
  const visibleSpec = activeSpec ?? latestSpec
  const activeTicketVersion = planning.data?.ticketVersions.find((version) => version.status === 'draft')
  const latestTicketVersion = planning.data?.ticketVersions[planning.data.ticketVersions.length - 1]
  const visibleTicketVersion = activeTicketVersion ?? latestTicketVersion

  // 页面刷新后从服务端 confirmed 事实恢复队列输入，不要求用户复制一串内部 ID。
  useEffect(() => {
    const confirmed = planning.data?.ticketVersions.find((version) => version.status === 'confirmed')
    if (confirmed) setTicketVersionId(confirmed.id)
  }, [planning.data?.ticketVersions])

  const actionError =
    createRequirement.error ??
    reconcileTaskRuns.error ??
    cancelTaskRun.error ??
    verifyRequirement.error ??
    confirmGrilling.error ??
    generateSpec.error ??
    confirmSpec.error ??
    generateTickets.error ??
    confirmTickets.error
    ?? startTargetRuntime.error
    ?? stopTargetRuntime.error

  const normalizedPreviewPath = previewPath.trim().startsWith('/')
    ? previewPath.trim()
    : `/${previewPath.trim()}`
  const previewSource = targetRuntime.data?.process.status === 'ready'
    ? `${targetRuntime.data.process.gatewayUrl}${normalizedPreviewPath}`
    : null
  const previewIdentity = requirementId.trim() && ticketVersionId.trim()
    ? { requirementId: requirementId.trim(), ticketVersionId: ticketVersionId.trim() }
    : { requirementId: '', ticketVersionId: '' }

  return (
    <main className="go-control-shell">
      <header className="go-control-header">
        <div>
          <span>{standalone ? 'STEP 21E · GO RUNTIME' : 'STEP 21E · GO DEFAULT'}</span>
          <h1>Go 控制面</h1>
          <p>
            {standalone
              ? 'SPA、HTTP API、SSE 与 Worker 由同一个 Go 进程提供。'
              : '默认工作台已切到 Go API；旧 Node 工作台仅保留为迁移对照。'}
          </p>
        </div>
        <div className="go-control-header-actions">
          <span className="go-health" data-ready={health.data?.status === 'ok'}>
            Go {health.data?.status === 'ok' ? '已连接' : '未连接'}
          </span>
          <span className="go-stream" data-status={eventStream.status}>
            SSE {streamStatusLabel(eventStream.status)}
          </span>
          {standalone ? (
            <span className="go-standalone-badge">Go 单进程模式</span>
          ) : (
            <a className="secondary-button" href="/legacy">旧 Node 对照</a>
          )}
        </div>
      </header>

      {health.error ? <ErrorBanner error={health.error} prefix="连接 Go 服务失败" /> : null}
      {eventStream.error ? <ErrorBanner error={eventStream.error} prefix="解析 SSE 失败" /> : null}
      {actionError ? <ErrorBanner error={actionError} prefix="操作失败" /> : null}

      <section className="go-control-grid" aria-label="Go API 资源控制面">
        <article className="go-control-card">
          <header><span>01</span><div><h2>Requirement</h2><p>创建或读取需求头</p></div></header>
          <form onSubmit={handleCreate}>
            <label>
              <span>Project ID</span>
              <input value={projectId} onChange={(event) => setProjectId(event.target.value)} required />
            </label>
            <label>
              <span>标题</span>
              <input value={title} onChange={(event) => setTitle(event.target.value)} required />
            </label>
            <button className="primary-button" disabled={createRequirement.isPending}>创建 Requirement</button>
          </form>

          <label className="go-resource-lookup">
            <span>当前 Requirement ID</span>
            <input
              value={requirementId}
              onChange={(event) => setRequirementId(event.target.value)}
              placeholder="req-..."
            />
          </label>
          {requirement.isFetching ? <p className="form-note">正在读取 Go Requirement…</p> : null}
          {requirement.error ? <InlineError error={requirement.error} /> : null}
          {requirement.data ? (
            <dl className="go-resource-facts">
              <Fact label="ID" value={requirement.data.id} />
              <Fact label="标题" value={requirement.data.title} />
              <Fact label="阶段" value={requirement.data.stage} />
              <Fact label="Revision" value={String(requirement.data.revision)} />
            </dl>
          ) : null}
          {requirement.data?.stage === 'grilling' ? (
            <form onSubmit={handleConfirmGrilling}>
              <p className="form-note">
                21A：把讨论收敛成四个稳定字段。确认后服务端才允许生成 Spec。
              </p>
              <label><span>共同理解摘要</span><textarea value={summary} onChange={(event) => setSummary(event.target.value)} required /></label>
              <label><span>期望结果</span><textarea value={desiredOutcome} onChange={(event) => setDesiredOutcome(event.target.value)} required /></label>
              <label><span>修改边界</span><input value={changeBoundary} onChange={(event) => setChangeBoundary(event.target.value)} placeholder="例如 apps/web" required /></label>
              <label><span>验收信号</span><textarea value={acceptanceSignal} onChange={(event) => setAcceptanceSignal(event.target.value)} required /></label>
              <button className="primary-button" disabled={confirmGrilling.isPending}>确认 Grilling</button>
            </form>
          ) : null}
        </article>

        <article className="go-control-card">
          <header><span>02</span><div><h2>Spec</h2><p>只读检查仓库，生成并冻结版本化规格</p></div></header>
          {planning.data?.understanding ? (
            <div className="go-result-block">
              <strong>已确认共同理解</strong>
              <p>{planning.data.understanding.summary}</p>
            </div>
          ) : <p className="form-note">先在 Requirement 卡片完成 21A。</p>}
          {requirement.data?.stage === 'spec' ? (
            <form onSubmit={handleGenerateSpec}>
              <label>
                <span>修订要求（第一版可留空）</span>
                <textarea value={specInstruction} onChange={(event) => setSpecInstruction(event.target.value)} />
              </label>
              <button className="primary-button" disabled={generateSpec.isPending}>
                {generateSpec.isPending ? 'Codex 正在只读分析…' : '生成 Spec 草稿'}
              </button>
            </form>
          ) : null}
          {visibleSpec ? (
            <div className="go-result-block">
              <strong>v{visibleSpec.version} · {visibleSpec.status}</strong>
              <SpecDocument document={visibleSpec.document} />
              {activeSpec ? (
                <button
                  type="button"
                  className="primary-button"
                  disabled={confirmSpec.isPending || !requirement.data}
                  onClick={() => requirement.data && confirmSpec.mutate({
                    requirementId: requirement.data.id,
                    versionId: activeSpec.id,
                    expectedRevision: requirement.data.revision,
                  })}
                >
                  确认当前 Spec
                </button>
              ) : null}
            </div>
          ) : null}
          {planning.data?.specVersions.filter((version) => version.id !== visibleSpec?.id).map((version) => (
            <p className="form-note" key={version.id}>v{version.version} · {version.status}</p>
          ))}
        </article>

        <article className="go-control-card">
          <header><span>03</span><div><h2>Tickets</h2><p>把已确认 Spec 拆成可并行 DAG</p></div></header>
          {requirement.data?.stage === 'tickets' ? (
            <form onSubmit={handleGenerateTickets}>
              <label>
                <span>修订要求（第一版可留空）</span>
                <textarea value={ticketInstruction} onChange={(event) => setTicketInstruction(event.target.value)} />
              </label>
              <button className="primary-button" disabled={generateTickets.isPending}>
                {generateTickets.isPending ? 'Codex 正在拆票…' : '生成 Tickets 草稿'}
              </button>
            </form>
          ) : null}
          {visibleTicketVersion ? (
            <div className="go-result-block">
              <strong>v{visibleTicketVersion.version} · {visibleTicketVersion.status}</strong>
              <small>ID：{visibleTicketVersion.id}</small>
              {visibleTicketVersion.tickets.map((ticket) => <TicketCard key={ticket.id} ticket={ticket} />)}
              {activeTicketVersion ? (
                <button
                  type="button"
                  className="primary-button"
                  disabled={confirmTickets.isPending || !requirement.data}
                  onClick={() => handleConfirmTickets(activeTicketVersion.id)}
                >
                  确认并发布 TicketVersion
                </button>
              ) : visibleTicketVersion.status === 'confirmed' && requirement.data?.stage === 'implementation' ? (
                <button
                  type="button"
                  className="secondary-button"
                  disabled={confirmTickets.isPending}
                  onClick={() => handleConfirmTickets(visibleTicketVersion.id)}
                >
                  重新发布执行 manifest
                </button>
              ) : null}
            </div>
          ) : <p className="form-note">确认 Spec 后才会进入 21C。</p>}
        </article>

        <article className="go-control-card">
          <header><span>04</span><div><h2>Ticket Queue</h2><p>让 Scheduler 计算 ready Ticket</p></div></header>
          <form onSubmit={handleReconcile}>
            <label>
              <span>TicketVersion ID</span>
              <input
                value={ticketVersionId}
                onChange={(event) => setTicketVersionId(event.target.value)}
                placeholder="tickets-v1"
                required
              />
            </label>
            <p className="form-note">
              这里必须填写已经确认并写入服务端 artifact 的版本 ID，不能临时自定义。
              21C 确认成功后会自动填入这里；Scheduler 只会读取已发布的不可变 artifact。
            </p>
            <button
              className="primary-button"
              disabled={!requirementId.trim() || reconcileTaskRuns.isPending}
            >
              计算并加入队列
            </button>
          </form>
          {reconcileTaskRuns.data ? (
            <div className="go-result-block">
              <strong>本次新入队 {reconcileTaskRuns.data.enqueued.length} 张票</strong>
              {reconcileTaskRuns.data.enqueued.map((run) => (
                <button key={run.id} type="button" onClick={() => setTaskRunId(run.id)}>
                  {run.ticketId} · {run.status}
                </button>
              ))}
            </div>
          ) : null}
        </article>

        <article className="go-control-card">
          <header><span>05</span><div><h2>TaskRun</h2><p>查询执行尝试与取消活动任务</p></div></header>
          <label className="go-resource-lookup">
            <span>TaskRun ID</span>
            <input
              value={taskRunId}
              onChange={(event) => setTaskRunId(event.target.value)}
              placeholder="run-..."
            />
          </label>
          {taskRun.error ? <InlineError error={taskRun.error} /> : null}
          {taskRun.data ? <TaskRunCard run={taskRun.data} /> : null}
          <button
            className="risk-button"
            disabled={!taskRunId.trim() || cancelTaskRun.isPending || !isCancellable(taskRun.data)}
            onClick={() => cancelTaskRun.mutate(taskRunId.trim())}
            type="button"
          >
            取消当前 TaskRun
          </button>
        </article>

        <article className="go-control-card">
          <header><span>06</span><div><h2>Verification</h2><p>执行最终候选门禁并读取报告</p></div></header>
          <button
            className="primary-button"
            disabled={!requirementId.trim() || !ticketVersionId.trim() || verifyRequirement.isPending}
            onClick={() => verifyRequirement.mutate({
              requirementId: requirementId.trim(),
              request: { ticketVersionId: ticketVersionId.trim() },
            })}
            type="button"
          >
            运行 Verification
          </button>
          {verification.error && !isNotFound(verification.error) ? <InlineError error={verification.error} /> : null}
          {verification.data ? <VerificationCard report={verification.data} /> : (
            <p className="form-note">当前版本还没有 Verification 报告。</p>
          )}
        </article>

        <article className="go-control-card go-runtime-card">
          <header><span>07</span><div><h2>Target Preview</h2><p>由 Go 管理进程、健康检查与 iframe 网关</p></div></header>
          {targetRuntime.error ? <InlineError error={targetRuntime.error} /> : null}
          {targetRuntime.data ? (
            <>
              <dl className="go-resource-facts">
                <Fact label="项目" value={targetRuntime.data.target.name} />
                <Fact label="状态" value={targetRuntime.data.process.status} />
                <Fact label="所有权" value={targetRuntime.data.process.ownership} />
                <Fact label="PID" value={targetRuntime.data.process.pid?.toString() ?? '—'} />
                <Fact label="健康地址" value={targetRuntime.data.process.healthUrl} />
              </dl>
              <label>
                <span>预览路径（只属于页面 UI，不会改写启动命令）</span>
                <input value={previewPath} onChange={(event) => setPreviewPath(event.target.value)} />
              </label>
              <div className="go-runtime-actions">
                <button
                  className="primary-button"
                  disabled={startTargetRuntime.isPending || ['starting', 'ready'].includes(targetRuntime.data.process.status)}
                  onClick={() => startTargetRuntime.mutate(previewIdentity)}
                  type="button"
                >
                  {startTargetRuntime.isPending ? '等待健康检查…' : '启动候选预览'}
                </button>
                <button
                  className="risk-button"
                  disabled={stopTargetRuntime.isPending || targetRuntime.data.process.status === 'stopped'}
                  onClick={() => stopTargetRuntime.mutate()}
                  type="button"
                >停止</button>
                <button disabled={!previewSource} onClick={() => setPreviewRevision((value) => value + 1)} type="button">刷新 iframe</button>
                {previewSource ? <a href={`${targetRuntime.data.process.previewUrl}${normalizedPreviewPath}`} rel="noreferrer" target="_blank">外部打开 ↗</a> : null}
              </div>
              {targetRuntime.data.process.error ? <p className="go-runtime-error">{targetRuntime.data.process.error}</p> : null}
              {previewSource ? (
                <iframe key={previewRevision} className="go-runtime-frame" src={previewSource} title="Go 目标项目预览" />
              ) : <p className="form-note">启动成功后，iframe 只访问 Go 的 localhost 预览网关。</p>}
              {targetRuntime.data.process.recentOutput.length ? (
                <details><summary>最近 30 行进程输出</summary><pre className="go-runtime-output">{targetRuntime.data.process.recentOutput.join('\n')}</pre></details>
              ) : null}
            </>
          ) : <p className="form-note">正在读取目标运行时配置…</p>}
        </article>
      </section>
    </main>
  )
}

function SpecDocument({ document }: { document: SpecDocumentResponse }) {
  return (
    <div className="go-spec-document">
      <SpecSection title="问题" values={[document.problem]} />
      <SpecSection title="方案" values={[document.solution]} />
      <SpecSection title="用户故事" values={document.userStories} />
      <SpecSection title="实现决策" values={document.implementationDecisions} />
      <SpecSection title="测试决策" values={document.testingDecisions} />
      <SpecSection title="不做什么" values={document.outOfScope} />
      <SpecSection title="补充说明" values={document.furtherNotes} />
    </div>
  )
}

function SpecSection({ title, values }: { title: string; values: string[] }) {
  return (
    <section>
      <h3>{title}</h3>
      <ul>{values.map((value, index) => <li key={`${title}-${index}`}>{value}</li>)}</ul>
    </section>
  )
}

function TicketCard({ ticket }: { ticket: TicketResponse }) {
  return (
    <article className="go-ticket-card">
      <strong>{ticket.id} · {ticket.title}</strong>
      <p>{ticket.deliverable}</p>
      <small>依赖：{ticket.blockedBy.length ? ticket.blockedBy.join(', ') : '无，可并行启动'}</small>
      <ul>{ticket.acceptanceCriteria.map((criterion, index) => (
        <li key={`${ticket.id}-${index}`}>{criterion}</li>
      ))}</ul>
    </article>
  )
}

function TaskRunCard({ run }: { run: TaskRunResponse }) {
  return (
    <dl className="go-resource-facts">
      <Fact label="Ticket" value={run.ticketId} />
      <Fact label="Attempt" value={String(run.attempt)} />
      <Fact label="状态" value={run.status} />
      <Fact label="创建时间" value={formatTime(run.createdAt)} />
      {run.result ? <Fact label="结果" value={run.result.summary} /> : null}
      {run.failureReason ? <Fact label="失败原因" value={run.failureReason} /> : null}
    </dl>
  )
}

function VerificationCard({ report }: { report: VerificationReportResponse }) {
  return (
    <div className="go-verification" data-status={report.status}>
      <strong>{report.status === 'passed' ? '验证通过' : '验证阻塞'}</strong>
      <small>{report.candidateRevision ?? '没有候选 Revision'}</small>
      {report.records.map((record) => (
        <article key={record.id} data-status={record.status}>
          <header><span>{record.label}</span><strong>{record.status}</strong></header>
          <p>{record.detail}</p>
          {record.reason ? <small>{record.reason}</small> : null}
        </article>
      ))}
    </div>
  )
}

function Fact({ label, value }: { label: string; value: string }) {
  return <div><dt>{label}</dt><dd>{value}</dd></div>
}

function ErrorBanner({ error, prefix }: { error: unknown; prefix: string }) {
  return <div className="error-banner" role="alert">{prefix}：{errorMessage(error)}</div>
}

function InlineError({ error }: { error: unknown }) {
  return <p className="go-inline-error" role="alert">{errorMessage(error)}</p>
}

function errorMessage(error: unknown) {
  if (error instanceof WorkbenchApiError) return `${error.message}（${error.code} / HTTP ${error.status}）`
  return error instanceof Error ? error.message : String(error)
}

function isNotFound(error: unknown) {
  return error instanceof WorkbenchApiError && error.status === 404
}

function isCancellable(run: TaskRunResponse | undefined) {
  return run?.status === 'queued' || run?.status === 'claimed' || run?.status === 'running'
}

function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN')
}

function streamStatusLabel(status: 'connecting' | 'connected' | 'reconnecting' | 'error') {
  switch (status) {
    case 'connecting': return '连接中'
    case 'connected': return '已连接'
    case 'reconnecting': return '重连中'
    case 'error': return '数据错误'
  }
}

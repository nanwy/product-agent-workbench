import { createFileRoute } from '@tanstack/react-router'
import type { FormEvent } from 'react'
import { useState } from 'react'

import { WorkbenchApiError } from '../api/client'
import {
  useCancelTaskRunMutation,
  useCreateRequirementMutation,
  useGoHealthQuery,
  useGoRequirementQuery,
  useGoTaskRunQuery,
  useGoVerificationQuery,
  useReconcileTaskRunsMutation,
  useVerifyRequirementMutation,
} from '../api/queries'
import type {
  TaskRunResponse,
  VerificationReportResponse,
} from '../api/contracts'

export const Route = createFileRoute('/go-control')({ component: GoControlPage })

/**
 * 练习 19B 页面：只消费 Go 已经实现的 HTTP 资源。
 *
 * 前端视角：
 * - React Query 保存 Requirement、TaskRun、Verification 这些服务端状态；
 * - useState 只保存输入框和“当前看哪个 ID”这类瞬时 UI 状态；
 * - mutation 的响应由 queries.ts 写回缓存，组件不复制一份 response state。
 */
function GoControlPage() {
  // 第一步：这些值来自用户正在填写的表单，不是服务端事实，因此适合放 useState。
  const [projectId, setProjectId] = useState('product-agent-workbench')
  const [title, setTitle] = useState('')
  const [requirementId, setRequirementId] = useState('')
  const [ticketVersionId, setTicketVersionId] = useState('')
  const [taskRunId, setTaskRunId] = useState('')

  // 第二步：所有服务端事实从 Query Cache 读取。空 ID 时 hook 会保持 disabled。
  const health = useGoHealthQuery()
  const requirement = useGoRequirementQuery(requirementId.trim())
  const taskRun = useGoTaskRunQuery(taskRunId.trim())
  const verification = useGoVerificationQuery(
    requirementId.trim(),
    ticketVersionId.trim(),
  )

  // 第三步：按钮调用 mutation。成功回调只改变“当前选择”，数据本身由 mutation 写缓存。
  const createRequirement = useCreateRequirementMutation()
  const reconcileTaskRuns = useReconcileTaskRunsMutation()
  const cancelTaskRun = useCancelTaskRunMutation()
  const verifyRequirement = useVerifyRequirementMutation()

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

  const actionError =
    createRequirement.error ??
    reconcileTaskRuns.error ??
    cancelTaskRun.error ??
    verifyRequirement.error

  return (
    <main className="go-control-shell">
      <header className="go-control-header">
        <div>
          <span>STEP 19B · GO HTTP</span>
          <h1>Go 控制面</h1>
          <p>React Query 现在直接消费 Go API；旧 Node 工作台仍保留在主页面。</p>
        </div>
        <div className="go-control-header-actions">
          <span className="go-health" data-ready={health.data?.status === 'ok'}>
            Go {health.data?.status === 'ok' ? '已连接' : '未连接'}
          </span>
          <a className="secondary-button" href="/">返回旧工作台</a>
        </div>
      </header>

      {health.error ? <ErrorBanner error={health.error} prefix="连接 Go 服务失败" /> : null}
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
        </article>

        <article className="go-control-card">
          <header><span>02</span><div><h2>Ticket Queue</h2><p>让 Scheduler 计算 ready Ticket</p></div></header>
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
          <header><span>03</span><div><h2>TaskRun</h2><p>查询执行尝试与取消活动任务</p></div></header>
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
          <header><span>04</span><div><h2>Verification</h2><p>执行最终候选门禁并读取报告</p></div></header>
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
      </section>
    </main>
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


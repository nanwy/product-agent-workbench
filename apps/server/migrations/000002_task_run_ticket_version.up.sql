-- 第 2 份 SQLite migration：让 TaskRun 明确归属于不可变 TicketVersion
--
-- 不能直接修改已经应用过的 000001。SQLite 也不能原地删除表级 UNIQUE 约束，因此这里
-- 使用“新表 → 复制 → 替换”的标准重建流程。旧记录来自 Scheduler 之前的手动入队路径，
-- 统一回填为 legacy；新记录必须由应用层提供真实 TicketVersionID。

PRAGMA foreign_keys = OFF;
BEGIN IMMEDIATE;

CREATE TABLE task_runs_v2 (
    enqueue_seq INTEGER PRIMARY KEY,
    id TEXT NOT NULL UNIQUE,
    requirement_id TEXT NOT NULL REFERENCES requirements(id) ON DELETE RESTRICT,
    ticket_version_id TEXT NOT NULL,
    ticket_id TEXT NOT NULL,
    attempt INTEGER NOT NULL,
    status TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    claimed_at INTEGER,
    started_at INTEGER,
    finished_at INTEGER,
    result_summary TEXT,
    codex_thread_id TEXT,
    worktree_path TEXT,
    failure_reason TEXT,

    CHECK (attempt >= 1),
    CHECK (status IN (
        'queued','claimed','running','completed','failed','cancelled'
    )),
    UNIQUE (requirement_id, ticket_version_id, ticket_id, attempt)
);

INSERT INTO task_runs_v2 (
    enqueue_seq,
    id,
    requirement_id,
    ticket_version_id,
    ticket_id,
    attempt,
    status,
    created_at,
    claimed_at,
    started_at,
    finished_at,
    result_summary,
    codex_thread_id,
    worktree_path,
    failure_reason
)
SELECT
    enqueue_seq,
    id,
    requirement_id,
    'legacy',
    ticket_id,
    attempt,
    status,
    created_at,
    claimed_at,
    started_at,
    finished_at,
    result_summary,
    codex_thread_id,
    worktree_path,
    failure_reason
FROM task_runs;

DROP TABLE task_runs;
ALTER TABLE task_runs_v2 RENAME TO task_runs;

CREATE INDEX idx_task_runs_queue
ON task_runs(status, enqueue_seq);

-- 不把版本放进活动唯一键：同一 Requirement 的同名 Ticket 即使来自新旧两个版本，
-- 也不能同时修改目标项目。旧版本活动任务结束后，新版本 Reconcile 才能继续。
CREATE UNIQUE INDEX ux_task_runs_one_active_ticket
ON task_runs(requirement_id, ticket_id)
WHERE status IN ('queued', 'claimed', 'running');

COMMIT;
PRAGMA foreign_keys = ON;

-- 返回任何行都表示重建破坏了 task_events → task_runs 外键；迁移执行器必须把结果视为失败。
PRAGMA foreign_key_check;

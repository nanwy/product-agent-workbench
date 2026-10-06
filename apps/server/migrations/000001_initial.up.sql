-- 第 1 份 SQLite migration：建立服务端最小持久化模型
--
-- 这是会被 migration 工具执行的正式文件。设计依据记录在同目录 README.md；
-- 只创建当前 Store 契约需要的表、约束和索引，不提前增加未来字段。

-- ---------------------------------------------------------------------------
-- 第 1 步：requirements
-- ---------------------------------------------------------------------------
-- 目标：保存 Requirement 最小聚合头，并支持按 ID 读取和 revision 乐观锁更新。
--
-- 你需要填写：
-- 1. 用 CREATE TABLE 创建 requirements。
-- 2. id、project_id、title、stage、revision 都不允许 NULL。
-- 3. id 是文本主键。
-- 4. revision 必须大于等于 1。
-- 5. stage 只能是 domain/requirement.go 中列出的六个字符串。
--
-- 语法不会时，先看 lessons/0009-sqlite-migration-first-table.html 中的 articles 完整示例，
-- 再做表名、字段名和枚举值的对应替换；不要直接猜 CHECK 的括号和引号。
--
-- 写完后自查：哪个约束防止 revision=0？哪个约束防止拼错 stage？

CREATE TABLE requirements (
    id TEXT NOT NULL PRIMARY KEY,
    project_id TEXT NOT NULL,
    title TEXT NOT NULL,
    stage TEXT NOT NULL,
    revision INTEGER NOT NULL,

    CHECK (revision >=1),
    CHECK (stage IN ('grilling','spec','tickets','implementation','acceptance','delivered'))
);

-- ---------------------------------------------------------------------------
-- 第 2 步：task_runs
-- ---------------------------------------------------------------------------
-- 目标：保存每一次执行尝试，并提供独立于 created_at 的稳定入队顺序。
-- 完整同构示例和逐项解释：lessons/0010-sqlite-task-runs-table.html
--
-- 你需要填写：
-- 1. enqueue_seq 使用 SQLite 的 INTEGER PRIMARY KEY。INSERT 不提供它时，SQLite 会自动
--    分配整数；这里只需要对现存队列稳定排序，不需要 AUTOINCREMENT 的“永不复用”保证。
-- 2. id 是业务 ID，必须唯一且非空；因为 enqueue_seq 已占用整数主键位置，思考这里应使用
--    UNIQUE 还是第二个 PRIMARY KEY（SQLite 一张表不能有两个主键）。
-- 3. requirement_id 使用外键引用 requirements.id，并限制删除。
-- 4. ticket_version_id、ticket_id、attempt、status、created_at 必填；attempt >= 1。
-- 5. claimed_at、started_at、finished_at 和结果字段允许 NULL。
-- 6. status 只能是 queued/claimed/running/completed/failed/cancelled。
-- 7. (requirement_id, ticket_version_id, ticket_id, attempt) 必须唯一。
-- 8. 创建 status + enqueue_seq 队列索引。
-- 9. 创建只约束 queued/claimed/running 的 Ticket 活动态部分唯一索引。
--
-- 写完后自查：两个 created_at 完全相同的任务靠什么稳定排序？为什么不能 INSERT OR REPLACE？

CREATE TABLE task_runs (
    enqueue_seq INTEGER PRIMARY KEY,
    id TEXT NOT NULL UNIQUE,
    requirement_id TEXT NOT NULL  REFERENCES requirements(id) ON DELETE RESTRICT,
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
    result_revision TEXT,
    failure_reason TEXT,

    CHECK (attempt >= 1),
    CHECK (status IN (
        'queued','claimed','running','completed','failed','cancelled'
    )),
    UNIQUE (requirement_id, ticket_version_id, ticket_id, attempt)
);

-- ClaimNextTask 先按 status 筛选 queued，再按 enqueue_seq 找最早入队记录。
CREATE INDEX idx_task_runs_queue
ON task_runs(status, enqueue_seq);

-- 部分唯一索引只约束活动状态：同一 Ticket 不能同时有两个正在执行的 TaskRun。
-- 同一 Requirement 的不同 Ticket 可以同时活动，为后续 DAG Scheduler 和多 Worker 保留并行能力。
-- completed/failed/cancelled 不进入该索引，因此仍可保留历史并创建新的 attempt。
CREATE UNIQUE INDEX ux_task_runs_one_active_ticket
ON task_runs(requirement_id, ticket_id)
WHERE status IN ('queued', 'claimed', 'running');
-- ---------------------------------------------------------------------------
-- 第 3 步：task_events
-- ---------------------------------------------------------------------------
-- 目标：同时提供全局 SSE 游标和单个 TaskRun 内的严格事件顺序。
-- 完整同构示例和逐项解释：lessons/0011-sqlite-task-events-table.html
--
-- 你需要填写：
-- 1. id 使用 INTEGER PRIMARY KEY AUTOINCREMENT。AUTOINCREMENT 是 SQLite 特有语法，保证
--    自动生成的 id 大于该表历史上出现过的所有 id；即使以后清理旧事件也不复用 SSE 游标。
-- 2. task_run_id 外键引用 task_runs.id，并限制删除。
-- 3. sequence、type、payload、created_at 必填；sequence >= 1。
-- 4. payload 选择能无损保存 []byte 的 SQLite 类型。
-- 5. (task_run_id, sequence) 必须唯一；这个唯一约束同时服务事件排序查询。
--
-- 写完后自查：全局 id 和 sequence 分别回答哪个问题？失败批次为什么必须在事务中追加？

CREATE TABLE task_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_run_id TEXT NOT NULL REFERENCES task_runs(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL,
    type TEXT NOT NULL,
    payload BLOB NOT NULL,
    created_at INTEGER NOT NULL,
    CHECK (sequence >=1),
    UNIQUE (task_run_id, sequence)
);


-- ---------------------------------------------------------------------------
-- 最终检查（本文件通过 SQLite 执行验证后才能进入下一步）
-- ---------------------------------------------------------------------------
-- [x] 只有 requirements、task_runs、task_events 三张表
-- [ ] 所有动态业务值以后都通过 ? 参数传入，而不是拼接 SQL
-- [ ] foreign_keys 会由每个连接启用
-- [x] 每个索引都能对应 README 中的一条真实查询或不变量
-- [x] 没有为未来的列表页、搜索或统计提前添加索引
-- [x] 没有 DROP、触发器、UPSERT/REPLACE 或隐藏状态迁移

-- 阶段 22：保存新 Go 工作流创建的逻辑对话、Agent Turn 和稳定消息。
-- 旧 Node JSON 数据不迁移；本 migration 只向前创建新表。

CREATE TABLE conversations (
    id TEXT NOT NULL PRIMARY KEY,
    requirement_id TEXT NOT NULL UNIQUE
        REFERENCES requirements(id) ON DELETE RESTRICT,
    phase TEXT NOT NULL,
    status TEXT NOT NULL,
    planning_thread_id TEXT,
    implementation_thread_id TEXT,
    pending_understanding_revision TEXT,
    turn_ready_for_confirmation INTEGER NOT NULL,
    error TEXT,
    revision INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    CHECK (length(trim(id)) > 0),
    CHECK (length(trim(requirement_id)) > 0),
    CHECK (planning_thread_id IS NULL OR length(trim(planning_thread_id)) > 0),
    CHECK (implementation_thread_id IS NULL OR length(trim(implementation_thread_id)) > 0),
    CHECK (
        pending_understanding_revision IS NULL OR
        length(trim(pending_understanding_revision)) > 0
    ),
    CHECK (phase IN ('grilling', 'to-spec', 'to-tickets', 'implement')),
    CHECK (status IN (
        'idle', 'running', 'awaiting-user', 'awaiting-confirmation', 'failed', 'cancelled'
    )),
    CHECK (turn_ready_for_confirmation IN (0, 1)),
    CHECK (revision >= 1),
    CHECK (updated_at >= created_at),
    CHECK (
        status != 'failed' OR
        (error IS NOT NULL AND length(trim(error)) > 0)
    )
);

CREATE TABLE conversation_turns (
    id TEXT NOT NULL PRIMARY KEY,
    conversation_id TEXT NOT NULL
        REFERENCES conversations(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL,
    phase TEXT NOT NULL,
    status TEXT NOT NULL,
    codex_thread_id TEXT,
    started_at INTEGER NOT NULL,
    finished_at INTEGER,
    failure_reason TEXT,
    CHECK (length(trim(id)) > 0),
    CHECK (length(trim(conversation_id)) > 0),
    CHECK (codex_thread_id IS NULL OR length(trim(codex_thread_id)) > 0),
    CHECK (sequence >= 1),
    CHECK (phase IN ('grilling', 'to-spec', 'to-tickets', 'implement')),
    CHECK (status IN ('running', 'completed', 'failed', 'cancelled')),
    CHECK (
        (status = 'running' AND finished_at IS NULL) OR
        (status != 'running' AND finished_at IS NOT NULL)
    ),
    CHECK (finished_at IS NULL OR finished_at >= started_at),
    CHECK (
        status != 'failed' OR
        (failure_reason IS NOT NULL AND length(trim(failure_reason)) > 0)
    ),
    UNIQUE (conversation_id, sequence),
    UNIQUE (conversation_id, id)
);

-- 前端一次只能展示一个正在生成的 assistant 回合；该约束也阻止重复提交开启两个 Agent。
CREATE UNIQUE INDEX ux_conversation_turns_one_running
ON conversation_turns(conversation_id)
WHERE status = 'running';

CREATE TABLE conversation_messages (
    id TEXT NOT NULL PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    turn_id TEXT,
    sequence INTEGER NOT NULL,
    role TEXT NOT NULL,
    phase TEXT NOT NULL,
    status TEXT NOT NULL,
    segments_json BLOB NOT NULL,
    recorded_at INTEGER NOT NULL,
    CHECK (length(trim(id)) > 0),
    CHECK (length(trim(conversation_id)) > 0),
    CHECK (turn_id IS NULL OR length(trim(turn_id)) > 0),
    CHECK (sequence >= 1),
    CHECK (role IN ('user', 'assistant', 'system')),
    CHECK (phase IN ('grilling', 'to-spec', 'to-tickets', 'implement')),
    CHECK (status IN ('streaming', 'completed', 'failed', 'cancelled')),
    CHECK (status != 'streaming' OR turn_id IS NOT NULL),
    UNIQUE (conversation_id, sequence),
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE RESTRICT,
    FOREIGN KEY (conversation_id, turn_id)
        REFERENCES conversation_turns(conversation_id, id) ON DELETE RESTRICT
);

-- 练习 18B：保存每一次验证尝试，而不是只保留“最后通过/失败”布尔值。
--
-- 前端类比：verification_reports 类似 CI runs 列表。matrix_json 是该次 run 的 jobs 快照；
-- 页面查询最新一条，但旧报告仍能解释候选版本为什么曾经被门禁阻止。

CREATE TABLE verification_reports (
    report_seq INTEGER PRIMARY KEY,
    id TEXT NOT NULL UNIQUE,
    requirement_id TEXT NOT NULL REFERENCES requirements(id) ON DELETE RESTRICT,
    ticket_version_id TEXT NOT NULL,
    candidate_revision TEXT NOT NULL,
    workspace_path TEXT NOT NULL,
    status TEXT NOT NULL,
    matrix_json BLOB NOT NULL,
    started_at INTEGER NOT NULL,
    completed_at INTEGER NOT NULL,

    CHECK (status IN ('passed', 'blocked')),
    CHECK (completed_at >= started_at)
);

-- LatestReport 按 Requirement + TicketVersion 查询最近一次验证。
CREATE INDEX idx_verification_reports_latest
ON verification_reports(requirement_id, ticket_version_id, completed_at DESC, report_seq DESC);

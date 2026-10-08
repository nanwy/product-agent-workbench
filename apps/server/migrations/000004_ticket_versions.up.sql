-- 步骤 21C：保存可修订 Ticket DAG。只有 confirmed 版本会发布成执行 manifest。

CREATE TABLE ticket_versions (
    id TEXT NOT NULL PRIMARY KEY,
    requirement_id TEXT NOT NULL
        REFERENCES requirements(id) ON DELETE RESTRICT,
    spec_version_id TEXT NOT NULL
        REFERENCES spec_versions(id) ON DELETE RESTRICT,
    version INTEGER NOT NULL,
    status TEXT NOT NULL,
    document_json BLOB NOT NULL,
    created_at INTEGER NOT NULL,
    confirmed_at INTEGER,
    CHECK (version >= 1),
    CHECK (status IN ('draft', 'confirmed', 'superseded')),
    CHECK (
        (status = 'confirmed' AND confirmed_at IS NOT NULL) OR
        (status != 'confirmed' AND confirmed_at IS NULL)
    ),
    UNIQUE (requirement_id, version)
);

-- 同一 Requirement 最多只有一个可继续处理的 TicketVersion。
CREATE UNIQUE INDEX ux_ticket_versions_one_active
ON ticket_versions(requirement_id)
WHERE status IN ('draft', 'confirmed');

CREATE INDEX idx_ticket_versions_requirement_version
ON ticket_versions(requirement_id, version);

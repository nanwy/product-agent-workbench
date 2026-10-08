-- 步骤 21A/21B：持久化已确认共同理解和不可覆盖的 Spec 版本。
-- 聊天增量仍属于后续增强；当前只保存会被下游阶段依赖的稳定业务事实。

CREATE TABLE grilling_understandings (
    requirement_id TEXT NOT NULL PRIMARY KEY
        REFERENCES requirements(id) ON DELETE RESTRICT,
    summary TEXT NOT NULL,
    desired_outcome TEXT NOT NULL,
    change_boundary TEXT NOT NULL,
    acceptance_signal TEXT NOT NULL,
    confirmed_at INTEGER NOT NULL,
    CHECK (length(trim(summary)) > 0),
    CHECK (length(trim(desired_outcome)) > 0),
    CHECK (length(trim(change_boundary)) > 0),
    CHECK (length(trim(acceptance_signal)) > 0)
);

CREATE TABLE spec_versions (
    id TEXT NOT NULL PRIMARY KEY,
    requirement_id TEXT NOT NULL
        REFERENCES requirements(id) ON DELETE RESTRICT,
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

-- 一个 Requirement 同时最多只有一个仍有效的 Spec；历史版本继续保留用于复盘。
CREATE UNIQUE INDEX ux_spec_versions_one_active
ON spec_versions(requirement_id)
WHERE status IN ('draft', 'confirmed');

CREATE INDEX idx_spec_versions_requirement_version
ON spec_versions(requirement_id, version);

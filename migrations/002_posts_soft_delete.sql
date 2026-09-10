-- 002：posts 软删除改造（2026-09-07，T1.3 需求确认选了软删除）
-- 删除不再物理删行，只写 deleted_at 时间戳；行保留，可恢复、可审计。

ALTER TABLE posts
    ADD COLUMN deleted_at DATETIME(3) NULL DEFAULT NULL;

CREATE INDEX idx_deleted_at ON posts (deleted_at);

-- 003：comments 游标分页索引（2026-09-08，T1.4）
-- 评论列表也是游标分页（created_at DESC, id DESC），原 idx_post (post_id) 升级为
-- idx_post_time (post_id, created_at, id)——最左前缀原则下它同时覆盖原来只按 post_id 的查询。
-- 数据先行：表还空着加索引零成本，等百万行再加就是线上事故。

ALTER TABLE comments
    DROP INDEX idx_post,
    ADD INDEX idx_post_time (post_id, created_at, id);

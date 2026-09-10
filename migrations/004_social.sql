-- 004：社交关系（2026-09-09，M2 关注体系）
-- follows：(follower_id, followee_id) 联合主键 = 关注幂等的数据库兜底（同 likes 套路）。
-- idx_follower_time：我关注的人列表（按关注时间倒序）
-- idx_followee_time：关注我的人列表（按关注时间倒序）

CREATE TABLE IF NOT EXISTS follows (
    follower_id BIGINT UNSIGNED NOT NULL,
    followee_id BIGINT UNSIGNED NOT NULL,
    created_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (follower_id, followee_id),
    KEY idx_follower_time (follower_id, created_at),
    KEY idx_followee_time (followee_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 冗余计数（D-M2-1）：个人主页高频读取，事务内同步（与 M1 点赞计数同模式）
ALTER TABLE users
    ADD COLUMN following_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN follower_count  BIGINT NOT NULL DEFAULT 0;

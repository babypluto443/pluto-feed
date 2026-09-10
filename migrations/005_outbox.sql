-- 005：异步化基础设施（2026-09-09，M4）
-- outbox：业务事务内同写的"待发事件"表（T4.4 Outbox 模式的本地消息表）
--   relay 轮询 processed_at IS NULL 的行 → 投递 MQ → 标记 processed
-- processed_events：消费端去重表（T4.3/T4.5）
--   event_id 主键 = outbox.id，INSERT IGNORE 的 RowsAffected 判断 = 消费幂等防线

CREATE TABLE IF NOT EXISTS outbox (
    id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    event_type   VARCHAR(50)  NOT NULL,
    payload      JSON         NOT NULL,
    created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    processed_at DATETIME(3)  NULL DEFAULT NULL,
    KEY idx_processed (processed_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS processed_events (
    event_id     BIGINT UNSIGNED PRIMARY KEY,
    processed_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

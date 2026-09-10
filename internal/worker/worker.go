// Package worker Worker 进程的三角色（T4.1/T4.3/T4.5）：
//
//	relay    轮询 outbox → confirm 模式投递 MQ → 标记 processed（T4.4 Outbox 模式）
//	consumer 消费计数事件 → [去重表 + 计数更新] 同事务 → DEL 缓存 → ACK
//	dlq      死信兜底：日志记录（生产环境 = 报警 + 人工处理）
//
// 崩溃窗口分析（面试必考）：
//   - relay 已 publish 未 mark → 进程崩溃 → 重启后重投同一条消息
//     → 消费端 processed_events 去重 → 不会重复计数
//     → 至少一次投递 + 幂等消费 = 恰好一次效果
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/gorm"

	"pluto_feed/internal/cache"
	"pluto_feed/internal/model"
	"pluto_feed/internal/mq"
	"pluto_feed/internal/repository"
)

// Counter 计数事件处理器：去重 + 落库，两步在同一个事务里。
type Counter struct {
	db    *gorm.DB
	cache cache.Store
}

func NewCounter(db *gorm.DB, c cache.Store) *Counter {
	return &Counter{db: db, cache: c}
}

// allowedFields 计数字段白名单——表名/列名绝不能从消息里来（防注入）。
var allowedFields = map[string]bool{"like_count": true, "comment_count": true}

// Apply 消费一条计数事件：
//  1. INSERT IGNORE processed_events（event_id 主键）——重复消息在此被拦截
//  2. RowsAffected==1 才真正更新计数（首次消费）
//  3. 同事务提交——"标记已消费"与"计数更新"要么都成要么都不成
//  4. 成功后 DEL 帖子缓存（计数变了，M3 的缓存必须失效）
func (c *Counter) Apply(ctx context.Context, eventID uint64, e model.CountEvent) error {
	if !allowedFields[e.Field] {
		return fmt.Errorf("invalid count field: %s", e.Field)
	}
	err := c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec("INSERT IGNORE INTO processed_events (event_id) VALUES (?)", eventID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // 重复消息：已处理过，幂等跳过
		}
		return tx.Exec(fmt.Sprintf(
			"UPDATE posts SET %s = %s + ? WHERE id = ?", e.Field, e.Field), e.Delta, e.PostID).Error
	})
	if err != nil {
		return err
	}
	// 计数变了 → 帖子详情缓存失效（M3 Cache Aside 的写时失效，从 Worker 侧触发）
	if c.cache != nil {
		if err := c.cache.Del(ctx, cache.PostKey(e.PostID)); err != nil {
			log.Printf("[cache] del %s: %v（TTL 兜底自愈）", cache.PostKey(e.PostID), err)
		}
	}
	return nil
}

// RunRelay 轮询投递循环：outbox → MQ → 标记。
// 每 interval 轮询一次；单批最多 batchSize 条；publish 失败的批次不标记，下轮重投。
func RunRelay(ctx context.Context, db *gorm.DB, publisher *mq.Publisher, outbox repository.OutboxRepo, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := relayOnce(ctx, db, publisher, outbox); err != nil {
				log.Printf("[relay] round failed: %v", err)
			}
		}
	}
}

func relayOnce(ctx context.Context, db *gorm.DB, publisher *mq.Publisher, outbox repository.OutboxRepo) error {
	events, err := outbox.FetchPending(ctx, 100)
	if err != nil {
		return err
	}
	var done []uint64
	for _, evt := range events {
		if err := publisher.Publish(ctx, evt.ID, evt.EventType, evt.Payload); err != nil {
			// 投递失败：中止本轮，未标记的行下轮重投（至少一次语义）
			log.Printf("[relay] publish event %d (%s) failed: %v", evt.ID, evt.EventType, err)
			break
		}
		done = append(done, evt.ID)
	}
	if len(done) > 0 {
		if err := outbox.MarkProcessed(ctx, done...); err != nil {
			// 已投递但标记失败：重启后会重投，消费端去重表兜底
			log.Printf("[relay] mark processed failed: %v（消费端幂等兜底）", err)
		}
		log.Printf("[relay] published %d events", len(done))
	}
	return nil
}

// RunConsumer 消费循环：手动 ACK，失败 NACK（不重入队）→ 死信队列。
func RunConsumer(ctx context.Context, counter *Counter, ch *amqp.Channel) error {
	if err := ch.Qos(10, 0, false); err != nil { // 一次最多预取 10 条，削峰消费
		return fmt.Errorf("qos: %w", err)
	}
	deliveries, err := ch.Consume(mq.Queue, "pluto-worker", false /*手动 ACK*/, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume: %w", err)
	}
	for d := range deliveries {
		if err := handleDelivery(ctx, counter, d); err != nil {
			log.Printf("[consumer] event %s failed: %v → DLX", d.MessageId, err)
			// requeue=false：失败消息不重入队（避免毒消息无限循环），走死信
			_ = d.Nack(false, false)
			continue
		}
		_ = d.Ack(false)
	}
	return nil
}

func handleDelivery(ctx context.Context, counter *Counter, d amqp.Delivery) error {
	eventID, err := strconv.ParseUint(d.MessageId, 10, 64)
	if err != nil {
		return fmt.Errorf("bad message id %q: %w", d.MessageId, err)
	}
	var e model.CountEvent
	if err := json.Unmarshal(d.Body, &e); err != nil {
		return fmt.Errorf("bad payload: %w", err)
	}
	return counter.Apply(ctx, eventID, e)
}

// RunDLQ 死信兜底：只记日志。生产环境应同时报警 + 落死信表 + 人工/定时修复。
func RunDLQ(ctx context.Context, ch *amqp.Channel) error {
	deliveries, err := ch.Consume(mq.DLQ, "pluto-dlq", true /*自动 ACK——死信只记录不阻塞*/, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume dlq: %w", err)
	}
	for d := range deliveries {
		log.Printf("[DLQ] dead letter: id=%s rk=%s body=%s", d.MessageId, d.RoutingKey, d.Body)
	}
	return nil
}

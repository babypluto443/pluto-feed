package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"pluto_feed/internal/model"
)

// OutboxRepo 本地消息表存取（T4.4）。
type OutboxRepo interface {
	// EnqueueInTx 在业务事务内写入事件行（同事务 = 业务成功则事件必存在）。
	// gorm Create 会回填自增 ID —— 该 ID 就是消费端幂等的 event_id。
	EnqueueInTx(tx *gorm.DB, evt *model.Outbox) error
	// FetchPending relay 轮询：未处理的最早 N 条。
	FetchPending(ctx context.Context, limit int) ([]model.Outbox, error)
	// MarkProcessed 投递成功后批量标记。
	MarkProcessed(ctx context.Context, ids ...uint64) error
}

type outboxRepo struct{ db *gorm.DB }

func NewOutboxRepo(db *gorm.DB) OutboxRepo { return &outboxRepo{db: db} }

func (r *outboxRepo) EnqueueInTx(tx *gorm.DB, evt *model.Outbox) error {
	return tx.Create(evt).Error
}

func (r *outboxRepo) FetchPending(ctx context.Context, limit int) ([]model.Outbox, error) {
	var events []model.Outbox
	err := r.db.WithContext(ctx).
		Where("processed_at IS NULL").
		Order("id ASC").Limit(limit).Find(&events).Error
	return events, err
}

func (r *outboxRepo) MarkProcessed(ctx context.Context, ids ...uint64) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.Outbox{}).
		Where("id IN ?", ids).
		Update("processed_at", &now).Error
}

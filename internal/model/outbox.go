package model

import (
	"time"
)

// Outbox 本地消息表（T4.4）：业务事务内同写的"待发事件"。
// relay 轮询 processed_at IS NULL 的行投递 MQ，成功后标记 processed。
type Outbox struct {
	ID          uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	EventType   string     `gorm:"type:varchar(50);not null" json:"event_type"`
	Payload     []byte     `gorm:"type:json;not null" json:"payload"`
	CreatedAt   time.Time  `json:"created_at"`
	ProcessedAt *time.Time `json:"processed_at"`
}

func (Outbox) TableName() string { return "outbox" }

// 事件类型常量：repo 写 outbox 的 event_type = MQ 的 routing key，两处必须同源。
const (
	EventLikeCreated   = "like.created"
	EventLikeRemoved   = "like.removed"
	EventCommentCreate = "comment.created"
	EventCommentDelete = "comment.deleted"
)

// CountEvent 计数事件载荷（outbox.payload 的内容）。
// consumer 收到后：UPDATE posts SET <field> = <field> + delta WHERE id = post_id。
type CountEvent struct {
	PostID int64  `json:"post_id"`
	Field  string `json:"field"` // like_count | comment_count（白名单校验，防注入）
	Delta  int    `json:"delta"`
}

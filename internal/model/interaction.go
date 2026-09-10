package model

import "time"

// Like 点赞表。(post_id, user_id) 唯一键是幂等的数据库层保证。
type Like struct {
	PostID    int64     `gorm:"primaryKey;autoIncrement:false" json:"post_id"`
	UserID    int64     `gorm:"primaryKey;autoIncrement:false" json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (Like) TableName() string { return "likes" }

// Comment 评论表（M1 仅一级评论）。
// idx_post_time (post_id, created_at, id)：评论列表游标分页（migration 003）。
type Comment struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	PostID    int64     `gorm:"not null;index:idx_post_time,priority:1" json:"post_id"`
	UserID    int64     `gorm:"not null" json:"user_id"`
	Content   string    `gorm:"type:varchar(500);not null" json:"content"`
	CreatedAt time.Time `gorm:"index:idx_post_time,priority:2" json:"created_at"`
}

func (Comment) TableName() string { return "comments" }

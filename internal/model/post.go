package model

import (
	"time"

	"gorm.io/gorm"
)

// Post 帖子表。M1 最核心的两条索引：
//   - (created_at, id)：推荐流游标分页
//   - (user_id, created_at, id)：M2 关注流游标分页
// 软删除：删除只写 deleted_at，行保留（可恢复/审计），所有查询自动过滤。
type Post struct {
	ID           int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID       int64          `gorm:"not null;index:idx_user_time,priority:1" json:"user_id"`
	ImageURLs    StringList     `gorm:"type:json" json:"image_urls"`
	Content      string         `gorm:"type:varchar(1000);not null" json:"content"`
	Tags         StringList     `gorm:"type:json" json:"tags"`
	LikeCount    int64          `gorm:"not null;default:0" json:"like_count"`
	CommentCount int64          `gorm:"not null;default:0" json:"comment_count"`
	CreatedAt    time.Time      `gorm:"index:idx_time,priority:1;index:idx_user_time,priority:2" json:"created_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"` // 非空即"已删除"，GORM 查询自动加 deleted_at IS NULL
}

func (Post) TableName() string { return "posts" }

// StringList 存进 MySQL JSON 列的字符串列表。
// 实现 driver.Valuer / sql.Scanner，让 GORM 透明读写。
type StringList []string

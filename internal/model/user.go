package model

import "time"

// User 用户表。
// following_count / follower_count 为冗余计数（D-M2-1）：关注/取关事务内同步，
// 个人主页高频读取免 COUNT 聚合（M1 点赞计数的同款模式）。
type User struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Nickname       string    `gorm:"type:varchar(32);uniqueIndex;not null" json:"nickname"`
	PasswordHash   string    `gorm:"type:varchar(100);not null" json:"-"` // bcrypt 摘要，永不序列化给前端
	AvatarURL      string    `gorm:"type:varchar(255)" json:"avatar_url"`
	Bio            string    `gorm:"type:varchar(200)" json:"bio"`
	FollowingCount int64     `gorm:"not null;default:0" json:"following_count"`
	FollowerCount  int64     `gorm:"not null;default:0" json:"follower_count"`
	CreatedAt      time.Time `json:"created_at"`
}

func (User) TableName() string { return "users" }

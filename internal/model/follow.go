package model

import "time"

// Follow 关注关系表。(follower_id, followee_id) 联合主键 = 关注幂等的数据库兜底。
// idx_follower_time：我关注的人列表；idx_followee_time：关注我的人列表。
type Follow struct {
	FollowerID int64     `gorm:"primaryKey;autoIncrement:false" json:"follower_id"`
	FolloweeID int64     `gorm:"primaryKey;autoIncrement:false" json:"followee_id"`
	CreatedAt  time.Time `gorm:"index:idx_follower_time,priority:2;index:idx_followee_time,priority:2" json:"created_at"`
}

func (Follow) TableName() string { return "follows" }

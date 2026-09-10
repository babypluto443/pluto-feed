package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"pluto_feed/internal/model"
)

// SocialRepo 关注关系存取：幂等关注/取关（事务内同步计数）+ 双向列表游标分页。
type SocialRepo interface {
	// Follow 关注：幂等（INSERT IGNORE + 联合主键），事务内同步双方计数。
	Follow(ctx context.Context, followerID, followeeID int64) error
	// Unfollow 取关：没关注过也无副作用，事务内同步双方计数。
	Unfollow(ctx context.Context, followerID, followeeID int64) error
	// IsFollowing 是否已关注（供详情页"关注"按钮态等场景使用）。
	IsFollowing(ctx context.Context, followerID, followeeID int64) (bool, error)
	// ListFollowing 我关注的人（按关注时间倒序，游标分页）。
	ListFollowing(ctx context.Context, uid int64, before *PageCursor, limit int) ([]model.Follow, error)
	// ListFollowers 关注我的人（按关注时间倒序，游标分页）。
	ListFollowers(ctx context.Context, uid int64, before *PageCursor, limit int) ([]model.Follow, error)
	// FollowingIDs 全量关注对象 id（关注流 IN 查询 + 上限判断用，不分页）。
	FollowingIDs(ctx context.Context, uid int64) ([]int64, error)
}

type socialRepo struct{ db *gorm.DB }

func NewSocialRepo(db *gorm.DB) SocialRepo { return &socialRepo{db: db} }

func (r *socialRepo) Follow(ctx context.Context, followerID, followeeID int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec("INSERT IGNORE INTO follows (follower_id, followee_id) VALUES (?, ?)",
			followerID, followeeID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // 重复关注，幂等返回
		}
		// 双方计数：关注者 following_count+1，被关注者 follower_count+1
		if err := tx.Model(&model.User{}).Where("id = ?", followerID).
			UpdateColumn("following_count", gorm.Expr("following_count + ?", 1)).Error; err != nil {
			return err
		}
		return tx.Model(&model.User{}).Where("id = ?", followeeID).
			UpdateColumn("follower_count", gorm.Expr("follower_count + ?", 1)).Error
	})
}

func (r *socialRepo) Unfollow(ctx context.Context, followerID, followeeID int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("follower_id = ? AND followee_id = ?", followerID, followeeID).
			Delete(&model.Follow{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // 本来就没关注，空操作
		}
		if err := tx.Model(&model.User{}).Where("id = ?", followerID).
			UpdateColumn("following_count", gorm.Expr("following_count - ?", 1)).Error; err != nil {
			return err
		}
		return tx.Model(&model.User{}).Where("id = ?", followeeID).
			UpdateColumn("follower_count", gorm.Expr("follower_count - ?", 1)).Error
	})
}

func (r *socialRepo) IsFollowing(ctx context.Context, followerID, followeeID int64) (bool, error) {
	var f model.Follow
	err := r.db.WithContext(ctx).
		Where("follower_id = ? AND followee_id = ?", followerID, followeeID).
		First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// listByDirection 双向列表的公共实现：direction 决定"谁的关注对象列"。
// 游标语义与 Feed 一致：(created_at DESC, 对方id DESC)。
func (r *socialRepo) listByDirection(ctx context.Context, uid int64, before *PageCursor, limit int, direction string) ([]model.Follow, error) {
	q := r.db.WithContext(ctx).Model(&model.Follow{})
	var otherCol string
	switch direction {
	case "following": // 我关注的人：按 followee_id 维度
		q = q.Where("follower_id = ?", uid)
		otherCol = "followee_id"
	case "followers": // 关注我的人：按 follower_id 维度
		q = q.Where("followee_id = ?", uid)
		otherCol = "follower_id"
	}
	if before != nil {
		q = q.Where("(created_at < ? OR (created_at = ? AND "+otherCol+" < ?))",
			before.Time, before.Time, before.ID)
	}
	var follows []model.Follow
	err := q.Order("created_at DESC, " + otherCol + " DESC").Limit(limit).Find(&follows).Error
	return follows, err
}

func (r *socialRepo) ListFollowing(ctx context.Context, uid int64, before *PageCursor, limit int) ([]model.Follow, error) {
	return r.listByDirection(ctx, uid, before, limit, "following")
}

func (r *socialRepo) ListFollowers(ctx context.Context, uid int64, before *PageCursor, limit int) ([]model.Follow, error) {
	return r.listByDirection(ctx, uid, before, limit, "followers")
}

func (r *socialRepo) FollowingIDs(ctx context.Context, uid int64) ([]int64, error) {
	var ids []int64
	err := r.db.WithContext(ctx).Model(&model.Follow{}).
		Where("follower_id = ?", uid).
		Pluck("followee_id", &ids).Error
	return ids, err
}

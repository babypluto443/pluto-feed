package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"pluto_feed/internal/model"
)

// InteractionRepo 互动存取：幂等写 + **同事务发出计数事件**（T4.3 异步化改造）。
//
// M1 时代的同步计数 UPDATE 已退役：本 repo 不再直接改 posts.like_count/comment_count，
// 而是改成"业务行 + outbox 事件行"**同事务双写**——业务成功则事件必存在，
// 计数更新由 Worker 消费事件完成（最终一致）。
// 幂等纪律升级为四件套：INSERT IGNORE → RowsAffected 判断 → 同事务 outbox → 消费端去重表。
type InteractionRepo interface {
	// Like 点赞：幂等；首次点赞发出 like.created 事件（like_count +1）。
	Like(ctx context.Context, uid, postID int64) error
	// Unlike 取消点赞：没点过也不报错；实际删除发出 like.removed 事件（like_count -1）。
	Unlike(ctx context.Context, uid, postID int64) error
	// CreateComment 发评论：发出 comment.created 事件（comment_count +1）。
	CreateComment(ctx context.Context, c *model.Comment) error
	// GetComment 查单条评论（属主校验前的读取）。不存在返回 (nil, nil)。
	GetComment(ctx context.Context, id int64) (*model.Comment, error)
	// DeleteComment 删评论：发出 comment.deleted 事件（comment_count -1）。
	DeleteComment(ctx context.Context, id int64) error
	// ListCommentsByCursor 评论列表游标分页（同 Feed 的翻页语义）。
	ListCommentsByCursor(ctx context.Context, postID int64, before *PageCursor, limit int) ([]model.Comment, error)
	// HasLiked 当前用户是否已点赞某帖（详情页"我点过没"）。
	HasLiked(ctx context.Context, uid, postID int64) (bool, error)
	// LikedPostIDs 批量：uid 在这些帖子中已点赞的集合（列表页防 N+1）。
	LikedPostIDs(ctx context.Context, uid int64, postIDs []int64) (map[int64]bool, error)
}

type interactionRepo struct {
	db     *gorm.DB
	outbox OutboxRepo // 同事务写事件行需要 outbox repo（组装见 main.go）
}

func NewInteractionRepo(db *gorm.DB, outbox OutboxRepo) InteractionRepo {
	return &interactionRepo{db: db, outbox: outbox}
}

// enqueueCountEvent 在业务事务内写入计数事件。field 只允许白名单字段。
func (r *interactionRepo) enqueueCountEvent(tx *gorm.DB, eventType string, e model.CountEvent) error {
	if e.Field != "like_count" && e.Field != "comment_count" {
		return fmt.Errorf("invalid count field: %s", e.Field)
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return r.outbox.EnqueueInTx(tx, &model.Outbox{EventType: eventType, Payload: payload})
}

// Like 四件套之首：INSERT IGNORE → RowsAffected 判断 → 同事务 outbox 事件行。
// 重复点赞（RowsAffected==0）不产生事件——从源头杜绝重复计数。
func (r *interactionRepo) Like(ctx context.Context, uid, postID int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec("INSERT IGNORE INTO likes (post_id, user_id) VALUES (?, ?)", postID, uid)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // 已点过，幂等返回，不发事件
		}
		return r.enqueueCountEvent(tx, model.EventLikeCreated, model.CountEvent{
			PostID: postID, Field: "like_count", Delta: 1,
		})
	})
}

// Unlike 与 Like 对称：只有真的删除了行才发 like.removed 事件。
func (r *interactionRepo) Unlike(ctx context.Context, uid, postID int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("post_id = ? AND user_id = ?", postID, uid).Delete(&model.Like{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		return r.enqueueCountEvent(tx, model.EventLikeRemoved, model.CountEvent{
			PostID: postID, Field: "like_count", Delta: -1,
		})
	})
}

func (r *interactionRepo) CreateComment(ctx context.Context, c *model.Comment) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(c).Error; err != nil {
			return err
		}
		return r.enqueueCountEvent(tx, model.EventCommentCreate, model.CountEvent{
			PostID: c.PostID, Field: "comment_count", Delta: 1,
		})
	})
}

func (r *interactionRepo) GetComment(ctx context.Context, id int64) (*model.Comment, error) {
	var c model.Comment
	err := r.db.WithContext(ctx).First(&c, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *interactionRepo) DeleteComment(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c model.Comment
		// 事务内重读一次：拿到 post_id 供事件载荷（也避免查删之间的竞态）
		if err := tx.First(&c, id).Error; err != nil {
			return err
		}
		if err := tx.Delete(&model.Comment{}, id).Error; err != nil {
			return err
		}
		return r.enqueueCountEvent(tx, model.EventCommentDelete, model.CountEvent{
			PostID: c.PostID, Field: "comment_count", Delta: -1,
		})
	})
}

// ListCommentsByCursor 复用 Feed 的翻页语义：(created_at DESC, id DESC) + 游标条件。
// 索引 idx_post_time (post_id, created_at, id) 完整覆盖。
func (r *interactionRepo) ListCommentsByCursor(ctx context.Context, postID int64, before *PageCursor, limit int) ([]model.Comment, error) {
	q := r.db.WithContext(ctx).Model(&model.Comment{}).Where("post_id = ?", postID)
	if before != nil {
		q = q.Where("(created_at < ? OR (created_at = ? AND id < ?))",
			before.Time, before.Time, before.ID)
	}
	var comments []model.Comment
	err := q.Order("created_at DESC, id DESC").Limit(limit).Find(&comments).Error
	return comments, err
}

func (r *interactionRepo) HasLiked(ctx context.Context, uid, postID int64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Like{}).
		Where("post_id = ? AND user_id = ?", postID, uid).Count(&n).Error
	return n > 0, err
}

// LikedPostIDs 一条 IN 查询拿整页的点赞集合——列表"是否点过"的批量版（防 N+1）。
func (r *interactionRepo) LikedPostIDs(ctx context.Context, uid int64, postIDs []int64) (map[int64]bool, error) {
	out := make(map[int64]bool, len(postIDs))
	if uid <= 0 || len(postIDs) == 0 {
		return out, nil
	}
	var ids []int64
	err := r.db.WithContext(ctx).Model(&model.Like{}).
		Where("user_id = ? AND post_id IN ?", uid, postIDs).
		Pluck("post_id", &ids).Error
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

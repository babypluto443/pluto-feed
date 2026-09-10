package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"pluto_feed/internal/model"
)

// PageCursor 已解码的游标：翻页时"取比这组值更旧的数据"。
// 排序键是 (created_at, id) —— created_at 决不出全序时由 id 决出，见 idx_time。
type PageCursor struct {
	Time time.Time
	ID   int64
}

// PostRepo 帖子存取接口（service 依赖接口，测试可替换假实现）。
type PostRepo interface {
	Create(ctx context.Context, p *model.Post) error
	GetByID(ctx context.Context, id int64) (*model.Post, error) // 不存在返回 (nil, nil)
	SoftDelete(ctx context.Context, id int64) error
	ListByCursor(ctx context.Context, before *PageCursor, limit int) ([]model.Post, error)
	// ListByUserIDs 按作者集合的游标分页（M2 关注流：user_id IN 关注列表）。
	ListByUserIDs(ctx context.Context, userIDs []int64, before *PageCursor, limit int) ([]model.Post, error)
	// ListByUser 单作者的游标分页（M2 个人主页；idx_user_time 正是为此预留）。
	ListByUser(ctx context.Context, userID int64, before *PageCursor, limit int) ([]model.Post, error)
	// CountByUser 某用户的帖子总数（个人主页计数，M3 再缓存化）。
	CountByUser(ctx context.Context, userID int64) (int64, error)
	// SearchByKeyword 全文搜索（O4：ngram FULLTEXT，支持中文）。
	SearchByKeyword(ctx context.Context, keyword string, limit int) ([]model.Post, error)
}

type postRepo struct{ db *gorm.DB }

func NewPostRepo(db *gorm.DB) PostRepo { return &postRepo{db: db} }

func (r *postRepo) Create(ctx context.Context, p *model.Post) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// GetByID 软删除的帖子查不到：GORM 见到 DeletedAt 字段会自动追加 deleted_at IS NULL。
func (r *postRepo) GetByID(ctx context.Context, id int64) (*model.Post, error) {
	var p model.Post
	err := r.db.WithContext(ctx).First(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SoftDelete 只写 deleted_at 时间戳，行保留。
func (r *postRepo) SoftDelete(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&model.Post{}, id).Error
}

// ListByCursor 游标分页查询：比 before 更旧的 limit 条，按 (created_at DESC, id DESC)。
// before 为 nil 表示第一页（从头取）。
// 多取 limit+1 条的技巧在 service 层做（它才知道还有没有下一页）——这里给多少取多少。
func (r *postRepo) ListByCursor(ctx context.Context, before *PageCursor, limit int) ([]model.Post, error) {
	q := r.db.WithContext(ctx).Model(&model.Post{})
	if before != nil {
		// 复合条件：时间更旧，或时间相同但 id 更小（同秒多帖不丢不重的关键）
		q = q.Where("(created_at < ? OR (created_at = ? AND id < ?))",
			before.Time, before.Time, before.ID)
	}
	var posts []model.Post
	err := q.Order("created_at DESC, id DESC").Limit(limit).Find(&posts).Error
	return posts, err
}

// ListByUserIDs 关注流查询：user_id IN (关注列表) + 游标条件。
// 注意：IN 多值时 idx_user_time 无法保证跨用户的整体有序，
// MySQL 对每个 user_id 走索引前缀扫描后做归并排序——组数受限（见 service 层 5000 上限）时成本可控。
func (r *postRepo) ListByUserIDs(ctx context.Context, userIDs []int64, before *PageCursor, limit int) ([]model.Post, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Model(&model.Post{}).Where("user_id IN ?", userIDs)
	if before != nil {
		q = q.Where("(created_at < ? OR (created_at = ? AND id < ?))",
			before.Time, before.Time, before.ID)
	}
	var posts []model.Post
	err := q.Order("created_at DESC, id DESC").Limit(limit).Find(&posts).Error
	return posts, err
}

// ListByUser 单作者游标分页：idx_user_time (user_id, created_at, id) 完整覆盖。
func (r *postRepo) ListByUser(ctx context.Context, userID int64, before *PageCursor, limit int) ([]model.Post, error) {
	q := r.db.WithContext(ctx).Model(&model.Post{}).Where("user_id = ?", userID)
	if before != nil {
		q = q.Where("(created_at < ? OR (created_at = ? AND id < ?))",
			before.Time, before.Time, before.ID)
	}
	var posts []model.Post
	err := q.Order("created_at DESC, id DESC").Limit(limit).Find(&posts).Error
	return posts, err
}

func (r *postRepo) CountByUser(ctx context.Context, userID int64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Post{}).Where("user_id = ?", userID).Count(&n).Error
	return n, err
}

// SearchByKeyword ngram 全文搜索：MATCH...AGAINST 走 ft_content 索引，
// 软删过滤 + 时间倒序（相关性排序对社区场景意义不大，简单可控）。
func (r *postRepo) SearchByKeyword(ctx context.Context, keyword string, limit int) ([]model.Post, error) {
	var posts []model.Post
	err := r.db.WithContext(ctx).Model(&model.Post{}).
		Where("MATCH(content) AGAINST(? IN NATURAL LANGUAGE MODE)", keyword).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&posts).Error
	return posts, err
}

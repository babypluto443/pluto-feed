package service

import (
	"context"
	"log"
	"strconv"
	"time"
	"unicode/utf8"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/cache"
	"pluto_feed/internal/model"
	"pluto_feed/internal/repository"
)

const (
	MaxCommentLen = 500 // 与 comments.content 列宽一致

	hotLikeWeight    = 1.0 // 热度权重：点赞 1 分
	hotCommentWeight = 2.0 // 评论 2 分（互动深度更高）
)

// InteractionService 互动业务：点赞 / 取消 / 评论 / 删评论 / 评论列表。
// cache 可为 nil（单测/降级）。写路径的缓存策略 = 失效 + 热榜累加：
//   - DEL 帖子缓存：计数变了，缓存里的旧计数必须消失（下次读重建）
//   - ZIncrBy 热榜：点赞/评论是热度信号，分钟桶累加（T3.5）
type InteractionService struct {
	posts        repository.PostRepo
	users        UserRepo
	interactions repository.InteractionRepo
	cache        cache.Store
}

func NewInteractionService(posts repository.PostRepo, users UserRepo, interactions repository.InteractionRepo, c cache.Store) *InteractionService {
	return &InteractionService{posts: posts, users: users, interactions: interactions, cache: c}
}

// afterInteraction 写路径的统一收尾：失效帖子缓存 + 热榜累加。
// 缓存错误一律吞掉记日志——缓存挂了不能影响业务主链路。
func (s *InteractionService) afterInteraction(ctx context.Context, postID int64, hotDelta float64) {
	if s.cache == nil {
		return
	}
	if err := s.cache.Del(ctx, cache.PostKey(postID)); err != nil {
		log.Printf("[cache] del %s: %v（TTL 兜底自愈）", cache.PostKey(postID), err)
	}
	if hotDelta != 0 {
		if err := s.cache.ZIncrBy(ctx, cache.HotBucket(time.Now()), strconv.FormatInt(postID, 10), hotDelta); err != nil {
			log.Printf("[cache] zincrby hot: %v", err)
		}
	}
}

// mustGetPost 取帖子，不存在（含已软删）统一 404。
// 互动对象必须先存在——对不存在的帖子点赞是非法请求。
func (s *InteractionService) mustGetPost(ctx context.Context, postID int64) error {
	p, err := s.posts.GetByID(ctx, postID)
	if err != nil {
		return err
	}
	if p == nil {
		return apierror.ErrNotFound
	}
	return nil
}

// Like 点赞。幂等由 repo 层的 INSERT IGNORE + 联合主键保证：
// 同一用户狂点十次，数据库里也只有一条记录、计数只加一次。
func (s *InteractionService) Like(ctx context.Context, uid, postID int64) error {
	if err := s.mustGetPost(ctx, postID); err != nil {
		return err
	}
	if err := s.interactions.Like(ctx, uid, postID); err != nil {
		return err
	}
	s.afterInteraction(ctx, postID, hotLikeWeight)
	return nil
}

// Unlike 取消点赞。没点过也返回成功（幂等）——客户端"取消"按钮按两次不该报错。
func (s *InteractionService) Unlike(ctx context.Context, uid, postID int64) error {
	if err := s.mustGetPost(ctx, postID); err != nil {
		return err
	}
	if err := s.interactions.Unlike(ctx, uid, postID); err != nil {
		return err
	}
	// 取消点赞：热度不回退（热度是"累计互动量"，取消只影响 like_count）
	s.afterInteraction(ctx, postID, 0)
	return nil
}

// Comment 发评论。评论数在 repo 事务内同步。
func (s *InteractionService) Comment(ctx context.Context, uid, postID int64, content string) (*CommentView, error) {
	if err := s.mustGetPost(ctx, postID); err != nil {
		return nil, err
	}
	n := utf8.RuneCountInString(content)
	if n < 1 {
		return nil, apierror.New(400, 40020, "评论不能为空")
	}
	if n > MaxCommentLen {
		return nil, apierror.New(400, 40021, "评论最多 500 字")
	}

	c := &model.Comment{PostID: postID, UserID: uid, Content: content}
	if err := s.interactions.CreateComment(ctx, c); err != nil {
		return nil, err
	}

	author, err := s.users.GetByID(ctx, uid)
	if err != nil {
		return nil, err
	}
	view := NewCommentView(c, author)
	s.afterInteraction(ctx, postID, hotCommentWeight)
	return &view, nil
}

// DeleteComment 删评论：仅限本人（评论不像帖子那样开放转发场景，规则最简单）。
func (s *InteractionService) DeleteComment(ctx context.Context, uid, commentID int64) error {
	c, err := s.interactions.GetComment(ctx, commentID)
	if err != nil {
		return err
	}
	if c == nil {
		return apierror.ErrNotFound
	}
	if c.UserID != uid {
		return apierror.ErrForbidden
	}
	if err := s.interactions.DeleteComment(ctx, commentID); err != nil {
		return err
	}
	s.afterInteraction(ctx, c.PostID, 0)
	return nil
}

// ListComments 评论列表：游标分页（与 Feed 同一套语义），作者批量填充防 N+1。
func (s *InteractionService) ListComments(ctx context.Context, postID int64, cursor string, limit int) (*CommentPage, error) {
	if err := s.mustGetPost(ctx, postID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}

	var before *repository.PageCursor
	if cursor != "" {
		c, err := DecodeCursor(cursor)
		if err != nil {
			return nil, err
		}
		before = &c
	}

	comments, err := s.interactions.ListCommentsByCursor(ctx, postID, before, limit+1)
	if err != nil {
		return nil, err
	}
	hasMore := len(comments) > limit
	if hasMore {
		comments = comments[:limit]
	}

	// 作者批量查询（同 Feed 的防 N+1 套路）
	ids := make([]int64, 0, len(comments))
	seen := make(map[int64]struct{}, len(comments))
	for i := range comments {
		if _, ok := seen[comments[i].UserID]; !ok {
			seen[comments[i].UserID] = struct{}{}
			ids = append(ids, comments[i].UserID)
		}
	}
	authors, err := s.users.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	items := make([]CommentView, 0, len(comments))
	for i := range comments {
		items = append(items, NewCommentView(&comments[i], authors[comments[i].UserID]))
	}

	page := &CommentPage{Items: items, HasMore: hasMore}
	if hasMore && len(comments) > 0 {
		last := comments[len(comments)-1]
		page.NextCursor = EncodeCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

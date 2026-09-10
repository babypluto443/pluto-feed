package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/cache"
	"pluto_feed/internal/model"
	"pluto_feed/internal/repository"
)

// 分页常量：默认一页 20，客户端要再多也最多 50（防止 limit=100000 拖垮数据库）。
const (
	DefaultPageSize = 20
	MaxPageSize     = 50
)

// EncodeCursor 把排序键 (created_at, id) 编码成不透明字符串。
// 格式：base64("unix_nano_id")。
// 对客户端是不透明的——它只该原样回传，不该解析它（想改格式也只改这一处）。
func EncodeCursor(t time.Time, id int64) string {
	raw := fmt.Sprintf("%d_%d", t.UnixNano(), id)
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor 反向解码。客户端伪造/篡改的 cursor 在这里被拦下（返回 400）。
func DecodeCursor(s string) (repository.PageCursor, error) {
	raw, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return repository.PageCursor{}, apierror.New(400, 40005, "无效的游标")
	}
	parts := strings.SplitN(string(raw), "_", 2)
	if len(parts) != 2 {
		return repository.PageCursor{}, apierror.New(400, 40005, "无效的游标")
	}
	nano, err1 := strconv.ParseInt(parts[0], 10, 64)
	id, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return repository.PageCursor{}, apierror.New(400, 40005, "无效的游标")
	}
	return repository.PageCursor{Time: time.Unix(0, nano), ID: id}, nil
}

// FeedService 推荐流（M1 = 全站时间倒序）。
type FeedService struct {
	posts repository.PostRepo
	users        repository.UserRepo
	social       repository.SocialRepo
	cache        cache.Store // 可为 nil；热榜结果缓存（T3.5）
	interactions repository.InteractionRepo // 查询"当前用户是否点过赞"
}

// NewFeedService social/cache/interactions 依赖为 M2/M3/M4 新增；组装见 main.go。
func NewFeedService(posts repository.PostRepo, users repository.UserRepo, social repository.SocialRepo, c cache.Store, interactions repository.InteractionRepo) *FeedService {
	return &FeedService{posts: posts, users: users, social: social, cache: c, interactions: interactions}
}

// MaxFollowingFeedIDs 关注流 IN 查询的作者数上限（D-M2-3）。
// 超过即拒绝并提示——这是"读扩散"方案的边界，也是 M4 推拉模式的动因。
const MaxFollowingFeedIDs = 5000

// FeedPage 一页 Feed。
type FeedPage struct {
	Items      []PostView `json:"items"`
	NextCursor string     `json:"next_cursor"` // 空串 = 没有下一页了
	HasMore    bool       `json:"has_more"`
}

// List 取一页帖子。cursor 为空 = 第一页；翻页时把上一页响应里的
// next_cursor 原样传回来。
//
// 翻页原理（为什么不用页码）：
//   - 页码分页 OFFSET 100000 要先扫过并丢掉前 10 万行，越翻越慢
//   - 游标分页 WHERE (created_at,id) < (上一页最后一行) 直接从索引定位，恒定代价
//   - 且翻页期间有新帖插入，页码会"串页"，游标不会
func (s *FeedService) List(ctx context.Context, viewerUID int64, cursor string, limit int) (*FeedPage, error) {
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

	// 多取 1 条：取满 limit+1 说明还有下一页（不用额外 COUNT 查总数）
	posts, err := s.posts.ListByCursor(ctx, before, limit+1)
	if err != nil {
		return nil, err
	}
	return s.buildPage(ctx, posts, limit, viewerUID)
}

// ListFollowingFeed 关注流（M2）：只看已关注用户的帖子。
// 实现路径：全量取关注对象 id → IN 查询 + 现有游标条件 → 复用分页装配件。
//
// 大 V 问题的引子（D-M2-3）：IN 方案的作者数有上限（MaxFollowingFeedIDs），
// 超限场景（关注几万大 V 的用户）靠"读扩散"不可行——M4 用推拉模式解决。
func (s *FeedService) ListFollowingFeed(ctx context.Context, viewerUID int64, cursor string, limit int) (*FeedPage, error) {
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}

	ids, err := s.social.FollowingIDs(ctx, viewerUID)
	if err != nil {
		return nil, err
	}
	// 空关注列表：返回空页（合法状态，不是错误）
	if len(ids) == 0 {
		return &FeedPage{Items: []PostView{}}, nil
	}
	if len(ids) > MaxFollowingFeedIDs {
		return nil, apierror.New(400, 40040,
			"关注列表过大，关注流暂不支持（推拉模式规划中，见 M4）")
	}

	var before *repository.PageCursor
	if cursor != "" {
		c, err := DecodeCursor(cursor)
		if err != nil {
			return nil, err
		}
		before = &c
	}

	posts, err := s.posts.ListByUserIDs(ctx, ids, before, limit+1)
	if err != nil {
		return nil, err
	}
	return s.buildPage(ctx, posts, limit, viewerUID)
}

// buildPage 分页装配件：limit+1 → hasMore 判断 → 作者批量填充 → liked 批量填充 → 游标生成。
// List 与 ListFollowingFeed 共用，避免同一套逻辑写两遍（DRY）。
// viewerUID > 0 时批量查点赞态（一条 IN，防 N+1）；未登录恒为 false。
func (s *FeedService) buildPage(ctx context.Context, posts []model.Post, limit int, viewerUID int64) (*FeedPage, error) {
	hasMore := len(posts) > limit
	if hasMore {
		posts = posts[:limit]
	}

	// 批量取作者：收集去重后的 uid，一条 IN 查询搞定（防 N+1）
	idSet := make(map[int64]struct{}, len(posts))
	ids := make([]int64, 0, len(posts))
	for i := range posts {
		if _, ok := idSet[posts[i].UserID]; !ok {
			idSet[posts[i].UserID] = struct{}{}
			ids = append(ids, posts[i].UserID)
		}
	}
	authors, err := s.users.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	items := make([]PostView, 0, len(posts))
	for i := range posts {
		items = append(items, NewPostView(&posts[i], authors[posts[i].UserID]))
	}

	// 当前用户的点赞态：一次批量查询填充（登录才查）
	if viewerUID > 0 && s.interactions != nil && len(items) > 0 {
		postIDs := make([]int64, 0, len(posts))
		for i := range posts {
			postIDs = append(postIDs, posts[i].ID)
		}
		if likedMap, err := s.interactions.LikedPostIDs(ctx, viewerUID, postIDs); err == nil {
			for i := range items {
				items[i].Liked = likedMap[items[i].ID]
			}
		}
	}

	page := &FeedPage{Items: items, HasMore: hasMore}
	if hasMore && len(posts) > 0 {
		last := posts[len(posts)-1]
		page.NextCursor = EncodeCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

// ListHot 热榜（T3.5）：分钟桶 ZINCRBY 聚合 → 取 topN 帖子。
// 聚合结果缓存 60 秒——热榜本身不需要强实时，缓存掉聚合查询的开销。
// 缓存未启用时每次实时聚合（正确性不受影响，只是多花点 CPU/IO）。
func (s *FeedService) ListHot(ctx context.Context, viewerUID int64, limit int) (*FeedPage, error) {
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	if s.cache == nil {
		return s.loadHot(ctx, limit, viewerUID)
	}

	key := cache.HotResultKey(limit)
	var cached []PostView
	if found, _ := s.cache.GetJSON(ctx, key, &cached); found && cached != nil {
		return &FeedPage{Items: s.fillLiked(ctx, cached, viewerUID)}, nil // 缓存的是公共榜单，liked 必须按请求者现算
	}

	page, err := s.loadHot(ctx, limit, viewerUID)
	if err != nil {
		return nil, err
	}
	// 入缓存前剥离 viewer 私有状态（liked），否则 A 的点赞态会泄漏给 B
	public := make([]PostView, len(page.Items))
	copy(public, page.Items)
	for i := range public {
		public[i].Liked = false
	}
	if err := s.cache.SetJSON(ctx, key, public, time.Minute); err != nil {
		_ = err
	}
	return page, nil
}

// fillLiked 就地填充一批视图的点赞态（未登录跳过）。
func (s *FeedService) fillLiked(ctx context.Context, items []PostView, viewerUID int64) []PostView {
	if viewerUID <= 0 || s.interactions == nil || len(items) == 0 {
		return items
	}
	ids := make([]int64, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].ID)
	}
	if likedMap, err := s.interactions.LikedPostIDs(ctx, viewerUID, ids); err == nil {
		for i := range items {
			items[i].Liked = likedMap[items[i].ID]
		}
	}
	return items
}

func (s *FeedService) loadHot(ctx context.Context, limit int, viewerUID int64) (*FeedPage, error) {
	if s.cache == nil {
		// 无缓存层：没有热度数据来源，返回空榜（正确性优先，M3 起服务默认启用缓存）
		return &FeedPage{Items: []PostView{}}, nil
	}
	items, err := s.cache.HotTopN(ctx, 60, limit) // 聚合最近 60 分钟
	if err != nil {
		return nil, err
	}
	page := &FeedPage{Items: []PostView{}}
	for _, it := range items {
		p, err := s.posts.GetByID(ctx, it.PostID)
		if err != nil || p == nil { // 帖子可能已被删：跳过
			continue
		}
		author, err := s.users.GetByID(ctx, p.UserID)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, NewPostView(p, author))
	}
	page.Items = s.fillLiked(ctx, page.Items, viewerUID)
	return page, nil
}

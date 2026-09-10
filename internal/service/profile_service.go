package service

import (
	"context"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/model"
	"pluto_feed/internal/repository"
)

// ProfileService 个人主页聚合：用户资料 + 社交计数 + 帖子列表。
// 独立于 social_service：关注是"关系操作"，个人主页是"读侧聚合"，职责不同。
type ProfileService struct {
	posts        repository.PostRepo
	users        UserRepo
	social       repository.SocialRepo
	interactions repository.InteractionRepo // 帖子列表的点赞态填充
}

func NewProfileService(posts repository.PostRepo, users UserRepo, social repository.SocialRepo, interactions repository.InteractionRepo) *ProfileService {
	return &ProfileService{posts: posts, users: users, social: social, interactions: interactions}
}

// ProfileView 个人主页响应。
type ProfileView struct {
	ID             int64     `json:"id"`
	Nickname       string    `json:"nickname"`
	AvatarURL      string    `json:"avatar_url"`
	Bio            string    `json:"bio"`
	FollowingCount int64     `json:"following_count"`
	FollowerCount  int64     `json:"follower_count"`
	PostCount      int64     `json:"post_count"`
	Posts          *FeedPage `json:"posts"` // 帖子列表：同一套游标分页
}

// Get 个人主页（公开）。targetID 是被查看的用户；viewerID 当前登录者（可为 0 = 未登录）。
// viewerID 预留：M3 加 "is_following"（我是否关注了 TA）时使用，本期先不做（YAGNI）。
func (s *ProfileService) Get(ctx context.Context, viewerID, targetID int64, cursor string, limit int) (*ProfileView, error) {
	u, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, apierror.ErrNotFound
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

	// 帖子列表：idx_user_time (user_id, created_at, id) 正是 T1.1 预留的索引在此兑现
	posts, err := s.posts.ListByUser(ctx, targetID, before, limit+1)
	if err != nil {
		return nil, err
	}
	page, err := s.buildOwnPostsPage(ctx, u, posts, limit, viewerID)
	if err != nil {
		return nil, err
	}

	postCount, err := s.posts.CountByUser(ctx, targetID)
	if err != nil {
		return nil, err
	}

	return &ProfileView{
		ID:             u.ID,
		Nickname:       u.Nickname,
		AvatarURL:      u.AvatarURL,
		Bio:            u.Bio,
		FollowingCount: u.FollowingCount,
		FollowerCount:  u.FollowerCount,
		PostCount:      postCount,
		Posts:          page,
	}, nil
}

// buildOwnPostsPage 主页帖子装配件：作者就是本人，天然无 N+1。
func (s *ProfileService) buildOwnPostsPage(ctx context.Context, u *model.User, posts []model.Post, limit int, viewerID int64) (*FeedPage, error) {
	hasMore := len(posts) > limit
	if hasMore {
		posts = posts[:limit]
	}
	author := AuthorView{ID: u.ID, Nickname: u.Nickname, AvatarURL: u.AvatarURL}
	items := make([]PostView, 0, len(posts))
	for i := range posts {
		v := NewPostView(&posts[i], u)
		v.Author = author
		items = append(items, v)
	}
	// 当前用户点赞态批量填充（未登录跳过）
	if s.interactions != nil && len(items) > 0 {
		ids := make([]int64, 0, len(items))
		for i := range items {
			ids = append(ids, items[i].ID)
		}
		if likedMap, err := s.interactions.LikedPostIDs(ctx, viewerID, ids); err == nil && viewerID > 0 {
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

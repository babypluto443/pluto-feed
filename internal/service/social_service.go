package service

import (
	"context"
	"log"
	"time"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/cache"
	"pluto_feed/internal/model"
	"pluto_feed/internal/repository"
)

// SocialService 关注业务：关注 / 取关 / 双向列表。
type SocialService struct {
	users  UserRepo
	social repository.SocialRepo
	cache  cache.Store // 可为 nil；关注计数变化时失效双方的用户缓存
}

func NewSocialService(users UserRepo, social repository.SocialRepo, c cache.Store) *SocialService {
	return &SocialService{users: users, social: social, cache: c}
}

// afterSocial 关注/取关的统一收尾：双方用户缓存失效（following_count/follower_count 变了）。
func (s *SocialService) afterSocial(ctx context.Context, followerID, followeeID int64) {
	if s.cache == nil {
		return
	}
	if err := s.cache.Del(ctx, cache.UserKey(followerID), cache.UserKey(followeeID)); err != nil {
		log.Printf("[cache] del user caches: %v（TTL 兜底自愈）", err)
	}
}

// FollowUserView 列表里的"一个人"：用户资料 + 关注时间。
type FollowUserView struct {
	ID         int64     `json:"id"`
	Nickname   string    `json:"nickname"`
	AvatarURL  string    `json:"avatar_url"`
	Bio        string    `json:"bio"`
	FollowedAt time.Time `json:"followed_at"`
}

// SocialListPage 一页关注/粉丝列表（翻页结构与 FeedPage 一致）。
type SocialListPage struct {
	Items      []FollowUserView `json:"items"`
	NextCursor string           `json:"next_cursor"`
	HasMore    bool             `json:"has_more"`
}

// Follow 关注某人。
func (s *SocialService) Follow(ctx context.Context, uid, targetID int64) error {
	if err := s.checkTarget(ctx, uid, targetID); err != nil {
		return err
	}
	if err := s.social.Follow(ctx, uid, targetID); err != nil {
		return err
	}
	s.afterSocial(ctx, uid, targetID)
	return nil
}

// Unfollow 取消关注。没关注过 → 空操作成功（幂等）。
func (s *SocialService) Unfollow(ctx context.Context, uid, targetID int64) error {
	if err := s.checkTarget(ctx, uid, targetID); err != nil {
		return err
	}
	if err := s.social.Unfollow(ctx, uid, targetID); err != nil {
		return err
	}
	s.afterSocial(ctx, uid, targetID)
	return nil
}

// checkTarget 目标合法性：不能操作自己；目标用户必须存在。
func (s *SocialService) checkTarget(ctx context.Context, uid, targetID int64) error {
	if uid == targetID {
		return apierror.New(400, 40030, "不能关注自己")
	}
	u, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return err
	}
	if u == nil {
		return apierror.ErrNotFound
	}
	return nil
}

// ListFollowing 我关注的人。
func (s *SocialService) ListFollowing(ctx context.Context, uid int64, cursor string, limit int) (*SocialListPage, error) {
	return s.list(ctx, uid, cursor, limit, true)
}

// ListFollowers 关注我的人。
func (s *SocialService) ListFollowers(ctx context.Context, uid int64, cursor string, limit int) (*SocialListPage, error) {
	return s.list(ctx, uid, cursor, limit, false)
}

func (s *SocialService) list(ctx context.Context, uid int64, cursor string, limit int, following bool) (*SocialListPage, error) {
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

	var follows []model.Follow
	var err error
	if following {
		follows, err = s.social.ListFollowing(ctx, uid, before, limit+1)
	} else {
		follows, err = s.social.ListFollowers(ctx, uid, before, limit+1)
	}
	if err != nil {
		return nil, err
	}
	hasMore := len(follows) > limit
	if hasMore {
		follows = follows[:limit]
	}

	// 批量取用户资料（同 Feed 的防 N+1 套路）
	ids := make([]int64, 0, len(follows))
	seen := make(map[int64]struct{}, len(follows))
	for _, f := range follows {
		other := f.FolloweeID
		if !following {
			other = f.FollowerID
		}
		if _, ok := seen[other]; !ok {
			seen[other] = struct{}{}
			ids = append(ids, other)
		}
	}
	users, err := s.users.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	items := make([]FollowUserView, 0, len(follows))
	for _, f := range follows {
		other := f.FolloweeID
		if !following {
			other = f.FollowerID
		}
		v := FollowUserView{ID: other, FollowedAt: f.CreatedAt}
		if u := users[other]; u != nil {
			v.Nickname = u.Nickname
			v.AvatarURL = u.AvatarURL
			v.Bio = u.Bio
		}
		items = append(items, v)
	}

	page := &SocialListPage{Items: items, HasMore: hasMore}
	if hasMore && len(follows) > 0 {
		last := follows[len(follows)-1]
		other := last.FolloweeID
		if !following {
			other = last.FollowerID
		}
		page.NextCursor = EncodeCursor(last.CreatedAt, other)
	}
	return page, nil
}

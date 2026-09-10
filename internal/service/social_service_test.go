package service

import (
	"context"
	"testing"
	"time"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/model"
	"pluto_feed/internal/repository"
)

// fakeSocialRepo 内存假实现：忠实复刻 SQL 语义（INSERT IGNORE + RowsAffected + 计数同步）。
type fakeSocialRepo struct {
	follows    map[int64]map[int64]time.Time // followerID → followeeID → 关注时间
	userCounts map[int64][2]int64            // uid → [following_count, follower_count]
	clock      int64
}

func newFakeSocialRepo() *fakeSocialRepo {
	return &fakeSocialRepo{
		follows:    map[int64]map[int64]time.Time{},
		userCounts: map[int64][2]int64{},
	}
}

func (f *fakeSocialRepo) tick() time.Time {
	f.clock++
	return time.Unix(0, f.clock)
}

func (f *fakeSocialRepo) Follow(_ context.Context, followerID, followeeID int64) error {
	set, ok := f.follows[followerID]
	if !ok {
		set = map[int64]time.Time{}
		f.follows[followerID] = set
	}
	if _, exists := set[followeeID]; exists {
		return nil // INSERT IGNORE 命中已存在行
	}
	set[followeeID] = f.tick()
	f.bump(followerID, 1, 0)
	f.bump(followeeID, 0, 1)
	return nil
}

func (f *fakeSocialRepo) Unfollow(_ context.Context, followerID, followeeID int64) error {
	set, ok := f.follows[followerID]
	if !ok {
		return nil
	}
	if _, exists := set[followeeID]; !exists {
		return nil
	}
	delete(set, followeeID)
	f.bump(followerID, -1, 0)
	f.bump(followeeID, 0, -1)
	return nil
}

func (f *fakeSocialRepo) IsFollowing(_ context.Context, followerID, followeeID int64) (bool, error) {
	_, ok := f.follows[followerID][followeeID]
	return ok, nil
}

func (f *fakeSocialRepo) bump(uid int64, followingDelta, followerDelta int64) {
	cur := f.userCounts[uid]
	cur[0] += followingDelta
	cur[1] += followerDelta
	f.userCounts[uid] = cur
}

func (f *fakeSocialRepo) listByDirection(uid int64, before *repository.PageCursor, limit int, following bool) ([]model.Follow, error) {
	var out []model.Follow
	for followerID, set := range f.follows {
		for followeeID, at := range set {
			var self, other int64
			if following {
				self, other = followerID, followeeID
			} else {
				self, other = followeeID, followerID
			}
			if self != uid {
				continue
			}
			if before != nil {
				if !(at.Before(before.Time) || (at.Equal(before.Time) && other < before.ID)) {
					continue
				}
			}
			out = append(out, model.Follow{FollowerID: followerID, FolloweeID: followeeID, CreatedAt: at})
		}
	}
	// (created_at DESC, 对方id DESC)
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			a, b := out[i], out[j]
			aOther, bOther := a.FolloweeID, b.FolloweeID
			if !following {
				aOther, bOther = a.FollowerID, b.FollowerID
			}
			if a.CreatedAt.Before(b.CreatedAt) || (a.CreatedAt.Equal(b.CreatedAt) && aOther < bOther) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeSocialRepo) ListFollowing(_ context.Context, uid int64, before *repository.PageCursor, limit int) ([]model.Follow, error) {
	return f.listByDirection(uid, before, limit, true)
}

func (f *fakeSocialRepo) ListFollowers(_ context.Context, uid int64, before *repository.PageCursor, limit int) ([]model.Follow, error) {
	return f.listByDirection(uid, before, limit, false)
}

func (f *fakeSocialRepo) FollowingIDs(_ context.Context, uid int64) ([]int64, error) {
	var ids []int64
	for _, set := range f.follows {
		_ = set
		break
	}
	for followeeID := range f.follows[uid] {
		ids = append(ids, followeeID)
	}
	return ids, nil
}

var _ repository.SocialRepo = (*fakeSocialRepo)(nil) // 编译期断言

// —— 测试套件 ——

type socialFixture struct {
	svc *SocialService
	ur  *fakeUserRepo
	sr  *fakeSocialRepo
}

func newSocialFixture() *socialFixture {
	ur := newFakeUserRepo()
	sr := newFakeSocialRepo()
	return &socialFixture{svc: NewSocialService(ur, sr, nil), ur: ur, sr: sr}
}

func (fx *socialFixture) seedUser(name string) int64 {
	_ = fx.ur.Create(ctx, &model.User{Nickname: name, PasswordHash: "x"})
	return fx.ur.nextID
}

func TestFollow_SuccessAndCounts(t *testing.T) {
	fx := newSocialFixture()
	alice := fx.seedUser("alice")
	bob := fx.seedUser("bob")

	if err := fx.svc.Follow(ctx, alice, bob); err != nil {
		t.Fatalf("follow: %v", err)
	}
	if got := fx.sr.userCounts[alice][0]; got != 1 {
		t.Errorf("alice following_count = %d, want 1", got)
	}
	if got := fx.sr.userCounts[bob][1]; got != 1 {
		t.Errorf("bob follower_count = %d, want 1", got)
	}
}

func TestFollow_SelfRejected(t *testing.T) {
	fx := newSocialFixture()
	alice := fx.seedUser("alice")

	err := fx.svc.Follow(ctx, alice, alice)
	be, ok := err.(*apierror.BizError)
	if !ok || be.Code != 40030 {
		t.Errorf("err = %v, want code 40030", err)
	}
}

func TestFollow_NonexistentUser(t *testing.T) {
	fx := newSocialFixture()
	alice := fx.seedUser("alice")

	if err := fx.svc.Follow(ctx, alice, 9999); err != apierror.ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestFollow_Idempotent(t *testing.T) {
	fx := newSocialFixture()
	alice := fx.seedUser("alice")
	bob := fx.seedUser("bob")

	_ = fx.svc.Follow(ctx, alice, bob)
	_ = fx.svc.Follow(ctx, alice, bob) // 重复关注

	if got := fx.sr.userCounts[alice][0]; got != 1 {
		t.Errorf("alice following_count = %d, want 1 (idempotent!)", got)
	}
}

func TestUnfollow_NotFollowingIsNoop(t *testing.T) {
	fx := newSocialFixture()
	alice := fx.seedUser("alice")
	bob := fx.seedUser("bob")

	if err := fx.svc.Unfollow(ctx, alice, bob); err != nil {
		t.Fatalf("unfollow: %v", err)
	}
	if got := fx.sr.userCounts[alice][0]; got != 0 {
		t.Errorf("alice following_count = %d, want 0 (no negative!)", got)
	}
}

func TestUnfollow_RoundTrip(t *testing.T) {
	fx := newSocialFixture()
	alice := fx.seedUser("alice")
	bob := fx.seedUser("bob")

	_ = fx.svc.Follow(ctx, alice, bob)
	_ = fx.svc.Unfollow(ctx, alice, bob)

	if got := fx.sr.userCounts[alice][0]; got != 0 {
		t.Errorf("alice following_count = %d, want 0", got)
	}
	if got := fx.sr.userCounts[bob][1]; got != 0 {
		t.Errorf("bob follower_count = %d, want 0", got)
	}
}

func TestListFollowing_CursorPagination(t *testing.T) {
	fx := newSocialFixture()
	alice := fx.seedUser("alice")
	var targets []int64
	for _, name := range []string{"u1", "u2", "u3", "u4", "u5"} {
		targets = append(targets, fx.seedUser(name))
	}
	for _, target := range targets {
		_ = fx.svc.Follow(ctx, alice, target)
	}

	// limit=2 翻完三页：不丢不重，新→旧
	var all []FollowUserView
	cursor := ""
	for {
		page, err := fx.svc.ListFollowing(ctx, alice, cursor, 2)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		all = append(all, page.Items...)
		if !page.HasMore {
			break
		}
		cursor = page.NextCursor
	}
	if len(all) != 5 {
		t.Fatalf("got %d, want 5", len(all))
	}
	seen := map[int64]bool{}
	for _, v := range all {
		if seen[v.ID] {
			t.Errorf("duplicate user %d", v.ID)
		}
		seen[v.ID] = true
	}
	// alice 是用户 1，5 个目标用户是 2~6；最新关注的是 u5(id=6)
	if all[0].ID != 6 || all[4].ID != 2 {
		t.Errorf("order: first=%d last=%d, want 6..2 (new→old)", all[0].ID, all[4].ID)
	}
}

func TestListFollowers_ReturnsFans(t *testing.T) {
	fx := newSocialFixture()
	alice := fx.seedUser("alice")
	bob := fx.seedUser("bob")
	carol := fx.seedUser("carol")

	_ = fx.svc.Follow(ctx, bob, alice)    // bob → alice
	_ = fx.svc.Follow(ctx, carol, alice)  // carol → alice

	page, err := fx.svc.ListFollowers(ctx, alice, "", 10)
	if err != nil {
		t.Fatalf("list followers: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("followers = %d, want 2", len(page.Items))
	}
	if page.Items[0].ID != carol || page.Items[1].ID != bob {
		t.Errorf("order = [%d,%d], want [carol,bob] (new→old)", page.Items[0].ID, page.Items[1].ID)
	}
}

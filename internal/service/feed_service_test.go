package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/model"
	"pluto_feed/internal/repository"
)

// fakePostRepo 内存假实现。关键：必须忠实复刻 SQL 语义——
// (created_at DESC, id DESC) 排序 + 游标条件 (created_at < ? OR (= ? AND id < ?))。
// 它是 service 逻辑的"镜子"，假实现歪了测试就是自欺欺人。
type fakePostRepo struct {
	posts  []model.Post // 按插入顺序存
	nextID int64
}

func newFakePostRepo() *fakePostRepo { return &fakePostRepo{} }

func (f *fakePostRepo) seed(t time.Time, userID int64) model.Post {
	f.nextID++
	p := model.Post{ID: f.nextID, UserID: userID, Content: fmt.Sprintf("post-%d", f.nextID), CreatedAt: t}
	f.posts = append(f.posts, p)
	return p
}

func (f *fakePostRepo) Create(_ context.Context, p *model.Post) error {
	f.nextID++
	p.ID = f.nextID
	f.posts = append(f.posts, *p)
	return nil
}

func (f *fakePostRepo) GetByID(_ context.Context, id int64) (*model.Post, error) {
	for i := range f.posts {
		if f.posts[i].ID == id {
			return &f.posts[i], nil
		}
	}
	return nil, nil
}

func (f *fakePostRepo) SoftDelete(_ context.Context, id int64) error {
	for i := range f.posts {
		if f.posts[i].ID == id {
			f.posts = append(f.posts[:i], f.posts[i+1:]...)
			return nil
		}
	}
	return nil
}

func (f *fakePostRepo) ListByCursor(_ context.Context, before *repository.PageCursor, limit int) ([]model.Post, error) {
	// 复刻 SQL：先过滤游标，再 (created_at DESC, id DESC) 排序，再 LIMIT
	var filtered []model.Post
	for _, p := range f.posts {
		if before != nil {
			if !(p.CreatedAt.Before(before.Time) ||
				(p.CreatedAt.Equal(before.Time) && p.ID < before.ID)) {
				continue
			}
		}
		filtered = append(filtered, p)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if !filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
		}
		return filtered[i].ID > filtered[j].ID
	})
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (f *fakePostRepo) ListByUserIDs(_ context.Context, userIDs []int64, before *repository.PageCursor, limit int) ([]model.Post, error) {
	idSet := make(map[int64]bool, len(userIDs))
	for _, id := range userIDs {
		idSet[id] = true
	}
	var filtered []model.Post
	for _, p := range f.posts {
		if !idSet[p.UserID] {
			continue
		}
		if before != nil {
			if !(p.CreatedAt.Before(before.Time) ||
				(p.CreatedAt.Equal(before.Time) && p.ID < before.ID)) {
				continue
			}
		}
		filtered = append(filtered, p)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if !filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
		}
		return filtered[i].ID > filtered[j].ID
	})
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (f *fakePostRepo) ListByUser(ctx context.Context, userID int64, before *repository.PageCursor, limit int) ([]model.Post, error) {
	return f.ListByUserIDs(ctx, []int64{userID}, before, limit)
}

func (f *fakePostRepo) SearchByKeyword(_ context.Context, keyword string, limit int) ([]model.Post, error) {
	var out []model.Post
	for _, p := range f.posts {
		if strings.Contains(p.Content, keyword) {
			out = append(out, p)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakePostRepo) CountByUser(_ context.Context, userID int64) (int64, error) {
	var n int64
	for _, p := range f.posts {
		if p.UserID == userID {
			n++
		}
	}
	return n, nil
}

// —— 游标编解码 ——

func TestCursor_RoundTrip(t *testing.T) {
	at := time.Date(2026, 9, 7, 12, 0, 0, 123456789, time.UTC)
	enc := EncodeCursor(at, 105)

	c, err := DecodeCursor(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !c.Time.Equal(at) || c.ID != 105 {
		t.Errorf("got (%v, %d), want (%v, 105)", c.Time, c.ID, at)
	}
}

func TestCursor_ForgedRejected(t *testing.T) {
	bad := []string{"不是base64!!", "MTIzNDU=", "", "_", "abc_def"}
	for _, s := range bad {
		if s == "" {
			continue // 空串表示第一页，由 service 层处理，不走 Decode
		}
		if _, err := DecodeCursor(s); err == nil {
			t.Errorf("cursor %q should be rejected", s)
		} else if be, ok := err.(*apierror.BizError); !ok || be.HTTPStatus != 400 {
			t.Errorf("cursor %q: err should be 400 BizError, got %v", s, err)
		}
	}
}

// —— Feed 翻页 ——

func TestFeed_FirstPageAndPagination(t *testing.T) {
	base := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	fr := newFakePostRepo()
	for i := 0; i < 7; i++ {
		fr.seed(base.Add(time.Duration(i)*time.Minute), 1)
	}
	s := NewFeedService(fr, newFakeUserRepo(), newFakeSocialRepo(), nil, newFakeInteractionRepo())

	// 第一页
	p1, err := s.List(ctx, 0, "", 3)
	if err != nil {
		t.Fatalf("list page1: %v", err)
	}
	if len(p1.Items) != 3 || !p1.HasMore || p1.NextCursor == "" {
		t.Fatalf("page1 = %d items, hasMore=%v", len(p1.Items), p1.HasMore)
	}
	if p1.Items[0].Content != "post-7" {
		t.Errorf("newest first: got %q, want post-7", p1.Items[0].Content)
	}

	// 第二页、第三页
	p2, _ := s.List(ctx, 0, p1.NextCursor, 3)
	p3, _ := s.List(ctx, 0, p2.NextCursor, 3)

	if len(p2.Items) != 3 || len(p3.Items) != 1 {
		t.Fatalf("page2=%d page3=%d, want 3 and 1", len(p2.Items), len(p3.Items))
	}
	if p3.HasMore {
		t.Error("page3 should be the last (has_more=false)")
	}

	// 三页拼起来必须不丢不重，且严格按新→旧
	seen := map[string]bool{}
	var order []string
	for _, p := range [3]*FeedPage{p1, p2, p3} {
		for _, it := range p.Items {
			if seen[it.Content] {
				t.Errorf("duplicate: %s", it.Content)
			}
			seen[it.Content] = true
			order = append(order, it.Content)
		}
	}
	if len(seen) != 7 {
		t.Errorf("lost posts: got %d unique, want 7", len(seen))
	}
	want := []string{"post-7", "post-6", "post-5", "post-4", "post-3", "post-2", "post-1"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestFeed_SameSecondTiesNoLossNoDup(t *testing.T) {
	// 同一纳秒时间戳下 5 条帖子 —— 全靠 id 决出顺序（任务 14 的核心边界）
	base := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	fr := newFakePostRepo()
	for i := 0; i < 5; i++ {
		fr.seed(base, 1) // CreatedAt 完全相同
	}
	s := NewFeedService(fr, newFakeUserRepo(), newFakeSocialRepo(), nil, newFakeInteractionRepo())

	var order []string
	cursor := ""
	for {
		page, err := s.List(ctx, 0, cursor, 2)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, it := range page.Items {
			order = append(order, it.Content)
		}
		if !page.HasMore {
			break
		}
		cursor = page.NextCursor
	}

	want := []string{"post-5", "post-4", "post-3", "post-2", "post-1"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("tie order = %v, want %v (same-second ties must resolve by id desc)", order, want)
		}
	}
}

func TestFeed_LimitCappedAt50(t *testing.T) {
	base := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	fr := newFakePostRepo()
	for i := 0; i < 60; i++ {
		fr.seed(base.Add(time.Duration(i)*time.Minute), 1)
	}
	s := NewFeedService(fr, newFakeUserRepo(), newFakeSocialRepo(), nil, newFakeInteractionRepo())

	page, err := s.List(ctx, 0, "", 100) // 疯狂的 limit
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != MaxPageSize {
		t.Errorf("items = %d, want capped at %d", len(page.Items), MaxPageSize)
	}
	if !page.HasMore {
		t.Error("should have more after cap")
	}
}

func TestFeed_AuthorFilled(t *testing.T) {
	ur := newFakeUserRepo()
	_ = ur.Create(ctx, &model.User{Nickname: "pluto", PasswordHash: "x"})
	plutoID := ur.nextID

	base := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	fr := newFakePostRepo()
	fr.seed(base, plutoID)

	s := NewFeedService(fr, ur, newFakeSocialRepo(), nil, newFakeInteractionRepo())
	page, err := s.List(ctx, 0, "", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Items[0].Author.Nickname != "pluto" {
		t.Errorf("author = %+v, want pluto", page.Items[0].Author)
	}
}

// —— 关注流（M2）——

func TestFollowingFeed_OnlyFollowedUsersPosts(t *testing.T) {
	fx := newInteractionFixture() // 借用现有夹具：fr/ur/sr 一体
	alice := fx.seedUser("alice")
	bob := fx.seedUser("bob")
	carol := fx.seedUser("carol")

	base := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	// bob 发 2 帖、carol 发 1 帖
	fx.fr.seed(base.Add(1*time.Minute), bob)
	fx.fr.seed(base.Add(2*time.Minute), bob)
	fx.fr.seed(base.Add(3*time.Minute), carol)

	// alice 只关注 bob，没关注 carol
	sr := newFakeSocialRepo()
	_ = sr.Follow(ctx, alice, bob)
	fs := NewFeedService(fx.fr, fx.ur, sr, nil, fx.ir)

	page, err := fs.ListFollowingFeed(ctx, alice, "", 10)
	if err != nil {
		t.Fatalf("following feed: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2 (只含 bob 的帖)", len(page.Items))
	}
	for _, it := range page.Items {
		if it.Author.Nickname != "bob" {
			t.Errorf("混入了未关注用户: %s", it.Author.Nickname)
		}
	}
}

func TestFollowingFeed_EmptyFollowListIsEmptyPage(t *testing.T) {
	fx := newInteractionFixture()
	alice := fx.seedUser("alice")
	fx.fr.seed(time.Now(), 999)

	fs := NewFeedService(fx.fr, fx.ur, newFakeSocialRepo(), nil, fx.ir)
	page, err := fs.ListFollowingFeed(ctx, alice, "", 10)
	if err != nil {
		t.Fatalf("empty follow list should not error: %v", err)
	}
	if len(page.Items) != 0 || page.HasMore {
		t.Errorf("want empty page, got %d items hasMore=%v", len(page.Items), page.HasMore)
	}
}

func TestFollowingFeed_OverLimitRejected(t *testing.T) {
	fx := newInteractionFixture()
	alice := fx.seedUser("alice")

	sr := newFakeSocialRepo()
	// 造 5001 个关注对象（超上限）
	for i := int64(100); i <= int64(100)+MaxFollowingFeedIDs; i++ {
		_ = sr.Follow(ctx, alice, i)
	}
	fs := NewFeedService(fx.fr, fx.ur, sr, nil, fx.ir)

	_, err := fs.ListFollowingFeed(ctx, alice, "", 10)
	be, ok := err.(*apierror.BizError)
	if !ok || be.HTTPStatus != 400 {
		t.Errorf("err = %v, want 400 BizError (over-limit)", err)
	}
}

func TestFollowingFeed_CursorPagination(t *testing.T) {
	fx := newInteractionFixture()
	alice := fx.seedUser("alice")
	bob := fx.seedUser("bob")
	fx.sr.Follow(ctx, alice, bob)

	base := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		fx.fr.seed(base.Add(time.Duration(i)*time.Minute), bob)
	}

	// limit=2 翻完三页，不丢不重
	fs := NewFeedService(fx.fr, fx.ur, fx.sr, nil, fx.ir)
	var all []string
	cursor := ""
	for {
		page, err := fs.ListFollowingFeed(ctx, alice, cursor, 2)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, it := range page.Items {
			all = append(all, it.Content)
		}
		if !page.HasMore {
			break
		}
		cursor = page.NextCursor
	}
	if len(all) != 5 {
		t.Fatalf("got %d, want 5", len(all))
	}
	want := []string{"post-5", "post-4", "post-3", "post-2", "post-1"}
	for i := range want {
		if all[i] != want[i] {
			t.Fatalf("order = %v, want %v", all, want)
		}
	}
}

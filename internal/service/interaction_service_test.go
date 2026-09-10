package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/model"
	"pluto_feed/internal/repository"
)

// fakeInteractionRepo 内存假实现（M4 异步化语义）。
// 与真实 repo 对齐：写路径不再同步改计数，而是往 outbox 发事件行。
// 计数更新是 Worker 的事——单测只验证"事件发得对不对"。
type fakeInteractionRepo struct {
	likes       map[int64]map[int64]bool // postID → set(uid)
	comments    map[int64]model.Comment
	nextID      int64
	nextEventID uint64         // outbox 事件 id（消费幂等键）
	outbox      []model.Outbox // 事件箱：按顺序累积
	clockNano   int64          // 人造时钟：让每条评论的 created_at 严格递增
}

func newFakeInteractionRepo() *fakeInteractionRepo {
	return &fakeInteractionRepo{
		likes:    map[int64]map[int64]bool{},
		comments: map[int64]model.Comment{},
	}
}

// emit 模拟同事务写 outbox：业务成功才调用（真实 repo 里 RowsAffected==1 才走到这）。
func (f *fakeInteractionRepo) emit(eventType string, e model.CountEvent) {
	payload, _ := json.Marshal(e)
	f.nextEventID++
	f.outbox = append(f.outbox, model.Outbox{
		ID: f.nextEventID, EventType: eventType, Payload: payload,
	})
}

func (f *fakeInteractionRepo) events() []model.CountEvent {
	var out []model.CountEvent
	for _, evt := range f.outbox {
		var e model.CountEvent
		_ = json.Unmarshal(evt.Payload, &e)
		out = append(out, e)
	}
	return out
}

func (f *fakeInteractionRepo) Like(_ context.Context, uid, postID int64) error {
	set, ok := f.likes[postID]
	if !ok {
		set = map[int64]bool{}
		f.likes[postID] = set
	}
	if set[uid] {
		return nil // INSERT IGNORE 命中已存在行：无副作用、无事件
	}
	set[uid] = true
	f.emit(model.EventLikeCreated, model.CountEvent{PostID: postID, Field: "like_count", Delta: 1})
	return nil
}

func (f *fakeInteractionRepo) Unlike(_ context.Context, uid, postID int64) error {
	set, ok := f.likes[postID]
	if !ok {
		return nil
	}
	if !set[uid] {
		return nil // 没点过：无副作用、无事件
	}
	delete(set, uid)
	f.emit(model.EventLikeRemoved, model.CountEvent{PostID: postID, Field: "like_count", Delta: -1})
	return nil
}

func (f *fakeInteractionRepo) CreateComment(_ context.Context, c *model.Comment) error {
	f.nextID++
	f.clockNano++
	c.ID = f.nextID
	c.CreatedAt = time.Unix(0, f.clockNano)
	f.comments[c.ID] = *c
	f.emit(model.EventCommentCreate, model.CountEvent{PostID: c.PostID, Field: "comment_count", Delta: 1})
	return nil
}

func (f *fakeInteractionRepo) GetComment(_ context.Context, id int64) (*model.Comment, error) {
	if c, ok := f.comments[id]; ok {
		return &c, nil
	}
	return nil, nil
}

func (f *fakeInteractionRepo) DeleteComment(_ context.Context, id int64) error {
	c, ok := f.comments[id]
	if !ok {
		return nil // service 已先用 GetComment 做过 404 判断
	}
	delete(f.comments, id)
	f.emit(model.EventCommentDelete, model.CountEvent{PostID: c.PostID, Field: "comment_count", Delta: -1})
	return nil
}

func (f *fakeInteractionRepo) HasLiked(_ context.Context, uid, postID int64) (bool, error) {
	return f.likes[postID][uid], nil
}

func (f *fakeInteractionRepo) LikedPostIDs(_ context.Context, uid int64, postIDs []int64) (map[int64]bool, error) {
	out := make(map[int64]bool, len(postIDs))
	for _, id := range postIDs {
		if f.likes[id][uid] {
			out[id] = true
		}
	}
	return out, nil
}

func (f *fakeInteractionRepo) ListCommentsByCursor(_ context.Context, postID int64, before *repository.PageCursor, limit int) ([]model.Comment, error) {
	var out []model.Comment
	for _, c := range f.comments {
		if c.PostID != postID {
			continue
		}
		if before != nil {
			if !(c.CreatedAt.Before(before.Time) ||
				(c.CreatedAt.Equal(before.Time) && c.ID < before.ID)) {
				continue
			}
		}
		out = append(out, c)
	}
	// 按 (created_at DESC, id DESC) 排序
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			a, b := out[i], out[j]
			if a.CreatedAt.Before(b.CreatedAt) || (a.CreatedAt.Equal(b.CreatedAt) && a.ID > b.ID) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

var _ repository.InteractionRepo = (*fakeInteractionRepo)(nil) // 编译期断言

// —— 测试套件：三个 fake repo 必须是同一套（service 里它们要互相配合）——

type interactionFixture struct {
	svc *InteractionService
	fr  *fakePostRepo
	ur  *fakeUserRepo
	ir  *fakeInteractionRepo
	sr  *fakeSocialRepo // M2：关注流测试复用同一套用户/帖子数据
}

func newInteractionFixture() *interactionFixture {
	fr := newFakePostRepo()
	ur := newFakeUserRepo()
	ir := newFakeInteractionRepo()
	sr := newFakeSocialRepo()
	return &interactionFixture{
		svc: NewInteractionService(fr, ur, ir, nil),
		fr:  fr, ur: ur, ir: ir, sr: sr,
	}
}

// seedUser 建一个用户并返回 uid
func (fx *interactionFixture) seedUser(name string) int64 {
	_ = fx.ur.Create(ctx, &model.User{Nickname: name, PasswordHash: "x"})
	return fx.ur.nextID
}

// seedPost 为某用户发一篇帖子并返回 postID
func (fx *interactionFixture) seedPost(uid int64) int64 {
	p := model.Post{UserID: uid, ImageURLs: model.StringList{"a.jpg"}, Content: "帖子"}
	_ = fx.fr.Create(ctx, &p)
	return p.ID
}

func TestLike_EmitsOneEventPerUser(t *testing.T) {
	fx := newInteractionFixture()
	uid := fx.seedUser("alice")
	postID := fx.seedPost(uid)

	// 同一用户连点三次：只有第一次产生事件（重复点赞不进 outbox）
	for i := 0; i < 3; i++ {
		if err := fx.svc.Like(ctx, uid, postID); err != nil {
			t.Fatalf("like #%d: %v", i, err)
		}
	}
	events := fx.ir.events()
	if len(events) != 1 {
		t.Fatalf("outbox = %d events, want 1 (idempotent!)", len(events))
	}
	e := events[0]
	if e.PostID != postID || e.Field != "like_count" || e.Delta != 1 {
		t.Errorf("event = %+v, want {postID, like_count, +1}", e)
	}
}

func TestLike_NonexistentPost(t *testing.T) {
	fx := newInteractionFixture()
	uid := fx.seedUser("alice")

	if err := fx.svc.Like(ctx, uid, 9999); err != apierror.ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestUnlike_NotLikedEmitsNothing(t *testing.T) {
	fx := newInteractionFixture()
	uid := fx.seedUser("alice")
	postID := fx.seedPost(uid)

	if err := fx.svc.Unlike(ctx, uid, postID); err != nil {
		t.Fatalf("unlike: %v", err)
	}
	if len(fx.ir.outbox) != 0 {
		t.Errorf("outbox = %d events, want 0 (没点过就不该有 like.removed)", len(fx.ir.outbox))
	}
}

func TestLikeUnlike_EmitsCreatedThenRemoved(t *testing.T) {
	fx := newInteractionFixture()
	alice := fx.seedUser("alice")
	bob := fx.seedUser("bob")
	postID := fx.seedPost(alice)

	_ = fx.svc.Like(ctx, alice, postID)
	_ = fx.svc.Like(ctx, bob, postID)
	_ = fx.svc.Unlike(ctx, bob, postID)

	events := fx.ir.events()
	if len(events) != 3 {
		t.Fatalf("outbox = %d events, want 3 (+1,+1,-1)", len(events))
	}
	if events[2].Delta != -1 {
		t.Errorf("third event = %+v, want delta -1", events[2])
	}
}

func TestComment_EmitsEvent(t *testing.T) {
	fx := newInteractionFixture()
	alice := fx.seedUser("alice")
	postID := fx.seedPost(alice)

	view, err := fx.svc.Comment(ctx, alice, postID, "好帖！")
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if view.Author.Nickname != "alice" {
		t.Errorf("author = %q, want alice", view.Author.Nickname)
	}
	events := fx.ir.events()
	if len(events) != 1 || events[0].Field != "comment_count" || events[0].Delta != 1 {
		t.Errorf("events = %+v, want one comment_count +1", events)
	}
}

func TestComment_ValidationAndDeletedPost(t *testing.T) {
	fx := newInteractionFixture()
	alice := fx.seedUser("alice")
	postID := fx.seedPost(alice)

	if _, err := fx.svc.Comment(ctx, alice, postID, ""); err == nil {
		t.Error("empty comment should fail")
	}
	long := make([]rune, 501)
	for i := range long {
		long[i] = '评'
	}
	if _, err := fx.svc.Comment(ctx, alice, postID, string(long)); err == nil {
		t.Error("501-char comment should fail")
	}
	if _, err := fx.svc.Comment(ctx, alice, 9999, "hi"); err != apierror.ErrNotFound {
		t.Errorf("comment on missing post: err = %v, want ErrNotFound", err)
	}
}

func TestDeleteComment_OwnerOnly(t *testing.T) {
	fx := newInteractionFixture()
	alice := fx.seedUser("alice")
	bob := fx.seedUser("bob")
	postID := fx.seedPost(alice)

	cv1, _ := fx.svc.Comment(ctx, alice, postID, "alice 的评论")
	cv2, _ := fx.svc.Comment(ctx, bob, postID, "bob 的评论")
	if got := len(fx.ir.outbox); got != 2 {
		t.Fatalf("outbox = %d, want 2 (两条评论两个事件)", got)
	}

	// bob 删 alice 的评论 → 403，不产生事件
	err := fx.svc.DeleteComment(ctx, bob, cv1.ID)
	if be, ok := err.(*apierror.BizError); !ok || be.HTTPStatus != 403 {
		t.Errorf("err = %v, want 403", err)
	}
	if len(fx.ir.outbox) != 2 {
		t.Errorf("403 后 outbox = %d, want 2（无新事件）", len(fx.ir.outbox))
	}

	// alice 删自己的 → ok，产生 comment.deleted
	if err := fx.svc.DeleteComment(ctx, alice, cv1.ID); err != nil {
		t.Fatalf("delete own: %v", err)
	}
	events := fx.ir.events()
	if events[2].Delta != -1 || events[2].Field != "comment_count" {
		t.Errorf("third event = %+v, want comment_count -1", events[2])
	}

	// 再删一次 → 404
	if err := fx.svc.DeleteComment(ctx, alice, cv1.ID); err != apierror.ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	_ = cv2
}

func TestListComments_CursorPagination(t *testing.T) {
	fx := newInteractionFixture()
	alice := fx.seedUser("alice")
	postID := fx.seedPost(alice)

	// 发 5 条评论
	for i := 0; i < 5; i++ {
		if _, err := fx.svc.Comment(ctx, alice, postID, "评论"); err != nil {
			t.Fatal(err)
		}
	}

	// limit=2 翻完三页，必须不丢不重
	var all []CommentView
	cursor := ""
	for {
		page, err := fx.svc.ListComments(ctx, postID, cursor, 2)
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
		t.Fatalf("got %d comments across pages, want 5", len(all))
	}
	ids := map[int64]bool{}
	for _, c := range all {
		if ids[c.ID] {
			t.Errorf("duplicate comment id %d", c.ID)
		}
		ids[c.ID] = true
	}
	// 新→旧
	if all[0].ID != 5 || all[4].ID != 1 {
		t.Errorf("order wrong: first=%d last=%d, want 5..1", all[0].ID, all[4].ID)
	}
}

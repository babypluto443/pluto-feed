package service

import (
	"testing"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/model"
	"pluto_feed/internal/repository"
)

// —— Create 校验 ——

func TestPostCreate_Validation(t *testing.T) {
	s := NewPostService(newFakePostRepo(), newFakeUserRepo(), nil, nil, nil)

	cases := []struct {
		name     string
		in       CreateInput
		wantCode int
	}{
		{"无图片", CreateInput{Content: "hi"}, 40010},
		{"空正文", CreateInput{ImageURLs: []string{"a.jpg"}}, 40012},
		{"正文超长", CreateInput{ImageURLs: []string{"a.jpg"}, Content: makeString(1001, "字")}, 40013},
		{"标签过多", CreateInput{ImageURLs: []string{"a.jpg"}, Content: "hi", Tags: []string{"1", "2", "3", "4", "5", "6"}}, 40014},
	}
	for _, c := range cases {
		_, err := s.Create(ctx, 1, c.in)
		be, ok := err.(*apierror.BizError)
		if !ok || be.Code != c.wantCode {
			t.Errorf("%s: err = %v, want code %d", c.name, err, c.wantCode)
		}
	}
}

func makeString(n int, s string) string {
	out := make([]rune, 0, n)
	for len(out) < n {
		out = append(out, []rune(s)...)
	}
	return string(out)
}

func TestPostCreate_Success(t *testing.T) {
	fr := newFakePostRepo()
	ur := newFakeUserRepo()
	_ = ur.Create(ctx, &model.User{Nickname: "pluto", PasswordHash: "x"})
	s := NewPostService(fr, ur, nil, nil, nil)

	view, err := s.Create(ctx, ur.nextID, CreateInput{
		ImageURLs: []string{"/uploads/20260907/a.jpg"},
		Content:   "第一篇帖子",
		Tags:      []string{"日常"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if view.Author.Nickname != "pluto" || view.Content != "第一篇帖子" {
		t.Errorf("view = %+v", view)
	}
}

// —— Delete 权限 ——

func TestPostDelete_OwnerCanDelete(t *testing.T) {
	fr := newFakePostRepo()
	s := NewPostService(fr, newFakeUserRepo(), nil, nil, nil)
	view, _ := s.Create(ctx, 1, CreateInput{ImageURLs: []string{"a.jpg"}, Content: "我的帖子"})

	if err := s.Delete(ctx, 1, view.ID); err != nil {
		t.Fatalf("delete by owner: %v", err)
	}
	// 软删除后详情查不到
	if _, err := s.Get(ctx, 0, view.ID); err != apierror.ErrNotFound {
		t.Errorf("get after delete: err = %v, want ErrNotFound", err)
	}
}

func TestPostDelete_NotOwnerForbidden(t *testing.T) {
	fr := newFakePostRepo()
	s := NewPostService(fr, newFakeUserRepo(), nil, nil, nil)
	view, _ := s.Create(ctx, 1, CreateInput{ImageURLs: []string{"a.jpg"}, Content: "我的帖子"})

	// 用户 2 删用户 1 的帖子 → 403
	err := s.Delete(ctx, 2, view.ID)
	if be, ok := err.(*apierror.BizError); !ok || be.HTTPStatus != 403 {
		t.Errorf("err = %v, want 403 Forbidden", err)
	}
	// 帖子还在（软删除没执行）
	if _, err := s.Get(ctx, 0, view.ID); err != nil {
		t.Errorf("post should still exist, got %v", err)
	}
}

func TestPostDelete_NotFound(t *testing.T) {
	s := NewPostService(newFakePostRepo(), newFakeUserRepo(), nil, nil, nil)
	if err := s.Delete(ctx, 1, 9999); err != apierror.ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

var _ repository.PostRepo = (*fakePostRepo)(nil) // 编译期断言：假实现满足接口

package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	cachepkg "pluto_feed/internal/cache"

	"github.com/alicebob/miniredis/v2"

	"golang.org/x/crypto/bcrypt"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/jwtutil"
	"pluto_feed/internal/model"
)

// fakeUserRepo 内存假实现：单元测试不连数据库。
type fakeUserRepo struct {
	users  map[string]*model.User
	nextID int64
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{users: map[string]*model.User{}}
}

func (f *fakeUserRepo) Create(_ context.Context, u *model.User) error {
	f.nextID++
	u.ID = f.nextID
	f.users[u.Nickname] = u
	return nil
}

func (f *fakeUserRepo) GetByNickname(_ context.Context, nickname string) (*model.User, error) {
	if u, ok := f.users[nickname]; ok {
		return u, nil
	}
	return nil, nil
}

func (f *fakeUserRepo) GetByID(_ context.Context, id int64) (*model.User, error) {
	for _, u := range f.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, nil
}

func (f *fakeUserRepo) GetByIDs(_ context.Context, ids []int64) (map[int64]*model.User, error) {
	out := make(map[int64]*model.User, len(ids))
	for _, id := range ids {
		for _, u := range f.users {
			if u.ID == id {
				out[id] = u
				break
			}
		}
	}
	return out, nil
}

func newTestService() *AuthService {
	jm := jwtutil.NewManager("test-secret", 30*time.Minute, 7*24*time.Hour)
	return NewAuthService(newFakeUserRepo(), jm, nil)
}

var ctx = context.Background()

// —— Register ——

func TestRegister_Success(t *testing.T) {
	s := newTestService()
	if err := s.Register(ctx, "pluto", "secret123"); err != nil {
		t.Fatalf("register: %v", err)
	}
}

func TestRegister_PasswordTooShort(t *testing.T) {
	s := newTestService()
	err := s.Register(ctx, "pluto", "123")
	if err == nil {
		t.Fatal("want error for short password, got nil")
	}
	if be, ok := err.(*apierror.BizError); !ok || be.Code != 40002 {
		t.Errorf("err = %v, want code 40002", err)
	}
}

func TestRegister_DuplicateNickname(t *testing.T) {
	s := newTestService()
	_ = s.Register(ctx, "pluto", "secret123")

	err := s.Register(ctx, "pluto", "another123")
	if err == nil {
		t.Fatal("want error for duplicate nickname, got nil")
	}
	if be, ok := err.(*apierror.BizError); !ok || be.Code != 40003 {
		t.Errorf("err = %v, want code 40003", err)
	}
}

func TestRegister_PasswordHashed(t *testing.T) {
	// 密码绝不能明文落库：存的是 bcrypt 哈希
	s := newTestService()
	_ = s.Register(ctx, "pluto", "secret123")

	s2 := s.users.(*fakeUserRepo)
	stored := s2.users["pluto"]
	if stored.PasswordHash == "secret123" {
		t.Fatal("password stored in plaintext!")
	}
	if bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("secret123")) != nil {
		t.Fatal("stored hash does not match original password")
	}
}

// —— Login ——

func TestLogin_Success(t *testing.T) {
	s := newTestService()
	_ = s.Register(ctx, "pluto", "secret123")

	tp, err := s.Login(ctx, "pluto", "secret123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if tp.AccessToken == "" || tp.RefreshToken == "" {
		t.Fatal("tokens should not be empty")
	}
}

func TestLogin_UnifiedErrorForWrongPasswordAndUnknownUser(t *testing.T) {
	s := newTestService()
	_ = s.Register(ctx, "pluto", "secret123")

	// 两种失败必须返回完全相同的错误（防撞库）
	_, errWrongPwd := s.Login(ctx, "pluto", "wrongpwd")
	_, errUnknown := s.Login(ctx, "nobody", "whatever1")

	if errWrongPwd == nil || errUnknown == nil {
		t.Fatal("both should fail")
	}
	beA := errWrongPwd.(*apierror.BizError)
	beB := errUnknown.(*apierror.BizError)
	if beA.Msg != beB.Msg || beA.Code != beB.Code {
		t.Errorf("errors differ: %v vs %v (leaks account existence!)", errWrongPwd, errUnknown)
	}
}

// —— Refresh ——

func TestRefresh_Success(t *testing.T) {
	s := newTestService()
	_ = s.Register(ctx, "pluto", "secret123")
	tp, _ := s.Login(ctx, "pluto", "secret123")

	newPair, err := s.Refresh(ctx, tp.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if newPair.AccessToken == "" {
		t.Fatal("new access token empty")
	}
}

func TestRefresh_RejectsAccessToken(t *testing.T) {
	s := newTestService()
	_ = s.Register(ctx, "pluto", "secret123")
	tp, _ := s.Login(ctx, "pluto", "secret123")

	// 拿 access 冒充 refresh —— 必须拒绝
	if _, err := s.Refresh(ctx, tp.AccessToken); err != apierror.ErrUnauthorized {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

// —— Me ——

func TestMe_ReturnsUserWithoutHash(t *testing.T) {
	s := newTestService()
	_ = s.Register(ctx, "pluto", "secret123")
	tp, _ := s.Login(ctx, "pluto", "secret123")

	// 从 access token 解出 uid，模拟中间件行为
	uid, err := s.jwt.Parse(tp.AccessToken, jwtutil.TypAccess)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	u, err := s.Me(ctx, uid)
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	if u.Nickname != "pluto" {
		t.Errorf("nickname = %q, want pluto", u.Nickname)
	}
	// 真正的安全属性：序列化成 JSON 后看不到密码哈希（model 的 json:"-" 标签）
	b, _ := json.Marshal(u)
	if strings.Contains(string(b), "PasswordHash") || strings.Contains(string(b), "$2a$") {
		t.Fatalf("password hash leaked in JSON: %s", b)
	}
}


// —— Refresh 白名单轮换（O2）：miniredis 驱动真 Redis 语义 ——

func TestRefresh_Rotation(t *testing.T) {
	mr := miniredis.RunT(t)
	cache := cachepkg.NewRedis(mr.Addr())
	jm := jwtutil.NewManager("test-secret", 30*time.Minute, 7*24*time.Hour)
	s := NewAuthService(newFakeUserRepo(), jm, cache)

	// 注册+登录 → 拿到第一对 token
	_ = s.Register(ctx, "rot_user", "secret123")
	pair1, err := s.Login(ctx, "rot_user", "secret123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// 第一次刷新：成功，拿到新对
	pair2, err := s.Refresh(ctx, pair1.RefreshToken)
	if err != nil {
		t.Fatalf("refresh #1: %v", err)
	}

	// 旧 refresh token 再刷：必须被拒（轮换作废/防重放）
	if _, err := s.Refresh(ctx, pair1.RefreshToken); err == nil {
		t.Fatal("old refresh token should be rejected after rotation")
	}

	// logout：新 refresh 作废
	if err := s.Logout(ctx, pair2.RefreshToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := s.Refresh(ctx, pair2.RefreshToken); err == nil {
		t.Fatal("refresh after logout should be rejected")
	}

	// 降级模式（cache=nil）：轮换关闭但不报错
	s2 := NewAuthService(newFakeUserRepo(), jm, nil)
	if err := s2.Register(ctx, "rot_user2", "secret123"); err != nil {
		t.Fatalf("degraded register: %v", err)
	}
	pairA, err := s2.Login(ctx, "rot_user2", "secret123")
	if err != nil {
		t.Fatalf("degraded login: %v", err)
	}
	if _, err := s2.Refresh(ctx, pairA.RefreshToken); err != nil {
		t.Fatalf("degraded refresh: %v", err)
	}
}

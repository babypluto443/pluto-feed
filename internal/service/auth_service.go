package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/cache"
	"pluto_feed/internal/jwtutil"
	"pluto_feed/internal/model"
)

// TokenPair 登录/刷新成功后返回给客户端的凭证。
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"` // access token 有效期（秒）
}

// AuthService 认证业务：注册 / 登录 / 刷新 / 登出 / 查用户。
// cache 可为 nil（降级）：nil 时 refresh 白名单关闭，退化为"轮换但不作废"。
type AuthService struct {
	users UserRepo
	jwt   *jwtutil.Manager
	cache cache.Store
}

// refresh 白名单：存 token 的 SHA-256（不存原文），值为 uid。
// 登录/刷新时登记，刷新时旧键作废（轮换），登出时删除（全端吊销）。
func refreshKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("rt:%s", hex.EncodeToString(sum[:]))
}

// UserRepo 由 repository 包定义接口；service 只依赖接口（依赖倒置）。
type UserRepo interface {
	Create(ctx context.Context, u *model.User) error
	GetByNickname(ctx context.Context, nickname string) (*model.User, error)
	GetByID(ctx context.Context, id int64) (*model.User, error)
	// GetByIDs 批量按 id 查（Feed/评论/关注列表的作者批量填充，防 N+1）
	GetByIDs(ctx context.Context, ids []int64) (map[int64]*model.User, error)
}

func NewAuthService(users UserRepo, jm *jwtutil.Manager, c cache.Store) *AuthService {
	return &AuthService{users: users, jwt: jm, cache: c}
}

// registerRefresh 把 refresh token 登记进白名单（TTL = refresh 有效期）。
func (s *AuthService) registerRefresh(refreshToken string, uid int64) {
	if s.cache == nil {
		return
	}
	ttl := s.jwt.RefreshTTL()
	if err := s.cache.SetJSON(context.Background(), refreshKey(refreshToken), uid, ttl); err != nil {
		log.Printf("[auth] whitelist set: %v（降级：轮换不做旧作废）", err)
	}
}

// revokeRefresh 从白名单删除（轮换/登出时调用）。
func (s *AuthService) revokeRefresh(refreshToken string) {
	if s.cache == nil {
		return
	}
	if err := s.cache.Del(context.Background(), refreshKey(refreshToken)); err != nil {
		log.Printf("[auth] whitelist del: %v", err)
	}
}

// whitelistUID 白名单中的 uid；第二返回值 false 表示键不存在（已被轮换/吊销/过期）。
// 降级模式（cache=nil）原样返回 uid —— 跳过校验，永不误拒。
func (s *AuthService) whitelistUID(refreshToken string, uid int64) (int64, bool) {
	if s.cache == nil {
		return uid, true
	}
	var stored int64
	found, err := s.cache.GetJSON(context.Background(), refreshKey(refreshToken), &stored)
	if err != nil || !found {
		return 0, false
	}
	return stored, true
}

// Register 注册新用户。
func (s *AuthService) Register(ctx context.Context, nickname, password string) error {
	if utf8.RuneCountInString(nickname) < 2 || utf8.RuneCountInString(nickname) > 32 {
		return apierror.New(400, 40001, "昵称长度需在 2~32 个字符之间")
	}
	if len(password) < 6 {
		return apierror.New(400, 40002, "密码至少 6 位")
	}

	// 昵称唯一性：service 层先查一遍（体验友好），DB 唯一索引兜底（并发保险）
	existing, err := s.users.GetByNickname(ctx, nickname)
	if err != nil {
		return err
	}
	if existing != nil {
		return apierror.New(400, 40003, "昵称已被占用")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.users.Create(ctx, &model.User{Nickname: nickname, PasswordHash: string(hash)})
}

// Login 登录：成功返回双 token。
// 安全要点：账号不存在 / 密码错误 → 一律返回同一句"账号或密码错误"，
// 不给攻击者"这个昵称存在"的信息（防撞库）。
func (s *AuthService) Login(ctx context.Context, nickname, password string) (*TokenPair, error) {
	if strings.TrimSpace(nickname) == "" || password == "" {
		return nil, apierror.New(400, 40004, "昵称和密码不能为空")
	}

	u, err := s.users.GetByNickname(ctx, nickname)
	if err != nil {
		return nil, err
	}
	if u == nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, apierror.New(401, 40101, "账号或密码错误")
	}
	pair, err := s.issuePair(u.ID)
	if err != nil {
		return nil, err
	}
	s.registerRefresh(pair.RefreshToken, u.ID) // 白名单登记（O2）
	return pair, nil
}

// Refresh 用 refresh token 换一对新 token。
// Refresh 刷新：白名单校验（轮换/吊销/过期的 token 一律拒绝）→ 旧 token 作废 → 发新对。
// 这就是"轮换"：refresh token 是一次性的，用一次换一对新的。
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	uid, err := s.jwt.Parse(refreshToken, jwtutil.TypRefresh)
	if err != nil {
		return nil, apierror.ErrUnauthorized
	}
	// 白名单校验：miss = 已被轮换/登出/过期（重放与被盗 token 在此被拦）
	wUID, ok := s.whitelistUID(refreshToken, uid)
	if !ok || wUID != uid {
		return nil, apierror.New(401, 40102, "登录已失效，请重新登录")
	}
	pair, err := s.issuePair(uid)
	if err != nil {
		return nil, err
	}
	s.revokeRefresh(refreshToken)          // 旧 token 立即作废（轮换核心）
	s.registerRefresh(pair.RefreshToken, uid) // 新 token 登记
	return pair, nil
}

// Logout 登出：从白名单删除 refresh token —— 丢手机也能远程登出全端。
func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	s.revokeRefresh(refreshToken)
	return nil
}

// Me 查用户信息（不含密码哈希——model 的 json:"-" 已保证不序列化）。
func (s *AuthService) Me(ctx context.Context, uid int64) (*model.User, error) {
	u, err := s.users.GetByID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, apierror.ErrNotFound
	}
	return u, nil
}

func (s *AuthService) issuePair(uid int64) (*TokenPair, error) {
	access, refresh, err := s.jwt.IssuePair(uid)
	if err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.jwt.AccessTTL() / time.Second),
	}, nil
}

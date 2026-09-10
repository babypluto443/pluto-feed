// Package jwtutil 负责 JWT 双 token（access / refresh）的签发与校验。
package jwtutil

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// token 类型常量：写在 claims 的 typ 字段里。
const (
	TypAccess  = "access"
	TypRefresh = "refresh"
)

// 哨兵错误：调用方（中间件/service）据此决定返回 401 的文案。
var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
)

// Claims 自定义声明：uid + typ + 标准声明（exp 等）。
type Claims struct {
	UID int64  `json:"uid"`
	Typ string `json:"typ"`
	jwt.RegisteredClaims
}

// Manager 持有密钥与两个 TTL，所有签发/校验走它。
type Manager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewManager(secret string, accessTTL, refreshTTL time.Duration) *Manager {
	return &Manager{secret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

// AccessTTL 返回 access token 的有效期（秒），供响应里的 expires_in 字段使用。
func (m *Manager) AccessTTL() time.Duration { return m.accessTTL }

// RefreshTTL 返回 refresh token 有效期（白名单 TTL 用）。
func (m *Manager) RefreshTTL() time.Duration { return m.refreshTTL }

// IssuePair 一次性签发双 token：access 短命、refresh 长命。
func (m *Manager) IssuePair(uid int64) (access, refresh string, err error) {
	now := time.Now()
	access, err = m.sign(uid, TypAccess, now.Add(m.accessTTL))
	if err != nil {
		return "", "", fmt.Errorf("issue access token: %w", err)
	}
	refresh, err = m.sign(uid, TypRefresh, now.Add(m.refreshTTL))
	if err != nil {
		return "", "", fmt.Errorf("issue refresh token: %w", err)
	}
	return access, refresh, nil
}

func (m *Manager) sign(uid int64, typ string, expiresAt time.Time) (string, error) {
	claims := Claims{
		UID: uid,
		Typ: typ,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        jti(), // 唯一 ID：exp 秒级精度下同秒签发的 token 也能互相区分（轮换的前提）
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

// jti 生成 16 位随机 hex（crypto/rand，非时间戳——保证唯一性）
func jti() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Parse 校验 token：
//  1. 只接受 HS256 签名（防算法替换攻击）
//  2. 标准库自动校验 exp 是否过期
//  3. typ 必须等于 wantTyp —— 防止 refresh token 冒充 access 使用
//
// 校验通过返回 uid。
func (m *Manager) Parse(tokenStr, wantTyp string) (int64, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (interface{}, error) {
		return m.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return 0, ErrExpiredToken
		}
		return 0, ErrInvalidToken
	}
	if claims.Typ != wantTyp {
		return 0, ErrInvalidToken
	}
	return claims.UID, nil
}

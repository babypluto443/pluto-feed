package jwtutil

import (
	"errors"
	"testing"
	"time"
)

func newTestManager(ttl time.Duration) *Manager {
	return NewManager("test-secret", ttl, 7*24*time.Hour)
}

func TestIssuePair_RoundTrip(t *testing.T) {
	m := newTestManager(30 * time.Minute)

	access, refresh, err := m.IssuePair(42)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	uid, err := m.Parse(access, TypAccess)
	if err != nil || uid != 42 {
		t.Errorf("parse access: uid=%d err=%v, want 42 nil", uid, err)
	}
	uid, err = m.Parse(refresh, TypRefresh)
	if err != nil || uid != 42 {
		t.Errorf("parse refresh: uid=%d err=%v, want 42 nil", uid, err)
	}
}

func TestParse_RefreshCannotActAsAccess(t *testing.T) {
	m := newTestManager(30 * time.Minute)

	_, refresh, _ := m.IssuePair(1)

	// 拿 refresh 冒充 access —— 必须拒绝
	if _, err := m.Parse(refresh, TypAccess); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("err = %v, want ErrInvalidToken", err)
	}
}

func TestParse_ExpiredTokenRejected(t *testing.T) {
	// 负 TTL 立即过期，用于造一个过期 token
	m := newTestManager(-time.Second)

	access, _, err := m.IssuePair(1)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if _, err := m.Parse(access, TypAccess); !errors.Is(err, ErrExpiredToken) {
		t.Errorf("err = %v, want ErrExpiredToken", err)
	}
}

func TestParse_TamperedSecretRejected(t *testing.T) {
	m := newTestManager(30 * time.Minute)
	access, _, _ := m.IssuePair(1)

	// 换一把密钥解析 —— 签名对不上必须拒绝
	other := NewManager("wrong-secret", 30*time.Minute, 7*24*time.Hour)
	if _, err := other.Parse(access, TypAccess); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("err = %v, want ErrInvalidToken", err)
	}
}

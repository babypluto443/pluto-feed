package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 辅助：写一个临时 yaml 文件并返回路径
func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_DefaultsOnly(t *testing.T) {
	t.Setenv("PLUTO_JWT_SECRET", "test-secret")
	t.Setenv("PLUTO_MYSQL_DSN", "root:123456@tcp(127.0.0.1:3307)/pluto_feed")

	c, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Addr != ":8080" {
		t.Errorf("default addr = %q, want :8080", c.Addr)
	}
	if c.JWTAccessTTL != 30*time.Minute {
		t.Errorf("default access ttl = %v, want 30m", c.JWTAccessTTL)
	}
	if c.JWTRefreshTTL != 7*24*time.Hour {
		t.Errorf("default refresh ttl = %v, want 7d", c.JWTRefreshTTL)
	}
}

func TestLoad_YAMLOverridesDefaults(t *testing.T) {
	t.Setenv("PLUTO_JWT_SECRET", "test-secret")
	t.Setenv("PLUTO_MYSQL_DSN", "root:123456@tcp(127.0.0.1:3307)/pluto_feed")

	path := writeTempYAML(t, `
addr: ":9090"
jwt_access_ttl_minutes: 10
jwt_refresh_ttl_days: 1
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Addr != ":9090" {
		t.Errorf("addr = %q, want :9090", c.Addr)
	}
	if c.JWTAccessTTL != 10*time.Minute {
		t.Errorf("access ttl = %v, want 10m", c.JWTAccessTTL)
	}
	if c.JWTRefreshTTL != 24*time.Hour {
		t.Errorf("refresh ttl = %v, want 24h", c.JWTRefreshTTL)
	}
}

func TestLoad_EnvOverridesYAML(t *testing.T) {
	t.Setenv("PLUTO_ADDR", ":7070")
	t.Setenv("PLUTO_JWT_SECRET", "env-secret")
	t.Setenv("PLUTO_MYSQL_DSN", "env-dsn")

	path := writeTempYAML(t, `
addr: ":9090"
jwt_secret: "yaml-secret"
mysql_dsn: "yaml-dsn"
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Addr != ":7070" {
		t.Errorf("addr = %q, want :7070 (env should win)", c.Addr)
	}
	if c.JWTSecret != "env-secret" {
		t.Errorf("jwt secret = %q, want env-secret (env should win)", c.JWTSecret)
	}
	if c.MySQLDSN != "env-dsn" {
		t.Errorf("dsn = %q, want env-dsn (env should win)", c.MySQLDSN)
	}
}

func TestLoad_MissingSecretFails(t *testing.T) {
	// 不设置任何 secret 来源 → 必须 fail-fast
	if _, err := Load(""); err == nil {
		t.Fatal("expected error when jwt secret missing, got nil")
	}
}

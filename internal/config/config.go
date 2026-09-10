package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 应用配置。加载优先级：环境变量 > yaml 文件 > 默认值。
type Config struct {
	Addr          string        // HTTP 监听地址
	MySQLDSN      string        // MySQL 数据源
	JWTSecret     string        // JWT 签名密钥
	JWTAccessTTL  time.Duration // access token 有效期
	JWTRefreshTTL time.Duration // refresh token 有效期
	RedisAddr     string        // Redis 地址（M3 缓存/热榜；为空则缓存层降级直连 DB）
	RabbitURL     string        // RabbitMQ 连接串（M4 异步化；Worker 进程使用）
	RateIPPerMin  int           // 每 IP 每分钟请求上限（0 = 关闭 IP 限流）
	RateUserPerMin int          // 每用户每分钟写操作上限（0 = 关闭用户限流）
}

// yamlConfig 与 config.yaml 的字段一一对应。
type yamlConfig struct {
	Addr             string `yaml:"addr"`
	MySQLDSN         string `yaml:"mysql_dsn"`
	JWTSecret        string `yaml:"jwt_secret"`
	AccessTTLMinutes int    `yaml:"jwt_access_ttl_minutes"`
	RefreshTTLDays   int    `yaml:"jwt_refresh_ttl_days"`
	RedisAddr        string `yaml:"redis_addr"`
	RabbitURL        string `yaml:"rabbit_url"`
	RateIPPerMin     int    `yaml:"rate_ip_per_min"`
	RateUserPerMin   int    `yaml:"rate_user_per_min"`
}

// Load 加载配置。path 为空时跳过 yaml，仅用环境变量与默认值。
func Load(path string) (*Config, error) {
	c := &Config{
		Addr:          ":8080",
		JWTAccessTTL:  30 * time.Minute,
		JWTRefreshTTL: 7 * 24 * time.Hour,
		RedisAddr:     "127.0.0.1:6380",                // 自建容器映射端口；空串 = 关闭缓存直连 DB
		RabbitURL:     "amqp://guest:guest@127.0.0.1:5673/", // 自建容器映射端口（M4）
		RateIPPerMin:  300, // IP 层：全站每分钟
		RateUserPerMin: 60, // 用户层：敏感写操作每分钟
	}

	// yaml 是可选的：容器部署用纯环境变量（12-Factor），没有 config.yaml 是合法状态。
	// 但"文件存在却解析失败"必须 fail-fast——那是配置写错了，静默降级会掩盖问题。
	var yc yamlConfig
	if path != "" {
		if _, statErr := os.Stat(path); statErr == nil {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read config file: %w", err)
			}
			if err := yaml.Unmarshal(data, &yc); err != nil {
				return nil, fmt.Errorf("parse config file: %w", err)
			}
		}
	}

	// 第二层：yaml 覆盖默认值
	if yc.Addr != "" {
		c.Addr = yc.Addr
	}
	if yc.MySQLDSN != "" {
		c.MySQLDSN = yc.MySQLDSN
	}
	if yc.JWTSecret != "" {
		c.JWTSecret = yc.JWTSecret
	}
	if yc.AccessTTLMinutes > 0 {
		c.JWTAccessTTL = time.Duration(yc.AccessTTLMinutes) * time.Minute
	}
	if yc.RefreshTTLDays > 0 {
		c.JWTRefreshTTL = time.Duration(yc.RefreshTTLDays) * 24 * time.Hour
	}
	if yc.RedisAddr != "" {
		c.RedisAddr = yc.RedisAddr
	}
	if yc.RabbitURL != "" {
		c.RabbitURL = yc.RabbitURL
	}
	if yc.RateIPPerMin > 0 {
		c.RateIPPerMin = yc.RateIPPerMin
	}
	if yc.RateUserPerMin > 0 {
		c.RateUserPerMin = yc.RateUserPerMin
	}

	// 第三层：环境变量覆盖 yaml（最高优先级）
	if v := os.Getenv("PLUTO_ADDR"); v != "" {
		c.Addr = v
	}
	if v := os.Getenv("PLUTO_MYSQL_DSN"); v != "" {
		c.MySQLDSN = v
	}
	if v := os.Getenv("PLUTO_JWT_SECRET"); v != "" {
		c.JWTSecret = v
	}
	if v := os.Getenv("PLUTO_REDIS_ADDR"); v != "" {
		c.RedisAddr = v
	}
	if v := os.Getenv("PLUTO_RABBIT_URL"); v != "" {
		c.RabbitURL = v
	}

	// 必填项校验：JWTSecret 没有任何来源就直接启动失败（fail-fast）
	if c.JWTSecret == "" {
		return nil, errors.New("config: jwt secret is required (set jwt_secret in yaml or PLUTO_JWT_SECRET)")
	}
	if c.MySQLDSN == "" {
		return nil, errors.New("config: mysql dsn is required (set mysql_dsn in yaml or PLUTO_MYSQL_DSN)")
	}
	return c, nil
}

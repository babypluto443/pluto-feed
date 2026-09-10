// Package cache 缓存层：Redis 封装（对齐原项目 middleware/redis 的职责）。
//
// 设计原则：
//   - Store 接口：service 依赖抽象，单测用假实现/生产用 RedisCache
//   - 缓存错误永不拖垮主链路：调用方把缓存错误降级为"当作未命中"
//   - JSON 序列化：值可读、跨服务通用；性能敏感场景再换 msgpack（YAGNI）
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// HotItem 热榜条目：帖子 id + 热度分。
type HotItem struct {
	PostID int64   `json:"post_id"`
	Score  float64 `json:"score"`
}

// Store 缓存抽象。
type Store interface {
	// GetJSON 读缓存。返回 (命中?, 错误)；未命中或错误都不该阻塞主链路。
	GetJSON(ctx context.Context, key string, dest any) (bool, error)
	// SetJSON 写缓存，ttl 到期自动删除（TTL 是缓存自愈的兜底）。
	SetJSON(ctx context.Context, key string, val any, ttl time.Duration) error
	// Del 失效缓存（写路径的 Cache Aside 配套动作）。
	Del(ctx context.Context, keys ...string) error
	// ZIncrBy 热度计数（分钟桶，见 HotTopN）。
	ZIncrBy(ctx context.Context, key, member string, incr float64) error
	// HotTopN 聚合最近 windowMinutes 个分钟桶，取热度前 N。
	HotTopN(ctx context.Context, windowMinutes, limit int) ([]HotItem, error)
}

// RedisCache Store 的 Redis 实现。
type RedisCache struct {
	rdb *redis.Client
}

// NewRedis 连接 Redis。连接失败不在此时报错——缓存层的设计就是"挂了也降级"，
// 真正用的时候才会发现；由调用方把错误当未命中处理。
func NewRedis(addr string) *RedisCache {
	return &RedisCache{
		rdb: redis.NewClient(&redis.Options{
			Addr:        addr,
			DialTimeout: 2 * time.Second, // 快速失败，别让请求卡在拨号上
			ReadTimeout: 500 * time.Millisecond,
		}),
	}
}

// Client 暴露底层连接（限流中间件需要 INCR/ZSET 等原始命令）。
func (c *RedisCache) Client() *redis.Client { return c.rdb }

func (c *RedisCache) GetJSON(ctx context.Context, key string, dest any) (bool, error) {
	b, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return false, nil // 明确未命中
	}
	if err != nil {
		return false, err // Redis 故障：调用方降级
	}
	if err := json.Unmarshal(b, dest); err != nil {
		return false, fmt.Errorf("cache unmarshal %s: %w", key, err)
	}
	return true, nil
}

func (c *RedisCache) SetJSON(ctx context.Context, key string, val any, ttl time.Duration) error {
	b, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("cache marshal %s: %w", key, err)
	}
	return c.rdb.Set(ctx, key, b, ttl).Err()
}

func (c *RedisCache) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return c.rdb.Del(ctx, keys...).Err()
}

// ZIncrBy 热度自增。key 是分钟桶（如 hot:202609091425）：
// 原项目热榜的"分钟桶"思想——热点分散到多个 key，避免单个大 key 成为写入热点。
func (c *RedisCache) ZIncrBy(ctx context.Context, key, member string, incr float64) error {
	if err := c.rdb.ZIncrBy(ctx, key, incr, member).Err(); err != nil {
		return err
	}
	// 桶只保留 11 分钟：热榜窗口 10 分钟 + 1 分钟缓冲，过期自动清理
	return c.rdb.Expire(ctx, key, 11*time.Minute).Err()
}

// HotTopN 聚合最近 windowMinutes 个分钟桶，返回热度前 N。
// 教学取舍：桶数少（60 个封顶），Go 侧归并比 ZUNIONSTORE 更直观且省一次临时 key；
// 桶数大时再换 ZUNIONSTORE（原项目同款），此处 YAGNI。
func (c *RedisCache) HotTopN(ctx context.Context, windowMinutes, limit int) ([]HotItem, error) {
	acc := make(map[int64]float64)
	now := time.Now()
	for i := 0; i < windowMinutes; i++ {
		bucket := now.Add(-time.Duration(i) * time.Minute).Format("200601021504")
		key := "hot:" + bucket
		members, err := c.rdb.ZRevRangeWithScores(ctx, key, 0, -1).Result()
		if err != nil {
			// 单桶失败不中断（可能刚过期），继续聚合其他桶
			continue
		}
		for _, m := range members {
			id, err := strconv.ParseInt(m.Member.(string), 10, 64)
			if err != nil {
				continue
			}
			acc[id] += m.Score
		}
	}

	items := make([]HotItem, 0, len(acc))
	for id, score := range acc {
		items = append(items, HotItem{PostID: id, Score: score})
	}
	// 分数降序；同分按 id 降序保证稳定
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].PostID > items[j].PostID
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// ---------- 常用 key 构造（集中一处，避免散落字符串拼接） ----------

func PostKey(id int64) string  { return fmt.Sprintf("post:%d", id) }
func UserKey(id int64) string  { return fmt.Sprintf("user:%d", id) }
func HotBucket(t time.Time) string { return "hot:" + t.Format("200601021504") }
func HotResultKey(limit int) string { return fmt.Sprintf("hot:result:%d", limit) }

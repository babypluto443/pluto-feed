package middleware

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"pluto_feed/internal/apierror"
)

// 限流的两个设计原则：
//  1. 限流是保护措施，不能变成单点故障——Redis 出错一律放行
//  2. 双层策略（D-O1）：IP 层拦全站滥用（固定窗口，便宜），
//     用户层拦敏感写操作刷量（滑动窗口，精确）
var errRateLimited = apierror.New(429, 42900, "请求过于频繁，请稍后再试")

// nowFloat 时钟可注入（单测模拟时间流逝用），生产走真实时间。
var nowFloat = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// RateLimitIP 每 IP 固定窗口限流（默认粒度：每分钟）。
// INCR + EXPIRE 两步：首次计数时才设置过期，键天然按分钟窗清理。
func RateLimitIP(rdb *redis.Client, limit int) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rdb == nil {
			c.Next()
			return
		}
		ctx := c.Request.Context()
		key := fmt.Sprintf("rl:ip:%s:%d", c.ClientIP(), time.Now().Unix()/60)

		n, err := rdb.Incr(ctx, key).Result()
		if err != nil {
			c.Next() // Redis 故障：放行（原则 1）
			return
		}
		if n == 1 {
			rdb.Expire(ctx, key, 90*time.Second) // 90s > 窗口 60s，防边界提前清键
		}
		if n > int64(limit) {
			apierror.Fail(c.Writer, errRateLimited)
			c.Abort()
			return
		}
		c.Next()
	}
}

// RateLimitUser 敏感写操作的每用户滑动窗口限流。
// ZSET 记录时间戳：清窗 → 计数 → 超限拒绝（本次不写入）→ 设置过期。
// 必须挂在 GinAuth 之后（依赖 context 里的 uid）。
func RateLimitUser(rdb *redis.Client, uidFetcher func(c *gin.Context) (int64, bool), limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, ok := uidFetcher(c)
		if !ok || rdb == nil {
			c.Next() // 未登录（理论不会走到）或 Redis 缺席：放行
			return
		}
		ctx := c.Request.Context()
		key := fmt.Sprintf("rl:user:%d", uid)
		now := nowFloat()
		member := fmt.Sprintf("%d", time.Now().UnixNano())

		pipe := rdb.TxPipeline()
		pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("(%f", now-window.Seconds())) // 清窗外
		pipe.ZAdd(ctx, key, redis.Z{Score: now, Member: member})
		cnt := pipe.ZCard(ctx, key)
		pipe.Expire(ctx, key, window+time.Minute)
		if _, err := pipe.Exec(ctx); err != nil {
			c.Next() // Redis 故障：放行
			return
		}
		if cnt.Val() > int64(limit) {
			apierror.Fail(c.Writer, errRateLimited)
			c.Abort()
			return
		}
		c.Next()
	}
}

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// 用 httptest + miniredis 打真请求：超限必须 429，窗口滑动后恢复。
func setup(t *testing.T, method string, middleware gin.HandlerFunc) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	handle := func(c *gin.Context) { c.Status(200) }
	switch method {
	case http.MethodPost:
		r.POST("/probe", middleware, handle)
	default:
		r.GET("/probe", middleware, handle)
	}
	return r
}

func TestRateLimitIP_FixedWindow(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	r := setup(t, http.MethodGet, RateLimitIP(rdb, 3)) // 阈值 3

	code := func() int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/probe", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		r.ServeHTTP(w, req)
		return w.Code
	}

	for i := 1; i <= 3; i++ {
		if got := code(); got != 200 {
			t.Fatalf("request #%d = %d, want 200", i, got)
		}
	}
	if got := code(); got != 429 {
		t.Fatalf("request #4 = %d, want 429 (超限)", got)
	}

	// 时间快进一个窗口：恢复放行
	mr.FastForward(2 * time.Minute)
	if got := code(); got != 200 {
		t.Fatalf("next window = %d, want 200", got)
	}
}

func TestRateLimitUser_SlidingWindow(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	uidFrom := func(c *gin.Context) (int64, bool) { return 42, true }
	r := setup(t, http.MethodPost, RateLimitUser(rdb, uidFrom, 3, time.Minute))

	code := func() int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/probe", nil)
		r.ServeHTTP(w, req)
		return w.Code
	}
	for i := 1; i <= 3; i++ {
		if got := code(); got != 200 {
			t.Fatalf("request #%d = %d, want 200", i, got)
		}
	}
	if got := code(); got != 429 {
		t.Fatalf("request #4 = %d, want 429", got)
	}

	// 冻结时钟再快进 70 秒（滑动窗口分数来自真实时间，须注入模拟）
	base := float64(time.Now().UnixNano()) / 1e9
	t.Cleanup(func() { nowFloat = func() float64 { return float64(time.Now().UnixNano()) / 1e9 } })
	nowFloat = func() float64 { return base }
	if got := code(); got != 429 {
		t.Fatalf("frozen clock = %d, want 429", got)
	}
	nowFloat = func() float64 { return base + 70 }
	if got := code(); got != 200 {
		t.Fatalf("after window slide = %d, want 200", got)
	}
}

func TestRateLimit_RedisDownAllowsThrough(t *testing.T) {
	// 连一个不存在的 Redis：限流器必须放行（保护措施不能变单点故障）
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: time.Millisecond})
	r := setup(t, http.MethodGet, RateLimitIP(rdb, 1))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("redis down: code = %d, want 200 (降级放行)", w.Code)
	}
}

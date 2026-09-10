package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// 用 miniredis（内存 Redis）测 RedisCache——不依赖真容器，跑得飞快。
// 原项目同款测试手段。
func newTestCache(t *testing.T) *RedisCache {
	t.Helper()
	mr := miniredis.RunT(t)
	return NewRedis(mr.Addr())
}

func TestGetSetJSON_RoundTrip(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	type demo struct{ Name string }
	want := demo{Name: "pluto"}

	// 未命中
	var got demo
	found, err := c.GetJSON(ctx, "k:1", &got)
	if err != nil || found {
		t.Fatalf("miss: found=%v err=%v", found, err)
	}

	// 写入后命中
	if err := c.SetJSON(ctx, "k:1", want, time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	found, err = c.GetJSON(ctx, "k:1", &got)
	if err != nil || !found || got.Name != "pluto" {
		t.Fatalf("hit: found=%v err=%v got=%+v", found, err, got)
	}
}

func TestSetJSON_TTLExpiry(t *testing.T) {
	mr := miniredis.RunT(t)
	c := NewRedis(mr.Addr())
	ctx := context.Background()

	_ = c.SetJSON(ctx, "k", "v", 300*time.Millisecond)
	mr.FastForward(time.Second) // 快进虚拟时钟，立刻触发过期

	found, _ := c.GetJSON(ctx, "k", new(string))
	if found {
		t.Fatal("key should be expired")
	}
}

func TestDel_Invalidates(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	_ = c.SetJSON(ctx, "a", 1, time.Minute)
	_ = c.SetJSON(ctx, "b", 2, time.Minute)
	if err := c.Del(ctx, "a", "b"); err != nil {
		t.Fatalf("del: %v", err)
	}
	if found, _ := c.GetJSON(ctx, "a", new(int)); found {
		t.Fatal("a should be deleted")
	}
	if found, _ := c.GetJSON(ctx, "b", new(int)); found {
		t.Fatal("b should be deleted")
	}
}

func TestHotTopN_AggregatesBuckets(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()
	now := time.Now()

	// 当前桶：post 1 加 3 分、post 2 加 1 分
	cur := HotBucket(now)
	_ = c.ZIncrBy(ctx, cur, "1", 3)
	_ = c.ZIncrBy(ctx, cur, "2", 1)
	// 上一分钟桶：post 2 加 5 分 → 总分 6，应排第一
	prev := HotBucket(now.Add(-time.Minute))
	_ = c.ZIncrBy(ctx, prev, "2", 5)

	items, err := c.HotTopN(ctx, 10, 2)
	if err != nil {
		t.Fatalf("hot: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].PostID != 2 || items[0].Score != 6 {
		t.Errorf("top1 = %+v, want post 2 score 6", items[0])
	}
	if items[1].PostID != 1 || items[1].Score != 3 {
		t.Errorf("top2 = %+v, want post 1 score 3", items[1])
	}
}

func TestHotTopN_EmptyWhenNoData(t *testing.T) {
	c := newTestCache(t)
	items, err := c.HotTopN(context.Background(), 10, 20)
	if err != nil || len(items) != 0 {
		t.Fatalf("items = %v err = %v, want empty", items, err)
	}
}

func TestZIncrBy_BucketExpires(t *testing.T) {
	mr := miniredis.RunT(t)
	c := NewRedis(mr.Addr())
	ctx := context.Background()

	_ = c.ZIncrBy(ctx, HotBucket(time.Now()), "1", 1)
	if !mr.Exists(HotBucket(time.Now())) {
		t.Fatal("bucket should exist right after incr")
	}
	// 快进 12 分钟：桶应已过期
	mr.FastForward(12 * time.Minute)
	if mr.Exists(HotBucket(time.Now())) {
		t.Fatal("bucket should be expired after 12min")
	}
}

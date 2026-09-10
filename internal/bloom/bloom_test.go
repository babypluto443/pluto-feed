package bloom

import (
	"encoding/binary"
	"testing"
)

func TestBloom_NoFalseNegative(t *testing.T) {
	b, err := New(10000, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	// Add 过的元素绝不能漏报（布隆唯一硬保证）
	var keys [][]byte
	for i := 0; i < 10000; i++ {
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(i))
		b.Add(key)
		keys = append(keys, key)
	}
	for _, key := range keys {
		if !b.Test(key) {
			t.Fatalf("false negative for %v — 布隆不可能漏报，实现有 bug", key)
		}
	}
}

func TestBloom_FalsePositiveRate(t *testing.T) {
	b, _ := New(10000, 0.01)
	for i := 0; i < 10000; i++ {
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(i))
		b.Add(key)
	}
	// 抽测 1 万个不存在的新键，误判率应接近 1%（放宽到 3%）
	fp := 0
	total := 10000
	for i := 10000; i < 10000+total; i++ {
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(i))
		if b.Test(key) {
			fp++
		}
	}
	rate := float64(fp) / float64(total)
	if rate > 0.03 {
		t.Fatalf("false positive rate = %.2f%%, want <= 3%%", rate*100)
	}
	t.Logf("实测误判率 %.2f%%（目标 1%%）", rate*100)
}

func TestBloom_EmptyAlwaysNegative(t *testing.T) {
	b, _ := New(1000, 0.01)
	for i := 0; i < 100; i++ {
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(i))
		if b.Test(key) {
			t.Fatalf("empty bloom said yes for %d", i)
		}
	}
}

func TestBloom_InvalidParams(t *testing.T) {
	if _, err := New(0, 0.01); err == nil {
		t.Error("n=0 should error")
	}
	if _, err := New(100, 1.5); err == nil {
		t.Error("p>=1 should error")
	}
}

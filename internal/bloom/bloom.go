// Package bloom 手写布隆过滤器（O3）：空间高效的"一定不存在"判定器。
//
// 用途：帖子详情接口防穿透——bloom 说"没有"的 id 直接 404，不碰 DB。
// 语义保证：
//   - Test=false ⇒ 一定不存在（只要 Add 过就一定查得到，无漏报）
//   - Test=true  ⇒ 可能不存在（误判率可控，落 DB 兜底即可，正确性无损）
//
// 设计边界（重要，面试可讲）：
// pluto_feed 的 post id 是自增连续整数——这个场景下用 bitset（第 n 位表示 id n）
// 其实严格更优：零误判、零空间浪费、O(1)。我们仍实现标准布隆，因为：
//   1. 真实系统的键往往是非连续的（UUID/雪花 ID/字符串 URL）——bitset 无能为力
//   2. 学会手写布隆 = 掌握可迁移到任何键类型的通用方案
// 若未来改用雪花 ID，本实现零改动可用；若坚持自增 ID，可降级为 bitset（一行换）。
package bloom

import (
	"fmt"
	"hash/fnv"
	"math"
)

// Bloom 布隆过滤器：m 位位数组 + k 个哈希函数。
type Bloom struct {
	bits []byte // 位数组（按字节切）
	m    uint64 // 总位数
	k    uint32 // 哈希函数个数
}

// New 按预期元素数与目标误判率分配空间。
// m = -n·lnP / (ln2)²，k = m/n·ln2（标准公式）。
// 例：n=100 万、p=1% → m≈9.6M bit（1.2MB）、k=7。
func New(expectedItems uint64, fpRate float64) (*Bloom, error) {
	if expectedItems == 0 || fpRate <= 0 || fpRate >= 1 {
		return nil, fmt.Errorf("bloom: invalid params n=%d p=%f", expectedItems, fpRate)
	}
	ln2 := math.Ln2
	m := uint64(math.Ceil(-float64(expectedItems) * math.Log(fpRate) / (ln2 * ln2)))
	k := uint32(math.Round(float64(m) / float64(expectedItems) * ln2))
	if k < 1 {
		k = 1
	}
	return &Bloom{bits: make([]byte, (m+7)/8), m: m, k: k}, nil
}

// Add 写入一个元素。
func (b *Bloom) Add(data []byte) {
	h1, h2 := b.doubleHash(data)
	for i := uint32(0); i < b.k; i++ {
		b.set((h1 + uint64(i)*h2) % b.m)
	}
}

// Test 判断元素"是否可能存在"。false ⇒ 必然不存在。
func (b *Bloom) Test(data []byte) bool {
	h1, h2 := b.doubleHash(data)
	for i := uint32(0); i < b.k; i++ {
		if !b.get((h1 + uint64(i)*h2) % b.m) {
			return false
		}
	}
	return true
}

// doubleHash 双哈希派生 k 个位置（Kirsch-Mitzenmacher 模拟：gᵢ = h₁ + i·h₂）。
// 只需计算两次哈希，比独立计算 k 次便宜。
func (b *Bloom) doubleHash(data []byte) (uint64, uint64) {
	h := fnv.New64a()
	_, _ = h.Write(data)
	h1 := h.Sum64()
	// 第二哈希：加盐重算（避免再分配一个 hasher 对象）
	h.Reset()
	_, _ = h.Write(data)
	_, _ = h.Write([]byte{0x9e}) // 任意盐字节
	h2 := h.Sum64()
	if h2 == 0 { // 防退化：h2=0 时所有 gᵢ 重合，退化为单哈希
		h2 = 1
	}
	return h1 % b.m, h2 % b.m
}

func (b *Bloom) set(bit uint64) { b.bits[bit/8] |= 1 << (bit % 8) }
func (b *Bloom) get(bit uint64) bool { return b.bits[bit/8]&(1<<(bit%8)) != 0 }

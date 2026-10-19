package common

import (
	"sync"
)

// InMemoryConcurrencyCounter 跟踪进程内每个用户的 in-flight 请求数。
// 用于 Redis 未启用时的 fallback 计数路径。该结构只负责增/减/读，TTL 自愈
// 由 Redis 分支负责；进程重启会自然清空。
type InMemoryConcurrencyCounter struct {
	mu     sync.Mutex
	counts map[int]int
}

var defaultConcurrencyCounter = &InMemoryConcurrencyCounter{
	counts: make(map[int]int),
}

// UserConcurrencyCounter 返回进程级并发计数器单例。
func UserConcurrencyCounter() *InMemoryConcurrencyCounter {
	return defaultConcurrencyCounter
}

// Incr 自增并返回自增后的值。
func (c *InMemoryConcurrencyCounter) Incr(userId int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[userId]++
	return c.counts[userId]
}

// Decr 自减一；下限为 0，并在清零时删除键，避免 map 无界增长。
func (c *InMemoryConcurrencyCounter) Decr(userId int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.counts[userId]; ok {
		if v <= 1 {
			delete(c.counts, userId)
		} else {
			c.counts[userId] = v - 1
		}
	}
}

// Current 读取当前值（主要用于测试与观测），不持锁对外保证顺序一致性。
func (c *InMemoryConcurrencyCounter) Current(userId int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[userId]
}

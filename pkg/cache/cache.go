// Package cache implements a hierarchical L1/L2 cache system
// that simulates GPU VRAM (L1) and system RAM (L2) constraints.
//
// The cache uses LRU (Least Recently Used) eviction policy:
// - When L1 is full, the LRU entry is demoted to L2
// - When L2 is full, the LRU entry is evicted entirely
package cache

import (
	"container/list"
	"fmt"
	"log"
	"sync"
	"time"
)

// CacheLevel represents where data is stored
type CacheLevel int

const (
	LevelUnknown CacheLevel = iota
	LevelL1                 // Hot cache (GPU VRAM simulation)
	LevelL2                 // Warm cache (RAM simulation)
	LevelMiss               // Not in cache
)

func (l CacheLevel) String() string {
	switch l {
	case LevelL1:
		return "L1 (VRAM)"
	case LevelL2:
		return "L2 (RAM)"
	case LevelMiss:
		return "MISS"
	default:
		return "UNKNOWN"
	}
}

// Message represents a single chat message
type Message struct {
	Content   string
	SenderID  string
	Timestamp time.Time
}

// ChatSession represents a cached chat conversation
type ChatSession struct {
	ChatID       string
	Messages     []Message
	LastAccessed time.Time
	CreatedAt    time.Time
	MessageCount int
}

// cacheEntry wraps a ChatSession with list element reference for LRU
type cacheEntry struct {
	session *ChatSession
	element *list.Element
}

// HierarchicalCache implements a two-level cache with LRU eviction
type HierarchicalCache struct {
	mu sync.RWMutex

	// L1 Cache (hot - simulates GPU VRAM)
	l1Cache    map[string]*cacheEntry
	l1List     *list.List
	l1Capacity int

	// L2 Cache (warm - simulates system RAM)
	l2Cache    map[string]*cacheEntry
	l2List     *list.List
	l2Capacity int

	// Statistics
	stats CacheStats

	// Server ID for logging
	serverID string
}

// CacheStats tracks cache performance metrics
type CacheStats struct {
	TotalRequests int64
	CacheHits     int64
	CacheMisses   int64
	L1Hits        int64
	L2Hits        int64
	Evictions     int64
	Demotions     int64
}

// NewHierarchicalCache creates a new two-level cache
func NewHierarchicalCache(serverID string, l1Capacity, l2Capacity int) *HierarchicalCache {
	return &HierarchicalCache{
		l1Cache:    make(map[string]*cacheEntry),
		l1List:     list.New(),
		l1Capacity: l1Capacity,
		l2Cache:    make(map[string]*cacheEntry),
		l2List:     list.New(),
		l2Capacity: l2Capacity,
		serverID:   serverID,
	}
}

// GetOrCreate retrieves a chat session from cache or creates a new one
// Returns the session and which cache level it was found at
func (c *HierarchicalCache) GetOrCreate(chatID string) (*ChatSession, CacheLevel) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats.TotalRequests++

	// Check L1 first
	if entry, ok := c.l1Cache[chatID]; ok {
		c.stats.CacheHits++
		c.stats.L1Hits++
		entry.session.LastAccessed = time.Now()
		c.l1List.MoveToFront(entry.element)
		return entry.session, LevelL1
	}

	// Check L2
	if entry, ok := c.l2Cache[chatID]; ok {
		c.stats.CacheHits++
		c.stats.L2Hits++
		entry.session.LastAccessed = time.Now()

		// Promote from L2 to L1
		c.promoteToL1(chatID, entry)
		return entry.session, LevelL2
	}

	// Cache miss - create new session
	c.stats.CacheMisses++
	session := &ChatSession{
		ChatID:       chatID,
		Messages:     make([]Message, 0),
		LastAccessed: time.Now(),
		CreatedAt:    time.Now(),
		MessageCount: 0,
	}

	// Add to L1
	c.addToL1(chatID, session)
	return session, LevelMiss
}

// AddMessage adds a message to a chat session
func (c *HierarchicalCache) AddMessage(chatID string, msg Message) (*ChatSession, CacheLevel, error) {
	session, level := c.GetOrCreate(chatID)

	c.mu.Lock()
	defer c.mu.Unlock()

	session.Messages = append(session.Messages, msg)
	session.MessageCount++
	session.LastAccessed = time.Now()

	return session, level, nil
}

// promoteToL1 moves an entry from L2 to L1 (must be called with lock held)
func (c *HierarchicalCache) promoteToL1(chatID string, entry *cacheEntry) {
	// Remove from L2
	c.l2List.Remove(entry.element)
	delete(c.l2Cache, chatID)

	// Add to L1
	c.addToL1(chatID, entry.session)

	log.Printf("[CACHE:%s] Promoted %s from L2 to L1", c.serverID, chatID)
}

// addToL1 adds a session to L1, potentially evicting/demoting existing entries
func (c *HierarchicalCache) addToL1(chatID string, session *ChatSession) {
	// Evict from L1 if at capacity
	for len(c.l1Cache) >= c.l1Capacity {
		c.demoteFromL1()
	}

	// Add to L1
	elem := c.l1List.PushFront(chatID)
	c.l1Cache[chatID] = &cacheEntry{
		session: session,
		element: elem,
	}
}

// demoteFromL1 moves the LRU entry from L1 to L2
func (c *HierarchicalCache) demoteFromL1() {
	// Get LRU entry from L1
	back := c.l1List.Back()
	if back == nil {
		return
	}

	chatID := back.Value.(string)
	entry := c.l1Cache[chatID]

	// Remove from L1
	c.l1List.Remove(back)
	delete(c.l1Cache, chatID)

	c.stats.Demotions++

	// Add to L2
	c.addToL2(chatID, entry.session)

	log.Printf("[CACHE:%s] Demoted %s from L1 to L2", c.serverID, chatID)
}

// addToL2 adds a session to L2, potentially evicting existing entries
func (c *HierarchicalCache) addToL2(chatID string, session *ChatSession) {
	// Evict from L2 if at capacity
	for len(c.l2Cache) >= c.l2Capacity {
		c.evictFromL2()
	}

	// Add to L2
	elem := c.l2List.PushFront(chatID)
	c.l2Cache[chatID] = &cacheEntry{
		session: session,
		element: elem,
	}
}

// evictFromL2 removes the LRU entry from L2 entirely
func (c *HierarchicalCache) evictFromL2() {
	back := c.l2List.Back()
	if back == nil {
		return
	}

	chatID := back.Value.(string)

	c.l2List.Remove(back)
	delete(c.l2Cache, chatID)
	c.stats.Evictions++

	log.Printf("[CACHE:%s] Evicted %s from L2 (to disk - simulated)", c.serverID, chatID)
}

// GetStats returns current cache statistics
func (c *HierarchicalCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

// GetCacheInfo returns detailed cache information
func (c *HierarchicalCache) GetCacheInfo() CacheInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	l1Chats := make([]string, 0, len(c.l1Cache))
	for chatID := range c.l1Cache {
		l1Chats = append(l1Chats, chatID)
	}

	l2Chats := make([]string, 0, len(c.l2Cache))
	for chatID := range c.l2Cache {
		l2Chats = append(l2Chats, chatID)
	}

	return CacheInfo{
		L1Size:     len(c.l1Cache),
		L1Capacity: c.l1Capacity,
		L2Size:     len(c.l2Cache),
		L2Capacity: c.l2Capacity,
		L1Chats:    l1Chats,
		L2Chats:    l2Chats,
		Stats:      c.stats,
	}
}

// CacheInfo contains detailed cache state information
type CacheInfo struct {
	L1Size     int
	L1Capacity int
	L2Size     int
	L2Capacity int
	L1Chats    []string
	L2Chats    []string
	Stats      CacheStats
}

// GetSession retrieves a specific session if it exists
func (c *HierarchicalCache) GetSession(chatID string) (*ChatSession, CacheLevel, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if entry, ok := c.l1Cache[chatID]; ok {
		return entry.session, LevelL1, true
	}
	if entry, ok := c.l2Cache[chatID]; ok {
		return entry.session, LevelL2, true
	}
	return nil, LevelMiss, false
}

// Clear empties both cache levels
func (c *HierarchicalCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.l1Cache = make(map[string]*cacheEntry)
	c.l1List = list.New()
	c.l2Cache = make(map[string]*cacheEntry)
	c.l2List = list.New()

	log.Printf("[CACHE:%s] Cache cleared", c.serverID)
}

// DebugPrint prints cache state for debugging
func (c *HierarchicalCache) DebugPrint() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	fmt.Printf("\n=== Cache State [%s] ===\n", c.serverID)
	fmt.Printf("L1 (%d/%d): ", len(c.l1Cache), c.l1Capacity)
	for e := c.l1List.Front(); e != nil; e = e.Next() {
		fmt.Printf("%s ", e.Value)
	}
	fmt.Println()

	fmt.Printf("L2 (%d/%d): ", len(c.l2Cache), c.l2Capacity)
	for e := c.l2List.Front(); e != nil; e = e.Next() {
		fmt.Printf("%s ", e.Value)
	}
	fmt.Println()

	fmt.Printf("Stats: Hits=%d (L1:%d, L2:%d), Misses=%d, Demotions=%d, Evictions=%d\n",
		c.stats.CacheHits, c.stats.L1Hits, c.stats.L2Hits,
		c.stats.CacheMisses, c.stats.Demotions, c.stats.Evictions)
	fmt.Println("===========================\n")
}

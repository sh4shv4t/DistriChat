package cache

import (
	"fmt"
	"testing"
	"time"
)

func TestNewHierarchicalCache(t *testing.T) {
	cache := NewHierarchicalCache("test", 5, 20)

	if cache == nil {
		t.Fatal("NewHierarchicalCache returned nil")
	}

	info := cache.GetCacheInfo()
	if info.L1Capacity != 5 {
		t.Errorf("Expected L1 capacity 5, got %d", info.L1Capacity)
	}
	if info.L2Capacity != 20 {
		t.Errorf("Expected L2 capacity 20, got %d", info.L2Capacity)
	}
}

func TestGetOrCreate(t *testing.T) {
	cache := NewHierarchicalCache("test", 5, 20)

	// First access should be a miss
	session1, level1 := cache.GetOrCreate("chat-1")
	if level1 != LevelMiss {
		t.Errorf("Expected cache miss, got %v", level1)
	}
	if session1.ChatID != "chat-1" {
		t.Errorf("Expected chat ID 'chat-1', got '%s'", session1.ChatID)
	}

	// Second access should be L1 hit
	session2, level2 := cache.GetOrCreate("chat-1")
	if level2 != LevelL1 {
		t.Errorf("Expected L1 hit, got %v", level2)
	}
	if session1 != session2 {
		t.Error("Expected same session object")
	}
}

func TestAddMessage(t *testing.T) {
	cache := NewHierarchicalCache("test", 5, 20)

	msg := Message{
		Content:   "Hello, world!",
		SenderID:  "user-1",
		Timestamp: time.Now(),
	}

	session, level, err := cache.AddMessage("chat-1", msg)
	if err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}
	if level != LevelMiss {
		t.Errorf("Expected cache miss for new chat, got %v", level)
	}
	if session.MessageCount != 1 {
		t.Errorf("Expected 1 message, got %d", session.MessageCount)
	}
	if len(session.Messages) != 1 {
		t.Errorf("Expected 1 message in slice, got %d", len(session.Messages))
	}
	if session.Messages[0].Content != "Hello, world!" {
		t.Errorf("Expected 'Hello, world!', got '%s'", session.Messages[0].Content)
	}
}

func TestL1Demotion(t *testing.T) {
	cache := NewHierarchicalCache("test", 3, 10)

	// Fill L1 cache
	for i := 0; i < 3; i++ {
		chatID := fmt.Sprintf("chat-%d", i)
		cache.GetOrCreate(chatID)
	}

	info := cache.GetCacheInfo()
	if info.L1Size != 3 {
		t.Errorf("Expected L1 size 3, got %d", info.L1Size)
	}

	// Add one more - should demote oldest to L2
	cache.GetOrCreate("chat-3")

	info = cache.GetCacheInfo()
	if info.L1Size != 3 {
		t.Errorf("Expected L1 size 3 after demotion, got %d", info.L1Size)
	}
	if info.L2Size != 1 {
		t.Errorf("Expected L2 size 1 after demotion, got %d", info.L2Size)
	}
	if info.Stats.Demotions != 1 {
		t.Errorf("Expected 1 demotion, got %d", info.Stats.Demotions)
	}
}

func TestL2Eviction(t *testing.T) {
	cache := NewHierarchicalCache("test", 2, 3)

	// Fill both L1 and L2
	for i := 0; i < 5; i++ {
		chatID := fmt.Sprintf("chat-%d", i)
		cache.GetOrCreate(chatID)
	}

	info := cache.GetCacheInfo()
	if info.L1Size != 2 {
		t.Errorf("Expected L1 size 2, got %d", info.L1Size)
	}
	if info.L2Size != 3 {
		t.Errorf("Expected L2 size 3, got %d", info.L2Size)
	}

	// Add one more - should evict from L2
	cache.GetOrCreate("chat-5")

	info = cache.GetCacheInfo()
	if info.L2Size != 3 {
		t.Errorf("Expected L2 size 3 after eviction, got %d", info.L2Size)
	}
	if info.Stats.Evictions != 1 {
		t.Errorf("Expected 1 eviction, got %d", info.Stats.Evictions)
	}
}

func TestL2Promotion(t *testing.T) {
	cache := NewHierarchicalCache("test", 2, 10)

	// Fill L1
	cache.GetOrCreate("chat-0")
	cache.GetOrCreate("chat-1")

	// Add more to demote chat-0 to L2
	cache.GetOrCreate("chat-2")

	info := cache.GetCacheInfo()

	// chat-0 should be in L2
	found := false
	for _, id := range info.L2Chats {
		if id == "chat-0" {
			found = true
			break
		}
	}
	if !found {
		t.Error("chat-0 should be in L2")
	}

	// Access chat-0 again - should promote to L1
	_, level := cache.GetOrCreate("chat-0")
	if level != LevelL2 {
		t.Errorf("Expected L2 hit, got %v", level)
	}

	info = cache.GetCacheInfo()

	// chat-0 should now be in L1
	found = false
	for _, id := range info.L1Chats {
		if id == "chat-0" {
			found = true
			break
		}
	}
	if !found {
		t.Error("chat-0 should be promoted to L1")
	}
}

func TestCacheStats(t *testing.T) {
	cache := NewHierarchicalCache("test", 5, 20)

	// Initial miss
	cache.GetOrCreate("chat-1")

	// L1 hit
	cache.GetOrCreate("chat-1")

	stats := cache.GetStats()
	if stats.TotalRequests != 2 {
		t.Errorf("Expected 2 total requests, got %d", stats.TotalRequests)
	}
	if stats.CacheMisses != 1 {
		t.Errorf("Expected 1 cache miss, got %d", stats.CacheMisses)
	}
	if stats.CacheHits != 1 {
		t.Errorf("Expected 1 cache hit, got %d", stats.CacheHits)
	}
	if stats.L1Hits != 1 {
		t.Errorf("Expected 1 L1 hit, got %d", stats.L1Hits)
	}
}

func TestClear(t *testing.T) {
	cache := NewHierarchicalCache("test", 5, 20)

	for i := 0; i < 10; i++ {
		cache.GetOrCreate(fmt.Sprintf("chat-%d", i))
	}

	info := cache.GetCacheInfo()
	if info.L1Size+info.L2Size == 0 {
		t.Error("Cache should not be empty before clear")
	}

	cache.Clear()

	info = cache.GetCacheInfo()
	if info.L1Size != 0 || info.L2Size != 0 {
		t.Error("Cache should be empty after clear")
	}
}

func TestGetSession(t *testing.T) {
	cache := NewHierarchicalCache("test", 5, 20)

	// Non-existent session
	_, _, found := cache.GetSession("chat-1")
	if found {
		t.Error("Should not find non-existent session")
	}

	// Create and find
	cache.GetOrCreate("chat-1")
	session, level, found := cache.GetSession("chat-1")
	if !found {
		t.Error("Should find existing session")
	}
	if level != LevelL1 {
		t.Errorf("Expected L1, got %v", level)
	}
	if session.ChatID != "chat-1" {
		t.Errorf("Expected 'chat-1', got '%s'", session.ChatID)
	}
}

func BenchmarkGetOrCreate(b *testing.B) {
	cache := NewHierarchicalCache("test", 5, 20)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chatID := fmt.Sprintf("chat-%d", i%100)
		cache.GetOrCreate(chatID)
	}
}

func BenchmarkAddMessage(b *testing.B) {
	cache := NewHierarchicalCache("test", 5, 20)

	msg := Message{
		Content:   "Test message",
		SenderID:  "user-1",
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chatID := fmt.Sprintf("chat-%d", i%10)
		cache.AddMessage(chatID, msg)
	}
}

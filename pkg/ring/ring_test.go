package ring

import (
	"fmt"
	"sync"
	"testing"
)

func TestNewHashRing(t *testing.T) {
	ring := NewHashRing(100)

	if ring == nil {
		t.Fatal("NewHashRing returned nil")
	}

	if ring.replicas != 100 {
		t.Errorf("Expected replicas to be 100, got %d", ring.replicas)
	}

	if ring.GetNodeCount() != 0 {
		t.Errorf("Expected 0 nodes, got %d", ring.GetNodeCount())
	}
}

func TestAddNode(t *testing.T) {
	ring := NewHashRing(10)

	ring.AddNode("server-a", 10, "localhost:50051")

	if ring.GetNodeCount() != 1 {
		t.Errorf("Expected 1 node, got %d", ring.GetNodeCount())
	}

	if ring.GetVirtualNodeCount() != 10 {
		t.Errorf("Expected 10 virtual nodes, got %d", ring.GetVirtualNodeCount())
	}

	// Adding same node again should not increase count
	ring.AddNode("server-a", 10, "localhost:50051")
	if ring.GetNodeCount() != 1 {
		t.Errorf("Expected 1 node after duplicate add, got %d", ring.GetNodeCount())
	}
}

func TestRemoveNode(t *testing.T) {
	ring := NewHashRing(10)

	ring.AddNode("server-a", 10, "localhost:50051")
	ring.AddNode("server-b", 10, "localhost:50052")

	if ring.GetNodeCount() != 2 {
		t.Fatalf("Expected 2 nodes, got %d", ring.GetNodeCount())
	}

	ring.RemoveNode("server-a")

	if ring.GetNodeCount() != 1 {
		t.Errorf("Expected 1 node after removal, got %d", ring.GetNodeCount())
	}

	if ring.GetVirtualNodeCount() != 10 {
		t.Errorf("Expected 10 virtual nodes, got %d", ring.GetVirtualNodeCount())
	}

	if ring.NodeExists("server-a") {
		t.Error("server-a should not exist after removal")
	}

	if !ring.NodeExists("server-b") {
		t.Error("server-b should still exist")
	}
}

func TestGetNode(t *testing.T) {
	ring := NewHashRing(100)

	ring.AddNode("server-a", 100, "localhost:50051")
	ring.AddNode("server-b", 100, "localhost:50052")
	ring.AddNode("server-c", 100, "localhost:50053")

	// Test that same key always returns same node
	nodeID1, _, ok1 := ring.GetNode("chat-123")
	nodeID2, _, ok2 := ring.GetNode("chat-123")

	if !ok1 || !ok2 {
		t.Fatal("GetNode should return ok=true when nodes exist")
	}

	if nodeID1 != nodeID2 {
		t.Errorf("Same key should return same node, got %s and %s", nodeID1, nodeID2)
	}

	// Test that we can get nodes for various keys
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("chat-%d", i)
		nodeID, addr, ok := ring.GetNode(key)
		if !ok {
			t.Errorf("GetNode failed for key %s", key)
		}
		if nodeID == "" {
			t.Errorf("GetNode returned empty nodeID for key %s", key)
		}
		if addr == "" {
			t.Errorf("GetNode returned empty address for key %s", key)
		}
	}
}

func TestGetNodeEmptyRing(t *testing.T) {
	ring := NewHashRing(10)

	_, _, ok := ring.GetNode("test-key")
	if ok {
		t.Error("GetNode should return ok=false for empty ring")
	}
}

func TestGetNodes(t *testing.T) {
	ring := NewHashRing(10)

	ring.AddNode("server-a", 10, "localhost:50051")
	ring.AddNode("server-b", 10, "localhost:50052")
	ring.AddNode("server-c", 10, "localhost:50053")

	nodes := ring.GetNodes("chat-123", 3)

	if len(nodes) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(nodes))
	}

	// Verify all nodes are distinct
	seen := make(map[string]bool)
	for _, node := range nodes {
		if seen[node.NodeID] {
			t.Errorf("Duplicate node in result: %s", node.NodeID)
		}
		seen[node.NodeID] = true
	}
}

func TestConsistency(t *testing.T) {
	ring := NewHashRing(50)

	ring.AddNode("server-a", 50, "localhost:50051")
	ring.AddNode("server-b", 50, "localhost:50052")

	// Record initial assignments
	assignments := make(map[string]string)
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("chat-%d", i)
		nodeID, _, _ := ring.GetNode(key)
		assignments[key] = nodeID
	}

	// Add a new node
	ring.AddNode("server-c", 50, "localhost:50053")

	// Count how many keys moved
	moved := 0
	for key, oldNode := range assignments {
		newNode, _, _ := ring.GetNode(key)
		if newNode != oldNode {
			moved++
		}
	}

	// With consistent hashing, roughly 1/3 of keys should move
	// Allow some variance
	maxExpectedMoves := 50 // 50% is generous tolerance
	if moved > maxExpectedMoves {
		t.Errorf("Too many keys moved after adding node: %d (expected < %d)", moved, maxExpectedMoves)
	}

	t.Logf("Keys moved after adding node: %d/100", moved)
}

func TestConcurrency(t *testing.T) {
	ring := NewHashRing(50)

	ring.AddNode("server-a", 50, "localhost:50051")
	ring.AddNode("server-b", 50, "localhost:50052")

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := fmt.Sprintf("chat-%d", idx)
			_, _, ok := ring.GetNode(key)
			if !ok {
				errors <- fmt.Errorf("GetNode failed for %s", key)
			}
		}(i)
	}

	// Concurrent writes
	wg.Add(1)
	go func() {
		defer wg.Done()
		ring.AddNode("server-c", 50, "localhost:50053")
	}()

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

func TestLoadDistribution(t *testing.T) {
	ring := NewHashRing(100)

	ring.AddNode("server-a", 100, "localhost:50051")
	ring.AddNode("server-b", 100, "localhost:50052")
	ring.AddNode("server-c", 100, "localhost:50053")

	distribution := make(map[string]int)

	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("chat-%d", i)
		nodeID, _, _ := ring.GetNode(key)
		distribution[nodeID]++
	}

	// With 100 virtual nodes per server, distribution should be roughly equal
	// Allow 20% variance
	expected := 10000 / 3
	tolerance := expected * 20 / 100

	for nodeID, count := range distribution {
		diff := count - expected
		if diff < 0 {
			diff = -diff
		}
		if diff > tolerance {
			t.Errorf("Node %s has uneven distribution: %d (expected ~%d ± %d)", 
				nodeID, count, expected, tolerance)
		}
		t.Logf("Node %s: %d keys (%.1f%%)", nodeID, count, float64(count)/100)
	}
}

func BenchmarkGetNode(b *testing.B) {
	ring := NewHashRing(100)

	ring.AddNode("server-a", 100, "localhost:50051")
	ring.AddNode("server-b", 100, "localhost:50052")
	ring.AddNode("server-c", 100, "localhost:50053")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("chat-%d", i)
		ring.GetNode(key)
	}
}

func BenchmarkAddNode(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ring := NewHashRing(100)
		ring.AddNode("server-a", 100, "localhost:50051")
	}
}

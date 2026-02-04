// Package ring implements a consistent hash ring with virtual nodes
// for distributed load balancing across a cluster of servers.
//
// Consistent hashing ensures that when nodes are added or removed,
// only a minimal number of keys need to be remapped, providing
// stability in distributed systems.
package ring

import (
	"fmt"
	"hash/crc32"
	"log"
	"sort"
	"sync"
)

// VirtualNode represents a single point on the hash ring
type VirtualNode struct {
	Hash     uint32 // The hash value position on the ring
	NodeID   string // The physical node this virtual node belongs to
	VNodeIdx int    // The virtual node index (e.g., 0, 1, 2, ...)
}

// HashRing implements a consistent hash ring with virtual nodes
// for load distribution across a cluster of servers.
type HashRing struct {
	mu           sync.RWMutex
	nodes        []VirtualNode        // Sorted list of virtual nodes
	nodeCapacity map[string]int       // Physical node -> capacity (number of virtual nodes)
	nodeAddress  map[string]string    // Physical node -> network address
	replicas     int                  // Default number of virtual nodes per physical node
}

// NewHashRing creates a new consistent hash ring.
// The replicas parameter sets the default number of virtual nodes per physical node.
// More virtual nodes = better load distribution but more memory usage.
func NewHashRing(replicas int) *HashRing {
	if replicas < 1 {
		replicas = 100 // Default to 100 virtual nodes
	}
	return &HashRing{
		nodes:        make([]VirtualNode, 0),
		nodeCapacity: make(map[string]int),
		nodeAddress:  make(map[string]string),
		replicas:     replicas,
	}
}

// hashKey generates a consistent hash for a given key using CRC32
// This provides fast, deterministic hashing suitable for consistent hashing.
func hashKey(key string) uint32 {
	return crc32.ChecksumIEEE([]byte(key))
}

// virtualNodeKey generates a unique key for a virtual node
func virtualNodeKey(nodeID string, vNodeIdx int) string {
	return fmt.Sprintf("%s#%d", nodeID, vNodeIdx)
}

// AddNode adds a physical node to the hash ring with a specified capacity.
// The capacity determines how many virtual nodes this server gets.
// Higher capacity servers should get more virtual nodes to handle more load.
func (hr *HashRing) AddNode(nodeID string, capacity int, address string) {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	// Check if node already exists
	if _, exists := hr.nodeCapacity[nodeID]; exists {
		log.Printf("[RING] Node %s already exists, skipping...", nodeID)
		return
	}

	if capacity < 1 {
		capacity = hr.replicas
	}

	hr.nodeCapacity[nodeID] = capacity
	hr.nodeAddress[nodeID] = address

	// Create virtual nodes
	for i := 0; i < capacity; i++ {
		vNodeKey := virtualNodeKey(nodeID, i)
		hash := hashKey(vNodeKey)

		vNode := VirtualNode{
			Hash:     hash,
			NodeID:   nodeID,
			VNodeIdx: i,
		}
		hr.nodes = append(hr.nodes, vNode)
	}

	// Sort nodes by hash value for binary search
	sort.Slice(hr.nodes, func(i, j int) bool {
		return hr.nodes[i].Hash < hr.nodes[j].Hash
	})

	log.Printf("[RING] Added node %s with %d virtual nodes at %s", nodeID, capacity, address)
}

// RemoveNode removes a physical node and all its virtual nodes from the ring.
// Returns the list of chat IDs that need to be rebalanced.
func (hr *HashRing) RemoveNode(nodeID string) {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	if _, exists := hr.nodeCapacity[nodeID]; !exists {
		log.Printf("[RING] Node %s not found, nothing to remove", nodeID)
		return
	}

	// Filter out all virtual nodes belonging to this physical node
	newNodes := make([]VirtualNode, 0, len(hr.nodes))
	removedCount := 0
	for _, vNode := range hr.nodes {
		if vNode.NodeID != nodeID {
			newNodes = append(newNodes, vNode)
		} else {
			removedCount++
		}
	}

	hr.nodes = newNodes
	delete(hr.nodeCapacity, nodeID)
	delete(hr.nodeAddress, nodeID)

	log.Printf("[RING] Removed node %s (%d virtual nodes removed). Keys rebalanced.", nodeID, removedCount)
}

// GetNode finds the physical node responsible for a given key.
// Uses binary search for O(log N) lookup performance.
// Returns the node ID and its network address.
func (hr *HashRing) GetNode(key string) (nodeID string, address string, ok bool) {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	if len(hr.nodes) == 0 {
		return "", "", false
	}

	hash := hashKey(key)

	// Binary search for the first node with hash >= key hash
	idx := sort.Search(len(hr.nodes), func(i int) bool {
		return hr.nodes[i].Hash >= hash
	})

	// Wrap around to the beginning if we've gone past the end
	if idx >= len(hr.nodes) {
		idx = 0
	}

	node := hr.nodes[idx]
	return node.NodeID, hr.nodeAddress[node.NodeID], true
}

// GetNodes returns an ordered list of distinct physical nodes starting from
// the node responsible for the key. This is used for failover - if the
// primary node is down, try the next one, and so on.
func (hr *HashRing) GetNodes(key string, count int) []NodeInfo {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	if len(hr.nodes) == 0 {
		return nil
	}

	hash := hashKey(key)

	// Find starting position
	startIdx := sort.Search(len(hr.nodes), func(i int) bool {
		return hr.nodes[i].Hash >= hash
	})

	if startIdx >= len(hr.nodes) {
		startIdx = 0
	}

	// Collect distinct physical nodes
	seen := make(map[string]bool)
	result := make([]NodeInfo, 0, count)

	for i := 0; i < len(hr.nodes) && len(result) < count; i++ {
		idx := (startIdx + i) % len(hr.nodes)
		nodeID := hr.nodes[idx].NodeID

		if !seen[nodeID] {
			seen[nodeID] = true
			result = append(result, NodeInfo{
				NodeID:  nodeID,
				Address: hr.nodeAddress[nodeID],
			})
		}
	}

	return result
}

// NodeInfo contains information about a physical node
type NodeInfo struct {
	NodeID  string
	Address string
}

// GetNodeCount returns the number of physical nodes in the ring
func (hr *HashRing) GetNodeCount() int {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	return len(hr.nodeCapacity)
}

// GetVirtualNodeCount returns the total number of virtual nodes in the ring
func (hr *HashRing) GetVirtualNodeCount() int {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	return len(hr.nodes)
}

// GetNodeCapacity returns the capacity (virtual nodes) for a specific node
func (hr *HashRing) GetNodeCapacity(nodeID string) (int, bool) {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	cap, ok := hr.nodeCapacity[nodeID]
	return cap, ok
}

// GetAllNodes returns all physical node IDs in the ring
func (hr *HashRing) GetAllNodes() []string {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	nodes := make([]string, 0, len(hr.nodeCapacity))
	for nodeID := range hr.nodeCapacity {
		nodes = append(nodes, nodeID)
	}
	return nodes
}

// NodeExists checks if a physical node exists in the ring
func (hr *HashRing) NodeExists(nodeID string) bool {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	_, exists := hr.nodeCapacity[nodeID]
	return exists
}

// GetNodeAddress returns the network address for a given node ID
func (hr *HashRing) GetNodeAddress(nodeID string) (string, bool) {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	addr, ok := hr.nodeAddress[nodeID]
	return addr, ok
}

// DebugPrint prints the current state of the hash ring for debugging
func (hr *HashRing) DebugPrint() {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	fmt.Println("\n=== Hash Ring State ===")
	fmt.Printf("Physical Nodes: %d\n", len(hr.nodeCapacity))
	fmt.Printf("Virtual Nodes: %d\n", len(hr.nodes))

	for nodeID, capacity := range hr.nodeCapacity {
		fmt.Printf("  - %s: %d virtual nodes @ %s\n", nodeID, capacity, hr.nodeAddress[nodeID])
	}

	if len(hr.nodes) <= 20 {
		fmt.Println("\nVirtual Node Distribution:")
		for _, vNode := range hr.nodes {
			fmt.Printf("  Hash: %10d -> %s#%d\n", vNode.Hash, vNode.NodeID, vNode.VNodeIdx)
		}
	}
	fmt.Println("========================\n")
}

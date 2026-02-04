// Package client implements a smart client that uses consistent hashing
// to route chat messages to the appropriate server, with automatic
// failover when the primary server is unavailable.
package client

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/distribchat/pkg/ring"
	pb "github.com/distribchat/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// SmartClient routes chat messages using consistent hashing with failover support
type SmartClient struct {
	mu sync.RWMutex

	// Hash ring for routing decisions
	ring *ring.HashRing

	// Connection pool - maps server address to gRPC client
	connections map[string]*serverConnection

	// Configuration
	config ClientConfig

	// Statistics
	stats ClientStats
}

// serverConnection represents a connection to a single server
type serverConnection struct {
	address string
	conn    *grpc.ClientConn
	client  pb.ChatServiceClient
	healthy bool
}

// ClientConfig contains configuration for the smart client
type ClientConfig struct {
	// Number of virtual nodes per server (default: 100)
	VirtualNodes int

	// Maximum retry attempts for failover (default: 3)
	MaxRetries int

	// Connection timeout (default: 5 seconds)
	ConnectTimeout time.Duration

	// Request timeout (default: 10 seconds)
	RequestTimeout time.Duration
}

// DefaultClientConfig returns sensible default configuration
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		VirtualNodes:   100,
		MaxRetries:     3,
		ConnectTimeout: 5 * time.Second,
		RequestTimeout: 10 * time.Second,
	}
}

// ClientStats tracks client routing statistics
type ClientStats struct {
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	FailoverCount   int64
	PrimaryHits     int64
}

// NewSmartClient creates a new smart client with consistent hash routing
func NewSmartClient(config ClientConfig) *SmartClient {
	if config.VirtualNodes <= 0 {
		config.VirtualNodes = 100
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}
	if config.ConnectTimeout <= 0 {
		config.ConnectTimeout = 5 * time.Second
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 10 * time.Second
	}

	return &SmartClient{
		ring:        ring.NewHashRing(config.VirtualNodes),
		connections: make(map[string]*serverConnection),
		config:      config,
	}
}

// AddServer adds a server to the client's routing table
func (c *SmartClient) AddServer(serverID string, address string, capacity int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Add to hash ring
	c.ring.AddNode(serverID, capacity, address)

	// Establish connection
	conn, err := c.connectToServer(address)
	if err != nil {
		log.Printf("[CLIENT] Warning: Could not connect to %s at %s: %v", serverID, address, err)
		// Still add to ring, connection will be retried later
		c.connections[address] = &serverConnection{
			address: address,
			healthy: false,
		}
		return nil
	}

	c.connections[address] = &serverConnection{
		address: address,
		conn:    conn,
		client:  pb.NewChatServiceClient(conn),
		healthy: true,
	}

	log.Printf("[CLIENT] Added server %s at %s (capacity: %d)", serverID, address, capacity)
	return nil
}

// RemoveServer removes a server from the routing table
func (c *SmartClient) RemoveServer(serverID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Get address before removing from ring
	addr, ok := c.ring.GetNodeAddress(serverID)
	if ok {
		// Close and remove connection
		if conn, exists := c.connections[addr]; exists {
			if conn.conn != nil {
				conn.conn.Close()
			}
			delete(c.connections, addr)
		}
	}

	// Remove from ring
	c.ring.RemoveNode(serverID)
	log.Printf("[CLIENT] Removed server %s", serverID)
}

// MarkServerDown marks a server as unhealthy (for simulation)
func (c *SmartClient) MarkServerDown(serverID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	addr, ok := c.ring.GetNodeAddress(serverID)
	if !ok {
		return
	}

	if conn, exists := c.connections[addr]; exists {
		conn.healthy = false
		log.Printf("[CLIENT] Marked server %s as DOWN", serverID)
	}
}

// MarkServerUp marks a server as healthy
func (c *SmartClient) MarkServerUp(serverID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	addr, ok := c.ring.GetNodeAddress(serverID)
	if !ok {
		return
	}

	if conn, exists := c.connections[addr]; exists {
		conn.healthy = true
		log.Printf("[CLIENT] Marked server %s as UP", serverID)
	}
}

// SendMessage routes a chat message to the appropriate server with failover
func (c *SmartClient) SendMessage(chatID, senderID, message string) (*pb.ChatResponse, error) {
	c.mu.Lock()
	c.stats.TotalRequests++
	c.mu.Unlock()

	// Get ordered list of servers for this chat ID (for failover)
	nodes := c.ring.GetNodes(chatID, c.config.MaxRetries)
	if len(nodes) == 0 {
		c.mu.Lock()
		c.stats.FailedRequests++
		c.mu.Unlock()
		return nil, fmt.Errorf("no servers available")
	}

	// Create the request
	req := &pb.ChatRequest{
		ChatId:    chatID,
		Message:   message,
		SenderId:  senderID,
		Timestamp: time.Now().Unix(),
	}

	// Try primary server first, then failover to subsequent servers
	var lastErr error
	for i, node := range nodes {
		log.Printf("[CLIENT] Routing %s to Server %s (attempt %d/%d)",
			chatID, node.NodeID, i+1, len(nodes))

		resp, err := c.sendToServer(node.Address, req)
		if err == nil && resp.Success {
			c.mu.Lock()
			c.stats.SuccessRequests++
			if i == 0 {
				c.stats.PrimaryHits++
			} else {
				c.stats.FailoverCount++
				log.Printf("[CLIENT] Failover successful: %s rerouted to %s",
					chatID, node.NodeID)
			}
			c.mu.Unlock()
			return resp, nil
		}

		lastErr = err
		if err != nil {
			log.Printf("[CLIENT] Failed to reach %s: %v", node.NodeID, err)
		} else if !resp.Success {
			log.Printf("[CLIENT] Server %s rejected request: %s", node.NodeID, resp.ErrorMessage)
		}

		// Mark this connection as potentially unhealthy
		c.markConnectionUnhealthy(node.Address)
	}

	c.mu.Lock()
	c.stats.FailedRequests++
	c.mu.Unlock()

	return nil, fmt.Errorf("all servers exhausted: %w", lastErr)
}

// sendToServer sends a request to a specific server
func (c *SmartClient) sendToServer(address string, req *pb.ChatRequest) (*pb.ChatResponse, error) {
	c.mu.RLock()
	conn, exists := c.connections[address]
	c.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no connection to %s", address)
	}

	// Check if marked unhealthy (simulated failure)
	if !conn.healthy {
		return nil, fmt.Errorf("server %s is marked as down", address)
	}

	if conn.client == nil {
		// Try to reconnect
		c.mu.Lock()
		grpcConn, err := c.connectToServer(address)
		if err != nil {
			c.mu.Unlock()
			return nil, err
		}
		conn.conn = grpcConn
		conn.client = pb.NewChatServiceClient(grpcConn)
		conn.healthy = true
		c.mu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.config.RequestTimeout)
	defer cancel()

	return conn.client.PostMessage(ctx, req)
}

// connectToServer establishes a gRPC connection to a server
func (c *SmartClient) connectToServer(address string) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.ConnectTimeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", address, err)
	}

	return conn, nil
}

// markConnectionUnhealthy marks a connection as potentially failed
func (c *SmartClient) markConnectionUnhealthy(address string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if conn, exists := c.connections[address]; exists {
		conn.healthy = false
	}
}

// GetStats returns current client statistics
func (c *SmartClient) GetStats() ClientStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

// GetTargetServer returns which server would handle a given chat ID
func (c *SmartClient) GetTargetServer(chatID string) (string, string, bool) {
	return c.ring.GetNode(chatID)
}

// GetServerCount returns the number of servers in the routing table
func (c *SmartClient) GetServerCount() int {
	return c.ring.GetNodeCount()
}

// Close closes all connections
func (c *SmartClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for addr, conn := range c.connections {
		if conn.conn != nil {
			conn.conn.Close()
			log.Printf("[CLIENT] Closed connection to %s", addr)
		}
	}
	c.connections = make(map[string]*serverConnection)
}

// HealthCheck checks if a specific server is healthy
func (c *SmartClient) HealthCheck(serverID string) (bool, error) {
	addr, ok := c.ring.GetNodeAddress(serverID)
	if !ok {
		return false, fmt.Errorf("server %s not found", serverID)
	}

	c.mu.RLock()
	conn, exists := c.connections[addr]
	c.mu.RUnlock()

	if !exists || conn.client == nil {
		return false, nil
	}

	if !conn.healthy {
		return false, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := conn.client.HealthCheck(ctx, &pb.HealthRequest{})
	if err != nil {
		return false, err
	}

	return resp.Healthy, nil
}

// DebugPrint prints client state for debugging
func (c *SmartClient) DebugPrint() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	fmt.Println("\n=== Smart Client State ===")
	fmt.Printf("Connected Servers: %d\n", len(c.connections))
	for addr, conn := range c.connections {
		status := "UP"
		if !conn.healthy {
			status = "DOWN"
		}
		fmt.Printf("  - %s [%s]\n", addr, status)
	}

	stats := c.stats
	fmt.Printf("\nStatistics:\n")
	fmt.Printf("  Total Requests:   %d\n", stats.TotalRequests)
	fmt.Printf("  Success Requests: %d\n", stats.SuccessRequests)
	fmt.Printf("  Failed Requests:  %d\n", stats.FailedRequests)
	fmt.Printf("  Primary Hits:     %d\n", stats.PrimaryHits)
	fmt.Printf("  Failovers:        %d\n", stats.FailoverCount)
	fmt.Println("===========================\n")

	c.ring.DebugPrint()
}

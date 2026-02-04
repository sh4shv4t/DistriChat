// Package server implements a gRPC chat server with hierarchical caching.
// Each server instance represents a compute node with limited memory
// that can handle chat sessions with L1/L2 cache tiers.
package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/distribchat/pkg/cache"
	pb "github.com/distribchat/proto"
	"google.golang.org/grpc"
)

// ChatServer implements the gRPC ChatService with hierarchical caching
type ChatServer struct {
	pb.UnimplementedChatServiceServer

	// Server identification
	serverID string
	address  string
	port     int

	// Cache for chat sessions
	cache *cache.HierarchicalCache

	// gRPC server instance
	grpcServer *grpc.Server

	// Server state
	startTime time.Time
	healthy   atomic.Bool
	mu        sync.RWMutex

	// Shutdown coordination
	shutdownCh chan struct{}
}

// ServerConfig contains configuration for creating a new server
type ServerConfig struct {
	ServerID   string
	Port       int
	L1Capacity int // GPU VRAM simulation (default: 5)
	L2Capacity int // RAM simulation (default: 20)
}

// NewChatServer creates a new chat server instance
func NewChatServer(config ServerConfig) *ChatServer {
	if config.L1Capacity <= 0 {
		config.L1Capacity = 5
	}
	if config.L2Capacity <= 0 {
		config.L2Capacity = 20
	}

	server := &ChatServer{
		serverID:   config.ServerID,
		port:       config.Port,
		address:    fmt.Sprintf("localhost:%d", config.Port),
		cache:      cache.NewHierarchicalCache(config.ServerID, config.L1Capacity, config.L2Capacity),
		startTime:  time.Now(),
		shutdownCh: make(chan struct{}),
	}

	server.healthy.Store(true)

	return server
}

// Start starts the gRPC server and begins accepting connections
func (s *ChatServer) Start() error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", s.port, err)
	}

	s.grpcServer = grpc.NewServer()
	pb.RegisterChatServiceServer(s.grpcServer, s)

	log.Printf("[SERVER:%s] Starting gRPC server on %s (L1: %d, L2: %d)",
		s.serverID, s.address, 
		s.cache.GetCacheInfo().L1Capacity,
		s.cache.GetCacheInfo().L2Capacity)

	go func() {
		if err := s.grpcServer.Serve(listener); err != nil {
			log.Printf("[SERVER:%s] gRPC server error: %v", s.serverID, err)
		}
	}()

	return nil
}

// Stop gracefully stops the server
func (s *ChatServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.healthy.Store(false)

	if s.grpcServer != nil {
		log.Printf("[SERVER:%s] Shutting down...", s.serverID)
		s.grpcServer.GracefulStop()
	}

	close(s.shutdownCh)
	log.Printf("[SERVER:%s] Server stopped", s.serverID)
}

// PostMessage handles incoming chat messages
func (s *ChatServer) PostMessage(ctx context.Context, req *pb.ChatRequest) (*pb.ChatResponse, error) {
	if !s.healthy.Load() {
		return &pb.ChatResponse{
			Success:      false,
			ServerId:     s.serverID,
			ErrorMessage: "server is shutting down",
		}, nil
	}

	log.Printf("[SERVER:%s] Received message for chat %s: %s",
		s.serverID, req.ChatId, truncateString(req.Message, 50))

	// Add message to cache
	msg := cache.Message{
		Content:   req.Message,
		SenderID:  req.SenderId,
		Timestamp: time.Unix(req.Timestamp, 0),
	}

	session, level, err := s.cache.AddMessage(req.ChatId, msg)
	if err != nil {
		return &pb.ChatResponse{
			Success:      false,
			ServerId:     s.serverID,
			ErrorMessage: err.Error(),
		}, nil
	}

	// Convert cache level to proto enum
	var cacheLocation pb.CacheLocation
	switch level {
	case cache.LevelL1:
		cacheLocation = pb.CacheLocation_CACHE_L1
	case cache.LevelL2:
		cacheLocation = pb.CacheLocation_CACHE_L2
	case cache.LevelMiss:
		cacheLocation = pb.CacheLocation_CACHE_MISS
	default:
		cacheLocation = pb.CacheLocation_CACHE_UNKNOWN
	}

	log.Printf("[SERVER:%s] Processed chat %s (cache: %s, messages: %d)",
		s.serverID, req.ChatId, level.String(), session.MessageCount)

	return &pb.ChatResponse{
		Success:       true,
		ServerId:      s.serverID,
		CacheLocation: cacheLocation,
		MessageCount:  int32(session.MessageCount),
	}, nil
}

// GetCacheStats returns current cache statistics
func (s *ChatServer) GetCacheStats(ctx context.Context, req *pb.StatsRequest) (*pb.StatsResponse, error) {
	info := s.cache.GetCacheInfo()

	return &pb.StatsResponse{
		ServerId:      s.serverID,
		L1Size:        int32(info.L1Size),
		L1Capacity:    int32(info.L1Capacity),
		L2Size:        int32(info.L2Size),
		L2Capacity:    int32(info.L2Capacity),
		TotalRequests: info.Stats.TotalRequests,
		CacheHits:     info.Stats.CacheHits,
		CacheMisses:   info.Stats.CacheMisses,
		L1Chats:       info.L1Chats,
		L2Chats:       info.L2Chats,
	}, nil
}

// HealthCheck verifies the server is alive
func (s *ChatServer) HealthCheck(ctx context.Context, req *pb.HealthRequest) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{
		Healthy:       s.healthy.Load(),
		ServerId:      s.serverID,
		UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
	}, nil
}

// GetServerID returns the server's ID
func (s *ChatServer) GetServerID() string {
	return s.serverID
}

// GetAddress returns the server's network address
func (s *ChatServer) GetAddress() string {
	return s.address
}

// GetPort returns the server's port
func (s *ChatServer) GetPort() int {
	return s.port
}

// IsHealthy returns whether the server is accepting requests
func (s *ChatServer) IsHealthy() bool {
	return s.healthy.Load()
}

// GetCacheInfo returns detailed cache information
func (s *ChatServer) GetCacheInfo() cache.CacheInfo {
	return s.cache.GetCacheInfo()
}

// DebugPrint prints the current server and cache state
func (s *ChatServer) DebugPrint() {
	fmt.Printf("\n=== Server %s ===\n", s.serverID)
	fmt.Printf("Address: %s\n", s.address)
	fmt.Printf("Healthy: %v\n", s.healthy.Load())
	fmt.Printf("Uptime: %v\n", time.Since(s.startTime))
	s.cache.DebugPrint()
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

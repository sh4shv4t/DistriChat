// DistriChat - Distributed Chat Routing Engine
//
// This is the main simulation that demonstrates:
// 1. Consistent Hashing with Virtual Nodes
// 2. Hierarchical L1/L2 Caching
// 3. Automatic Failover when a server goes down
//
// Run with: go run main.go
package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/distribchat/cmd/client"
	"github.com/distribchat/cmd/server"
)

const (
	// Server ports
	serverAPort = 50051
	serverBPort = 50052
	serverCPort = 50053

	// Virtual node counts (capacity - affects load distribution)
	serverACapacity = 100 // Standard capacity
	serverBCapacity = 150 // Higher capacity - more load
	serverCCapacity = 100 // Standard capacity

	// Cache settings
	l1Capacity = 5  // L1 (VRAM) capacity per server
	l2Capacity = 20 // L2 (RAM) capacity per server

	// Simulation settings
	totalMessages    = 50  // Total messages to send
	uniqueChats      = 25  // Number of unique chat sessions
	killServerAfter  = 10  // Kill Server B after this many messages
	messageDelay     = 100 * time.Millisecond
)

func main() {
	fmt.Println(banner)
	fmt.Println("DistriChat - High-Performance Distributed Routing Engine")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// ================================================================
	// PHASE 1: Start Servers
	// ================================================================
	fmt.Println("📦 PHASE 1: Starting Servers...")
	fmt.Println(strings.Repeat("-", 40))

	servers := startServers()
	defer stopServers(servers)

	// Give servers time to start
	time.Sleep(500 * time.Millisecond)
	fmt.Println()

	// ================================================================
	// PHASE 2: Initialize Smart Client
	// ================================================================
	fmt.Println("🔗 PHASE 2: Initializing Smart Client...")
	fmt.Println(strings.Repeat("-", 40))

	smartClient := initializeClient(servers)
	defer smartClient.Close()

	fmt.Println()

	// ================================================================
	// PHASE 3: Send Messages (Normal Operation)
	// ================================================================
	fmt.Println("📨 PHASE 3: Sending Messages (Normal Operation)...")
	fmt.Println(strings.Repeat("-", 40))

	messagesSent := 0
	serverBKilled := false

	// Track which chats go to which servers (before failure)
	chatAssignments := make(map[string]string)

	for i := 1; i <= totalMessages; i++ {
		select {
		case <-sigChan:
			fmt.Println("\n🛑 Received shutdown signal, stopping simulation...")
			return
		default:
		}

		// Generate a chat ID
		chatID := fmt.Sprintf("chat-%03d", (i-1)%uniqueChats)
		senderID := fmt.Sprintf("user-%d", rand.Intn(100))
		message := generateMessage(i)

		// Record initial assignment if not seen before
		if _, exists := chatAssignments[chatID]; !exists {
			targetServer, _, _ := smartClient.GetTargetServer(chatID)
			chatAssignments[chatID] = targetServer
		}

		// Send the message
		resp, err := smartClient.SendMessage(chatID, senderID, message)
		if err != nil {
			log.Printf("❌ Message %d failed: %v", i, err)
		} else {
			cacheIndicator := getCacheIndicator(resp.CacheLocation.String())
			log.Printf("✅ Message %d → Server %s | %s | Chat: %s (msgs: %d)",
				i, resp.ServerId, cacheIndicator, chatID, resp.MessageCount)
		}

		messagesSent++

		// ================================================================
		// PHASE 4: Simulate Server Failure
		// ================================================================
		if i == killServerAfter && !serverBKilled {
			fmt.Println()
			fmt.Println("💥 PHASE 4: SIMULATING SERVER FAILURE!")
			fmt.Println(strings.Repeat("=", 60))
			fmt.Printf("🔥 Killing Server B (port %d)...\n", serverBPort)

			// Stop Server B
			servers["B"].Stop()

			// Mark as down in client
			smartClient.MarkServerDown("Server-B")

			serverBKilled = true

			// Show which chats were on Server B and will need failover
			affectedChats := 0
			for chatID, targetServer := range chatAssignments {
				if targetServer == "Server-B" {
					affectedChats++
					newTarget, _, _ := smartClient.GetTargetServer(chatID)
					fmt.Printf("   📍 %s: Server-B → %s (failover)\n", chatID, newTarget)
				}
			}
			fmt.Printf("\n   Total affected chats: %d\n", affectedChats)
			fmt.Println(strings.Repeat("=", 60))
			fmt.Println()

			time.Sleep(500 * time.Millisecond)
		}

		time.Sleep(messageDelay)
	}

	fmt.Println()

	// ================================================================
	// PHASE 5: Final Statistics
	// ================================================================
	fmt.Println("📊 PHASE 5: Final Statistics")
	fmt.Println(strings.Repeat("=", 60))

	// Client statistics
	stats := smartClient.GetStats()
	fmt.Println("\n📈 Client Statistics:")
	fmt.Printf("   Total Requests:   %d\n", stats.TotalRequests)
	fmt.Printf("   Successful:       %d (%.1f%%)\n", stats.SuccessRequests,
		float64(stats.SuccessRequests)/float64(stats.TotalRequests)*100)
	fmt.Printf("   Failed:           %d\n", stats.FailedRequests)
	fmt.Printf("   Primary Hits:     %d\n", stats.PrimaryHits)
	fmt.Printf("   Failovers:        %d\n", stats.FailoverCount)

	// Server cache statistics
	fmt.Println("\n💾 Server Cache Statistics:")
	for name, srv := range servers {
		if !srv.IsHealthy() {
			fmt.Printf("\n   Server %s: OFFLINE\n", name)
			continue
		}
		info := srv.GetCacheInfo()
		fmt.Printf("\n   Server %s:\n", name)
		fmt.Printf("     L1 Cache: %d/%d\n", info.L1Size, info.L1Capacity)
		fmt.Printf("     L2 Cache: %d/%d\n", info.L2Size, info.L2Capacity)
		fmt.Printf("     Cache Hits: %d (L1: %d, L2: %d)\n",
			info.Stats.CacheHits, info.Stats.L1Hits, info.Stats.L2Hits)
		fmt.Printf("     Cache Misses: %d\n", info.Stats.CacheMisses)
		fmt.Printf("     Demotions: %d, Evictions: %d\n",
			info.Stats.Demotions, info.Stats.Evictions)
	}

	fmt.Println()
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("✨ Simulation Complete!")
	fmt.Println()
}

// startServers creates and starts all server instances
func startServers() map[string]*server.ChatServer {
	servers := make(map[string]*server.ChatServer)

	// Server A - Standard capacity
	serverA := server.NewChatServer(server.ServerConfig{
		ServerID:   "Server-A",
		Port:       serverAPort,
		L1Capacity: l1Capacity,
		L2Capacity: l2Capacity,
	})
	if err := serverA.Start(); err != nil {
		log.Fatalf("Failed to start Server A: %v", err)
	}
	servers["A"] = serverA

	// Server B - Higher capacity (will be killed)
	serverB := server.NewChatServer(server.ServerConfig{
		ServerID:   "Server-B",
		Port:       serverBPort,
		L1Capacity: l1Capacity,
		L2Capacity: l2Capacity,
	})
	if err := serverB.Start(); err != nil {
		log.Fatalf("Failed to start Server B: %v", err)
	}
	servers["B"] = serverB

	// Server C - Standard capacity
	serverC := server.NewChatServer(server.ServerConfig{
		ServerID:   "Server-C",
		Port:       serverCPort,
		L1Capacity: l1Capacity,
		L2Capacity: l2Capacity,
	})
	if err := serverC.Start(); err != nil {
		log.Fatalf("Failed to start Server C: %v", err)
	}
	servers["C"] = serverC

	return servers
}

// stopServers gracefully stops all servers
func stopServers(servers map[string]*server.ChatServer) {
	fmt.Println("\n🛑 Stopping all servers...")
	for name, srv := range servers {
		if srv.IsHealthy() {
			srv.Stop()
			fmt.Printf("   ✓ Server %s stopped\n", name)
		}
	}
}

// initializeClient creates and configures the smart client
func initializeClient(servers map[string]*server.ChatServer) *client.SmartClient {
	config := client.DefaultClientConfig()
	config.VirtualNodes = 100

	smartClient := client.NewSmartClient(config)

	// Add all servers to the client's hash ring
	smartClient.AddServer("Server-A", fmt.Sprintf("localhost:%d", serverAPort), serverACapacity)
	smartClient.AddServer("Server-B", fmt.Sprintf("localhost:%d", serverBPort), serverBCapacity)
	smartClient.AddServer("Server-C", fmt.Sprintf("localhost:%d", serverCPort), serverCCapacity)

	fmt.Printf("   ✓ Added Server-A (capacity: %d)\n", serverACapacity)
	fmt.Printf("   ✓ Added Server-B (capacity: %d) - WILL BE KILLED\n", serverBCapacity)
	fmt.Printf("   ✓ Added Server-C (capacity: %d)\n", serverCCapacity)

	return smartClient
}

// generateMessage creates a sample message for testing
func generateMessage(index int) string {
	messages := []string{
		"Hello, how are you?",
		"I'm doing great, thanks for asking!",
		"What's the weather like today?",
		"It's sunny and warm here.",
		"Have you seen the latest news?",
		"Yes, it's quite interesting!",
		"Let's meet up later.",
		"Sure, what time works for you?",
		"How about 3 PM?",
		"Perfect, see you then!",
	}
	return fmt.Sprintf("[Msg %d] %s", index, messages[index%len(messages)])
}

// getCacheIndicator returns an emoji/indicator for cache status
func getCacheIndicator(cacheStatus string) string {
	switch {
	case strings.Contains(cacheStatus, "L1"):
		return "🔥 L1-HIT"
	case strings.Contains(cacheStatus, "L2"):
		return "💨 L2-HIT"
	case strings.Contains(cacheStatus, "MISS"):
		return "❄️  MISS"
	default:
		return "❓ UNKNOWN"
	}
}

const banner = `
╔═══════════════════════════════════════════════════════════════╗
║                                                               ║
║   ██████╗ ██╗███████╗████████╗██████╗ ██╗ ██████╗██╗  ██╗    ║
║   ██╔══██╗██║██╔════╝╚══██╔══╝██╔══██╗██║██╔════╝██║  ██║    ║
║   ██║  ██║██║███████╗   ██║   ██████╔╝██║██║     ███████║    ║
║   ██║  ██║██║╚════██║   ██║   ██╔══██╗██║██║     ██╔══██║    ║
║   ██████╔╝██║███████║   ██║   ██║  ██║██║╚██████╗██║  ██║    ║
║   ╚═════╝ ╚═╝╚══════╝   ╚═╝   ╚═╝  ╚═╝╚═╝ ╚═════╝╚═╝  ╚═╝    ║
║                                                               ║
║   Distributed Chat Routing Engine with Consistent Hashing    ║
║                                                               ║
╚═══════════════════════════════════════════════════════════════╝
`

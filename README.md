# DistriChat 🚀

**High-Performance Distributed Routing Engine with Consistent Hashing**

DistriChat is a distributed, fault-tolerant routing and caching layer designed to manage stateful chat sessions across a dynamic cluster of inference servers. It solves the "Thundering Herd" problem common in scaling monolithic chat applications by implementing **Consistent Hashing with Virtual Nodes**.

## 🎯 Key Features

### 1. Consistent Hash Ring (The Core)
- Implements a sorted map of `hash(server_id + virtual_node_index)` for load distribution
- Uses `sort.Search` (binary search) for **O(log N)** lookups
- Thread-safe with `sync.RWMutex` for concurrent access

### 2. Virtual Nodes (Skew Mitigation)
- Heavily weighted servers get more virtual nodes on the ring
- Prevents hotspots and ensures even load distribution
- Configurable capacity per server

### 3. L1/L2 Hierarchical Cache (Resource Management)
- **L1 Cache**: Simulates GPU VRAM (hot cache, capacity: 5)
- **L2 Cache**: Simulates System RAM (warm cache, capacity: 20)
- **LRU Eviction Policy**: L1 → Demote to L2 → Evict to Disk

### 4. Smart Client with Failover
- Uses sticky sessions (server affinity) for optimal cache hits
- Automatically "walks the ring" if the primary node is unreachable
- Configurable retry attempts and timeouts

### 5. gRPC Communication
- High-performance Protocol Buffer based communication
- Type-safe contracts between Client and Server
- Streaming support ready

## 📁 Project Structure

```
DistriChat/
├── main.go                 # Simulation entry point
├── go.mod                  # Go module definition
├── Makefile               # Build automation
├── README.md              # This file
│
├── proto/                 # Protocol Buffer definitions
│   ├── chat.proto         # Service definitions
│   ├── chat.pb.go         # Generated Go code
│   └── chat_grpc.pb.go    # Generated gRPC code
│
├── pkg/                   # Reusable libraries
│   ├── ring/              # Consistent Hash Ring
│   │   ├── ring.go        # Implementation
│   │   └── ring_test.go   # Tests
│   │
│   └── cache/             # Hierarchical Cache
│       ├── cache.go       # L1/L2 cache implementation
│       └── cache_test.go  # Tests
│
└── cmd/                   # Application components
    ├── server/            # gRPC Server
    │   └── server.go      # Chat server with caching
    │
    └── client/            # Smart Client
        └── client.go      # Hash ring routing with failover
```

## 🚀 Quick Start

### Prerequisites
- Go 1.21 or later
- Make (optional, for using Makefile)

### Run the Simulation

```bash
# Using Make
make run

# Or directly with Go
go run main.go
```

### Run Tests

```bash
# All tests
make test

# With coverage
make test-cover

# Benchmarks
make bench
```

### Build Binary

```bash
make build
./bin/distribchat
```

## 🎮 Simulation Demo

The simulation demonstrates:

1. **Starting 3 Servers** (A, B, C) with different capacities
2. **Sending 50 messages** across 25 unique chat sessions
3. **Killing Server B** after 10 messages
4. **Automatic failover** of Server B's traffic to other servers

### Sample Output

```
╔═══════════════════════════════════════════════════════════════╗
║   DistriChat - Distributed Chat Routing Engine                ║
╚═══════════════════════════════════════════════════════════════╝

📦 PHASE 1: Starting Servers...
[SERVER:Server-A] Starting gRPC server on localhost:50051
[SERVER:Server-B] Starting gRPC server on localhost:50052
[SERVER:Server-C] Starting gRPC server on localhost:50053

📨 PHASE 3: Sending Messages...
✅ Message 1 → Server A | 🔥 L1-HIT | Chat: chat-001 (msgs: 1)
✅ Message 2 → Server B | ❄️  MISS  | Chat: chat-002 (msgs: 1)
...

💥 PHASE 4: SIMULATING SERVER FAILURE!
🔥 Killing Server B (port 50052)...
   📍 chat-002: Server-B → Server-C (failover)
   📍 chat-007: Server-B → Server-A (failover)

✅ Message 11 → Server C | 🔥 L1-HIT | Chat: chat-002 (msgs: 2)
[Failover successful: chat-002 rerouted to Server-C]
```

## 📊 Architecture

### Consistent Hashing Flow

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐
│   Client    │────▶│  Hash Ring   │────▶│  Target     │
│  (ChatID)   │     │  (Binary     │     │  Server     │
│             │     │   Search)    │     │             │
└─────────────┘     └──────────────┘     └─────────────┘
                           │
                    ┌──────┴──────┐
                    │  Virtual    │
                    │   Nodes     │
                    │  (100 per   │
                    │   server)   │
                    └─────────────┘
```

### Cache Hierarchy

```
┌─────────────────────────────────────────┐
│                Server                    │
├─────────────────────────────────────────┤
│  ┌─────────────────────────────────┐    │
│  │  L1 Cache (VRAM) - 5 sessions   │    │
│  │  [LRU] ────────────────────▶    │    │
│  └───────────────┬─────────────────┘    │
│                  │ Demote               │
│  ┌───────────────▼─────────────────┐    │
│  │  L2 Cache (RAM) - 20 sessions   │    │
│  │  [LRU] ────────────────────▶    │    │
│  └───────────────┬─────────────────┘    │
│                  │ Evict                │
│                  ▼                      │
│             (Disk/Gone)                 │
└─────────────────────────────────────────┘
```

## 🔧 Configuration

### Server Configuration

```go
serverConfig := server.ServerConfig{
    ServerID:   "Server-A",
    Port:       50051,
    L1Capacity: 5,   // GPU VRAM simulation
    L2Capacity: 20,  // RAM simulation
}
```

### Client Configuration

```go
clientConfig := client.ClientConfig{
    VirtualNodes:   100,              // Virtual nodes per server
    MaxRetries:     3,                // Failover attempts
    ConnectTimeout: 5 * time.Second,
    RequestTimeout: 10 * time.Second,
}
```

## 📈 Performance

### Benchmarks

```
BenchmarkGetNode-8       5000000    234 ns/op    0 B/op    0 allocs/op
BenchmarkAddMessage-8    1000000   1123 ns/op  320 B/op    4 allocs/op
```

### Complexity

| Operation | Time Complexity |
|-----------|-----------------|
| GetNode   | O(log N)        |
| AddNode   | O(N log N)      |
| RemoveNode| O(N)            |
| Cache Get | O(1)            |
| Cache Add | O(1)            |

## 🧪 Testing

### Unit Tests

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run specific package
go test -v ./pkg/ring/...
```

### Coverage

```bash
go test -cover -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## 📜 API Reference

### gRPC Service

```protobuf
service ChatService {
    rpc PostMessage(ChatRequest) returns (ChatResponse);
    rpc GetCacheStats(StatsRequest) returns (StatsResponse);
    rpc HealthCheck(HealthRequest) returns (HealthResponse);
}
```

### Hash Ring API

```go
// Create a new ring with 100 virtual nodes per server
ring := ring.NewHashRing(100)

// Add a server with custom capacity
ring.AddNode("server-a", 150, "localhost:50051")

// Find the server for a key
nodeID, address, ok := ring.GetNode("chat-123")

// Get ordered servers for failover
nodes := ring.GetNodes("chat-123", 3)
```

### Cache API

```go
// Create hierarchical cache
cache := cache.NewHierarchicalCache("server-a", 5, 20)

// Get or create a session
session, level := cache.GetOrCreate("chat-123")

// Add a message
session, level, err := cache.AddMessage("chat-123", message)
```

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Inspired by consistent hashing papers and distributed systems literature
- Built with [gRPC-Go](https://grpc.io/docs/languages/go/)
- Uses [Protocol Buffers](https://developers.google.com/protocol-buffers) for serialization

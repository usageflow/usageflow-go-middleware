# Socket Package

The `socket` package provides WebSocket connection management for real-time communication with the UsageFlow service.

## Overview

This package implements:
- Connection pooling for high throughput
- Automatic reconnection with exponential backoff
- Ping/pong health monitoring
- Request/response correlation
- Thread-safe connection management

## Core Components

### `UsageFlowSocketManager`

Manages a pool of WebSocket connections to UsageFlow service.

```go
type UsageFlowSocketManager struct {
    connections     []*PooledConnection
    wsURL           string
    poolSize        int
    currentIndex    int
    connecting      bool
    connectionMutex sync.Mutex
    apiKey          string
    mu              sync.RWMutex
}
```

**Features:**
- Connection pooling (default: 10 connections)
- Least-busy connection selection
- Round-robin load distribution
- Automatic reconnection

### `PooledConnection`

Represents a single WebSocket connection in the pool.

```go
type PooledConnection struct {
    ws              *websocket.Conn
    connected       bool
    pendingRequests int
    index           int
    mu              sync.RWMutex
    messageHandlers map[string]chan *UsageFlowSocketResponse
}
```

## Key Methods

### `NewUsageFlowSocketManager(apiKey string, poolSize ...int) *UsageFlowSocketManager`

Creates a new WebSocket manager instance.

```go
manager := socket.NewUsageFlowSocketManager("api-key")
manager := socket.NewUsageFlowSocketManager("api-key", 5) // Custom pool size
```

**Initialization:**
- Creates connection pool (default: 10 connections)
- Establishes all connections in parallel
- Sets up ping/pong handlers
- Starts message handler goroutines

### `Connect() error`

Establishes all WebSocket connections in the pool.

**Behavior:**
- Creates connections in parallel
- Retries failed connections in background
- Thread-safe (uses connectionMutex)

### `SendAsync(payload *UsageFlowSocketMessage) (*UsageFlowSocketResponse, error)`

Sends a message and waits for a response with timeout.

```go
response, err := manager.SendAsync(&socket.UsageFlowSocketMessage{
    Type: "request_for_allocation",
    Payload: &socket.RequestForAllocation{
        Alias:  "ledger-id",
        Amount: 1.0,
    },
})
```

**Features:**
- Automatic connection selection (least-busy)
- Request/response correlation via unique IDs
- Timeout handling (default: 2 seconds)
- Thread-safe

### `Send(payload *UsageFlowSocketMessage) error`

Sends a message without waiting for a response (fire-and-forget).

### `IsConnected() bool`

Checks if at least one connection is active.

**Thread-safe:** Uses read lock for concurrent access

## Connection Management

### Automatic Reconnection

**Triggers:**
- Connection close detected
- Read error (server restart, network issue)
- Ping failure

**Strategy:**
- Exponential backoff (5s, 10s, 20s, 40s, 60s max)
- Maximum 5 retry attempts
- Background reconnection (non-blocking)

### Health Monitoring

**Ping/Pong Mechanism:**
- Ping sent every 30 seconds
- Pong timeout: 60 seconds
- Read deadline extended on successful pong
- Connection marked dead on ping failure

**Read Deadline:**
- Initial: 60 seconds
- Extended on: successful read, pong received
- Timeout triggers reconnection

## Message Types

### `UsageFlowSocketMessage`

Outgoing message structure.

```go
type UsageFlowSocketMessage struct {
    Type    string      // Message type (e.g., "request_for_allocation")
    Payload interface{} // Message payload
    ID      string      // Unique correlation ID (auto-generated)
}
```

### `UsageFlowSocketResponse`

Incoming response structure.

```go
type UsageFlowSocketResponse struct {
    Type    string      // Response type
    Payload interface{} // Response payload
    ID      string      // Correlation ID
    ReplyTo string      // Original request ID
    Message string      // Optional message
    Error   string      // Error message (if any)
}
```

### Request Types

#### `RequestForAllocation`

Request for resource allocation.

```go
type RequestForAllocation struct {
    Alias    string                 // Ledger identifier
    Amount   float64                // Allocation amount
    Metadata map[string]interface{} // Optional metadata
}
```

#### `UseAllocationRequest`

Request to use/fulfill an allocation.

```go
type UseAllocationRequest struct {
    Alias        string                 // Ledger identifier
    Amount       float64                // Usage amount
    AllocationID string                 // Allocation ID from request
    Metadata     map[string]interface{} // Optional metadata
}
```

## Configuration Constants

```go
const (
    defaultWSURL      = "ws://localhost:9000/ws" // Development
    // defaultWSURL  = "wss://api.usageflow.io/ws" // Production
    defaultPoolSize   = 10
    reconnectDelay    = 5 * time.Second
    requestTimeout    = 2 * time.Second
    pingPeriod        = 30 * time.Second
    pongWait          = 60 * time.Second
    writeWait         = 10 * time.Second
    maxReconnectTries = 5
)
```

## Connection Pool Strategy

### Selection Algorithm

1. **Filter**: Only connected connections
2. **Select**: Least-busy connection (lowest pendingRequests)
3. **Fallback**: Round-robin if all have same load

### Load Balancing

- Tracks `pendingRequests` per connection
- Selects connection with minimum pending requests
- Round-robin for equal load distribution

## Error Handling

### Connection Errors

- **Dial failure**: Retries in background with exponential backoff
- **Read error**: Triggers reconnection, clears pending handlers
- **Write error**: Returns error to caller, updates connection status
- **Timeout**: Returns timeout error, cleans up handler

### Message Errors

- **Unmarshal error**: Logged, message skipped
- **Missing handler**: Message discarded (timeout or orphaned)
- **Handler timeout**: Handler cleaned up, error returned

## Thread Safety

All operations are thread-safe:
- **Read operations**: Use `sync.RWMutex` (multiple concurrent reads)
- **Write operations**: Use `sync.Mutex` (exclusive access)
- **Connection access**: Protected by connection-specific mutex
- **Handler map**: Protected by connection mutex

## Example Usage

```go
package main

import (
    "github.com/usageflow/usageflow-go-middleware/pkg/socket"
)

func main() {
    // Create manager
    manager := socket.NewUsageFlowSocketManager("api-key", 5)
    
    // Send async request
    response, err := manager.SendAsync(&socket.UsageFlowSocketMessage{
        Type: "get_application_policies",
        Payload: nil,
    })
    
    if err != nil {
        // Handle error
        return
    }
    
    // Process response
    if response.Error != "" {
        // Handle error response
        return
    }
    
    // Use response.Payload
}
```

## Best Practices

1. **Pool Size**: Adjust based on expected load (default 10 is usually sufficient)
2. **Error Handling**: Always check `response.Error` field
3. **Timeouts**: Be aware of 2-second default timeout
4. **Connection Status**: Check `IsConnected()` before critical operations
5. **Graceful Shutdown**: Call `Close()` on application shutdown

## Performance Considerations

- **Connection Pool**: Reduces connection overhead
- **Parallel Connections**: Established in parallel
- **Least-Busy Selection**: Distributes load evenly
- **Non-blocking Reconnection**: Doesn't block request processing


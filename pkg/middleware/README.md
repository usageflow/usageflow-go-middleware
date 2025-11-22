# Middleware Package

The `middleware` package provides the core Gin middleware for integrating UsageFlow API tracking and metering into your Go web applications.

## Overview

This package implements:

- Request interception and monitoring
- Usage tracking and allocation
- User identification from various sources
- Graceful degradation when WebSocket is disconnected
- Automatic configuration updates

## Core Components

### `UsageFlowAPI`

Main middleware structure that manages the entire UsageFlow integration.

```go
type UsageFlowAPI struct {
    APIKey                      string
    ApplicationId                string
    ApiConfig                   []config.ApiConfigStrategy
    ApplicationEndpointPolicies *config.PolicyResponse
    policyMap                   PolicyMap
    mu                          sync.RWMutex
    socketManager               *socket.UsageFlowSocketManager
    connected                   bool
}
```

**Thread Safety:** All public methods are thread-safe using `sync.RWMutex`.

### Key Methods

#### `New(apiKey string) *UsageFlowAPI`

Creates a new UsageFlowAPI instance and initializes the WebSocket connection pool.

```go
uf := middleware.New("your-api-key")
```

**Initialization:**

- Creates WebSocket manager with connection pool
- Starts automatic config updater (every 30 seconds)
- Initializes connection status tracking

#### `RequestInterceptor(routes, whiteListRoutes []config.Route) gin.HandlerFunc`

Creates a Gin middleware handler for intercepting and monitoring requests.

```go
routes := []config.Route{
    {Method: "*", URL: "*"},
}
whiteList := []config.Route{
    {Method: "GET", URL: "/health"},
}

r.Use(uf.RequestInterceptor(routes, whiteList))
```

**Behavior:**

1. Checks if route should be monitored (based on routes parameter)
2. Checks if route is whitelisted (skips monitoring)
3. Captures request metadata
4. Executes allocation request (if socket connected)
5. Processes original request
6. Executes fulfill request after completion

**Graceful Degradation:**

- If WebSocket is disconnected, requests continue normally
- No errors are returned to the client
- Middleware silently skips UsageFlow operations

#### `FetchApiConfig() ([]config.ApiConfigStrategy, error)`

Fetches the latest API configuration from UsageFlow service via WebSocket.

**Automatic Updates:**

- Called immediately on initialization
- Automatically called every 30 seconds
- Updates are thread-safe

#### `GetUserPrefix(c *gin.Context, method, url string) string`

Extracts user identifier from the request based on configuration.

**Supported Identity Locations:**

- `"headers"`: HTTP header value
- `"query"`: Query parameter
- `"path_params"`: URL path parameter
- `"query_params"`: Query parameter (alias)
- `"body"`: JSON body field
- `"bearer_token"`: JWT token claim

**Returns:** Transformed ledger ID format (lowercase, underscores)

#### `ExecuteRequestWithMetadata(ledgerId, method, url string, metadata map[string]interface{}, c *gin.Context) (bool, error)`

Executes the initial allocation request before processing the main request.

**Behavior:**

- Creates allocation via WebSocket
- Stores allocation ID in context (`eventId`)
- Returns `true` on success or if socket disconnected
- Returns `false` only on actual errors when connected

#### `ExecuteFulfillRequestWithMetadata(ledgerId, method, url string, metadata map[string]interface{}, c *gin.Context) (bool, error)`

Executes the fulfill request after the main request is processed.

**Behavior:**

- Retrieves allocation ID from context
- Calculates request duration
- Sends fulfill request via WebSocket
- Always returns `true` to allow request to complete

## Utility Functions

### `GetPatternedURL(c *gin.Context) string`

Extracts the URL pattern from Gin context, using `FullPath()` if available, falling back to raw path.

### `ExtractBearerToken(c *gin.Context) (string, error)`

Extracts Bearer token from Authorization header.

### `DecodeJWTUnverified(token string) (map[string]interface{}, error)`

Decodes JWT token without signature verification (for identity extraction only).

### `TransformToLedgerId(input string) string`

Converts input string to valid ledger ID format:

- Lowercase
- Non-alphanumeric characters replaced with underscores

### `GetRequestBody(c *gin.Context) (string, error)`

Reads request body as string (preserves body for subsequent handlers).

## Request Flow

```
1. Request arrives
   ↓
2. Check if route should be monitored
   ↓
3. Check if route is whitelisted
   ↓
4. Capture request metadata
   ↓
5. Extract user identifier (if configured)
   ↓
6. Execute allocation request (if connected)
   ↓
7. Process original request (c.Next())
   ↓
8. Execute fulfill request (if connected)
   ↓
9. Request completes
```

## Connection Status

The middleware tracks WebSocket connection status:

- `isConnected()`: Checks actual connection status from socket manager
- Updates status automatically before each operation
- Gracefully handles disconnections

## Error Handling

**Allocation Errors:**

- If socket disconnected: Continue normally (no error)
- If socket error: Update status, continue normally
- Only fails if socket connected AND actual error occurs

**Fulfill Errors:**

- Always continues normally (request already processed)
- Errors are logged but don't affect response

## Configuration Updates

Configuration is automatically updated:

- **Initial**: Fetched immediately on `New()`
- **Periodic**: Every 30 seconds in background goroutine
- **Thread-safe**: Uses mutex for updates
- **Non-blocking**: Updates don't block request processing

## Example Usage

```go
package main

import (
    "github.com/gin-gonic/gin"
    "github.com/usageflow/usageflow-go-middleware/pkg/config"
    "github.com/usageflow/usageflow-go-middleware/pkg/middleware"
)

func main() {
    r := gin.Default()

    // Initialize UsageFlow
    uf := middleware.New("your-api-key")

    // Configure routes
    routes := []config.Route{
        {Method: "*", URL: "*"},
    }

    whiteList := []config.Route{
        {Method: "GET", URL: "/health"},
    }

    // Apply middleware
    r.Use(uf.RequestInterceptor(routes, whiteList))

    // Your routes
    r.GET("/api/users", handleUsers)

    r.Run(":8080")
}
```

## Thread Safety

All public methods are thread-safe:

- Uses `sync.RWMutex` for read/write operations
- Safe for concurrent request handling
- Safe for concurrent config updates

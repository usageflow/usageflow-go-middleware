# UsageFlow Go Middleware

[![Go Reference](https://pkg.go.dev/badge/github.com/usageflow/usageflow-go-middleware.svg)](https://pkg.go.dev/github.com/usageflow/usageflow-go-middleware)
[![Go Report Card](https://goreportcard.com/badge/github.com/usageflow/usageflow-go-middleware.svg)](https://goreportcard.com/report/github.com/usageflow/usageflow-go-middleware)

> ⚠️ **Beta Notice**: This package is currently in beta for experimentation. While we strive to maintain stability, breaking changes may occur as we refine the API and features. We recommend testing thoroughly in development environments before deploying to production.

A Go middleware package for integrating UsageFlow API with your Gin web applications. This middleware helps you track and manage API usage, implement rate limiting, and handle authentication seamlessly.

## Installation

```bash
go get github.com/usageflow/usageflow-go-middleware
```

## Quick Start

```go
package main

import (
    "github.com/gin-gonic/gin"
    ufconfig "github.com/usageflow/usageflow-go-middleware/pkg/config"
    ufmiddleware "github.com/usageflow/usageflow-go-middleware/pkg/middleware"
)

func main() {
    // Initialize Gin
    r := gin.Default()

    // Initialize UsageFlow with your API key
    uf := ufmiddleware.New("your-api-key")

    // Define routes to monitor
    routes := []ufconfig.Route{
        {Method: "*", URL: "*"}, // Monitor all routes
    }

    // Define whitelist routes (optional)
    whiteList := []ufconfig.Route{
        // {Method: "*", URL: "*"}, // Uncomment to whitelist all routes
    }

    // Use the middleware
    r.Use(uf.RequestInterceptor(routes, whiteList))

    // Your routes
    r.GET("/api/users", func(c *gin.Context) {
        c.JSON(200, gin.H{"message": "Hello Users!"})
    })

    r.Run(":8080")
}
```

## Configuration

The middleware requires only three configuration points:

1. **API Key**: Your UsageFlow API key
2. **Routes to Monitor**: Which routes should be tracked
3. **Whitelist Routes**: Which routes should be ignored

### 1. API Key

Initialize the middleware with your API key:

```go
uf := ufmiddleware.New("your-api-key")
```

### 2. Routes to Monitor

Define which routes should be monitored. You can use wildcards to monitor all routes:

```go
// Monitor all routes
routes := []ufconfig.Route{
    {Method: "*", URL: "*"},
}

// Or monitor specific routes
routes := []ufconfig.Route{
    {Method: "GET", URL: "/api/v1/users"},
    {Method: "POST", URL: "/api/v1/data"},
}
```

### 3. Whitelist Routes

Define routes that should bypass the middleware. You can use wildcards to whitelist all routes:

```go
// Whitelist all routes
whiteList := []ufconfig.Route{
    {Method: "*", URL: "*"},
}

// Or whitelist specific routes
whiteList := []ufconfig.Route{
    {Method: "GET", URL: "/health"},
    {Method: "GET", URL: "/metrics"},
}
```

## Example

Here's a complete example showing how to use the middleware in a real application:

```go
package main

import (
    "net/http"
    "strconv"

    "github.com/gin-gonic/gin"
    ufconfig "github.com/usageflow/usageflow-go-middleware/pkg/config"
    ufmiddleware "github.com/usageflow/usageflow-go-middleware/pkg/middleware"
)

type User struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

func main() {
    r := gin.Default()

    // Initialize UsageFlow
    uf := ufmiddleware.New("your-api-key")

    // Configure routes
    routes := []ufconfig.Route{
        {Method: "*", URL: "*"}, // Monitor all routes
    }

    whiteList := []ufconfig.Route{
        // {Method: "*", URL: "*"}, // Uncomment to whitelist all routes
    }

    // Use the middleware
    r.Use(uf.RequestInterceptor(routes, whiteList))

    // Your application routes
    r.GET("/users", func(c *gin.Context) {
        c.JSON(http.StatusOK, gin.H{"users": []User{}})
    })

    r.Run(":8080")
}
```

## Release Process

This package uses GitHub Actions to automatically create new releases when changes are pushed to the main branch. The process works as follows:

1. When changes are pushed to the main branch, a GitHub Action automatically:
   - Creates a new git tag based on the version in `go.mod`
   - Creates a new GitHub release
   - Updates the package documentation

To trigger a new release:

1. Update the version in `go.mod`
2. Push your changes to the main branch
3. The GitHub Action will automatically create a new release

## Documentation

For detailed documentation and examples, please visit our [documentation site](https://docs.usageflow.io).

## Release Notes

For detailed release notes and migration guides, please see [RELEASE_NOTES.md](RELEASE_NOTES.md).

## License

This project is licensed under the MIT License - see the LICENSE file for details.

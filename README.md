# UsageFlow Go Middleware

[![Go Reference](https://pkg.go.dev/badge/github.com/usageflow/usageflow-go-middleware.svg)](https://pkg.go.dev/github.com/usageflow/usageflow-go-middleware)
[![Go Report Card](https://goreportcard.com/badge/github.com/usageflow/usageflow-go-middleware.svg)](https://goreportcard.com/report/github.com/usageflow/usageflow-go-middleware)

A powerful Go middleware package for integrating UsageFlow API with your Gin web applications. This middleware helps you track and manage API usage, implement rate limiting, and handle authentication seamlessly.

## Features

- Easy integration with Gin web framework
- Automatic API usage tracking
- Request interception and validation
- JWT token handling
- Configurable route whitelisting
- Thread-safe configuration management
- Automatic config updates
- Endpoint-specific policy management
- Rate limiting per endpoint
- Identity field extraction from various sources
- Request body preservation

## Installation

```bash
go get github.com/usageflow/usageflow-go-middleware
```

## Quick Start

```go
package main

import (
    "github.com/gin-gonic/gin"
    usageflow "github.com/usageflow/usageflow-go-middleware"
)

func main() {
    // Initialize Gin
    r := gin.Default()

    // Initialize UsageFlow with your API key
    uf := usageflow.New("your-api-key")

    // Define routes to monitor (using wildcards to monitor all routes)
    routes := []usageflow.Route{
        {Method: "*", URL: "*"},
    }

    // Define whitelist routes (optional)
    whiteList := []usageflow.Route{
        {Method: "*", URL: "*"},
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

### Routes to Monitor

Define which routes should be monitored by the middleware. You can use wildcards to monitor all routes:

```go
// Monitor all routes
routes := []usageflow.Route{
    {Method: "*", URL: "*"},
}

// Or monitor specific routes
routes := []usageflow.Route{
    {Method: "GET", URL: "/api/v1/users"},
    {Method: "POST", URL: "/api/v1/data"},
}
```

### Whitelist Routes

Define routes that should bypass the middleware. You can use wildcards to whitelist all routes:

```go
// Whitelist all routes
whiteList := []usageflow.Route{
    {Method: "*", URL: "*"},
}

// Or whitelist specific routes
whiteList := []usageflow.Route{
    {Method: "GET", URL: "/health"},
    {Method: "GET", URL: "/metrics"},
}
```

## Documentation

For detailed documentation and examples, please visit our [documentation site](https://docs.usageflow.io).

## Release Notes

For detailed release notes and migration guides, please see [RELEASE_NOTES.md](RELEASE_NOTES.md).

## License

This project is licensed under the MIT License - see the LICENSE file for details.

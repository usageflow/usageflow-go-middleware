# UsageFlow Go Middleware

[![Go Reference](https://pkg.go.dev/badge/github.com/usageflow/usageflow-go-middleware.svg)](https://pkg.go.dev/github.com/usageflow/usageflow-go-middleware)
[![Go Report Card](https://goreportcard.com/badge/github.com/usageflow/usageflow-go-middleware)](https://goreportcard.com/report/github.com/usageflow/usageflow-go-middleware)

**Note: This is a beta version of the UsageFlow Go Middleware. Bugs may be present, and the API is subject to change.**

A Go middleware package for integrating UsageFlow API with your Gin web applications. This middleware helps you track and manage API usage, implement rate limiting, and handle authentication seamlessly.

## Features

- Easy integration with Gin web framework
- Automatic API usage tracking
- Request interception and validation
- JWT token handling
- Configurable route whitelisting
- Thread-safe configuration management
- Automatic config updates

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

    // Initialize UsageFlow
    uf := &usageflow.UsageFlowAPI{}
    uf.Init("your-api-key")

    // Define routes to monitor
    routes := []usageflow.Route{
        {Method: "GET", URL: "/api/users"},
        {Method: "POST", URL: "/api/data"},
    }

    // Define whitelist routes (optional)
    whiteList := []usageflow.Route{
        {Method: "GET", URL: "/health"},
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

The middleware can be configured with various options:

```go
type UsageFlowAPI struct {
    APIKey        string
    ApplicationId string
    ApiConfig     *ApiConfigStrategy
}
```

## Documentation

For detailed documentation and examples, please visit our [documentation site](https://docs.usageflow.io).

### Route Configuration

Routes can be configured with methods and URLs:

```go
routes := []usageflow.Route{
    {Method: "GET", URL: "/api/v1/users"},
    {Method: "POST", URL: "/api/v1/data"},
    {Method: "*", URL: "/api/v1/public/*"}, // Wildcard support
}
```

### Whitelist Routes

Certain routes can be whitelisted to bypass the middleware:

```go
whiteList := []usageflow.Route{
    {Method: "GET", URL: "/health"},
    {Method: "GET", URL: "/metrics"},
}
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the LICENSE file for details.

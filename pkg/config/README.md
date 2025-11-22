# Config Package

The `config` package provides type definitions and data structures for UsageFlow middleware configuration.

## Overview

This package defines the core data structures used throughout the middleware for:

- API configuration strategies
- Endpoint policies
- Route definitions
- Verification responses

## Types

### `ApiConfigStrategy`

Represents the configuration strategy for API endpoints. Matches the `UsageFlowConfig` TypeScript interface.

```go
type ApiConfigStrategy struct {
    Url                   string  // Required: URL pattern to match
    Method                string  // Required: HTTP method (GET, POST, etc.)
    IdentityFieldName     *string // Optional: Field name for user identification
    IdentityFieldLocation *string // Optional: Location of identity field (headers, query, body, etc.)
}
```

**Fields:**

- `Url`: The URL pattern to match (e.g., "/api/users/:id")
- `Method`: HTTP method (e.g., "GET", "POST", "\*" for all)
- `IdentityFieldName`: Optional pointer to the field name containing user identity
- `IdentityFieldLocation`: Optional pointer to where the identity field is located:
  - `"headers"`: HTTP header
  - `"query"`: Query parameter
  - `"path_params"`: URL path parameter
  - `"body"`: Request body field
  - `"bearer_token"`: JWT token claim

### `ApplicationEndpointPolicy`

Represents an endpoint-specific policy that can override base configuration.

```go
type ApplicationEndpointPolicy struct {
    PolicyId           string
    AccountId          string
    ApplicationId      string
    EndpointPattern    string
    EndpointMethod     string
    IdentityField      string
    IdentityLocation   string
    RateLimit          int
    RateLimitInterval  string
    MeteringExpression string
    MeteringTrigger    string
    StripePriceId      string
    StripeCustomerId   string
    CreatedAt          int64
    UpdatedAt          int64
}
```

### `Route`

Defines a route configuration for monitoring or whitelisting.

```go
type Route struct {
    Method string // HTTP method or "*" for all
    URL    string // URL pattern or "*" for all
}
```

**Usage:**

- `{Method: "*", URL: "*"}` - Monitor/whitelist all routes
- `{Method: "GET", URL: "/api/users"}` - Specific route

### `PolicyListResponse`

Response structure for policy list API calls.

```go
type PolicyListResponse struct {
    Policies []ApiConfigStrategy
    Total    int
}
```

### `PolicyResponse`

Wraps policy list response with data field.

```go
type PolicyResponse struct {
    Data PolicyListResponse
}
```

### `VerifyResponse`

Response from the verification endpoint.

```go
type VerifyResponse struct {
    AccountId     string
    ApplicationId string
}
```

## Usage Examples

### Creating a Route Configuration

```go
import "github.com/usageflow/usageflow-go-middleware/pkg/config"

// Monitor all routes
routes := []config.Route{
    {Method: "*", URL: "*"},
}

// Monitor specific routes
routes := []config.Route{
    {Method: "GET", URL: "/api/users"},
    {Method: "POST", URL: "/api/orders"},
}
```

### Working with ApiConfigStrategy

```go
config := config.ApiConfigStrategy{
    Url:                   "/api/users/:id",
    Method:                "GET",
    IdentityFieldName:     stringPtr("userId"),
    IdentityFieldLocation: stringPtr("headers"),
}

// Check if identity fields are configured
if config.IdentityFieldName != nil && config.IdentityFieldLocation != nil {
    // Use identity extraction
}
```

## JSON Serialization

All types support JSON serialization/deserialization with proper tags:

- `json` tags for JSON field names
- `bson` tags for MongoDB compatibility (if needed)
- `omitempty` for optional fields

## Thread Safety

Types in this package are **not** thread-safe. If shared across goroutines, external synchronization is required.

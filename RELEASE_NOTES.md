# Release Notes

## v2.3.0 (Latest)

### New Features

#### Cookie Extraction Support

- **Standard Cookie Extraction**: Added support for extracting user identifiers from HTTP cookies. You can now use cookies as an identity field location in your API configuration.
- **JWT Cookie Support**: Added support for extracting claims from JWT tokens stored in cookies using the format `[technique=jwt]cookieName[pick=claim]`.
- **Flexible Cookie Naming**: Supports both direct cookie name access and `cookie.cookieName` format for clarity.
- **Case-Insensitive Matching**: Cookie name matching is case-insensitive, making it more robust across different browsers and clients.

#### Cookie Extraction Methods

1. **Standard Cookie Extraction**:
   - Extract identifier directly from cookie value
   - Supports format: `cookie.cookieName` or just `cookieName`
   - Example: Extract `sessionId` cookie value as user identifier

2. **JWT Cookie Extraction**:
   - Extract specific claims from JWT tokens stored in cookies
   - Format: `[technique=jwt]cookieName[pick=claim]`
   - Example: Extract `userId` claim from JWT stored in `sessionToken` cookie

### API Changes

#### New Identity Field Location

Added `"cookie"` as a new identity field location option:

```go
// Standard cookie extraction
{
    Url:                   "/api/session",
    Method:                "GET",
    IdentityFieldName:     stringPtr("sessionId"),
    IdentityFieldLocation: stringPtr("cookie"),
}

// JWT cookie extraction
{
    Url:                   "/api/auth",
    Method:                "GET",
    IdentityFieldName:     stringPtr("[technique=jwt]sessionToken[pick=userId]"),
    IdentityFieldLocation: stringPtr("cookie"),
}
```

#### New Helper Functions

- **`GetCookieValue()`**: Extracts a specific cookie value from the Cookie header (case-insensitive)
- **`ParseJwtCookieField()`**: Parses JWT cookie field format and returns cookie name and claim

### Usage Examples

#### Standard Cookie Extraction

```go
// Configuration
{
    Url:                   "/api/users",
    Method:                "GET",
    IdentityFieldName:     stringPtr("sessionId"),
    IdentityFieldLocation: stringPtr("cookie"),
}

// Request with cookie: Cookie: sessionId=user-123
// Extracted identifier: "user-123" (transformed to "user_123")
```

#### JWT Cookie Extraction

```go
// Configuration
{
    Url:                   "/api/protected",
    Method:                "GET",
    IdentityFieldName:     stringPtr("[technique=jwt]authToken[pick=sub]"),
    IdentityFieldLocation: stringPtr("cookie"),
}

// Request with cookie: Cookie: authToken=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
// Extracted identifier: Value of "sub" claim from JWT
```

#### Cookie with Prefix Format

```go
// Configuration
{
    Url:                   "/api/session",
    Method:                "GET",
    IdentityFieldName:     stringPtr("cookie.authToken"),
    IdentityFieldLocation: stringPtr("cookie"),
}

// Request with cookie: Cookie: authToken=token-value
// Extracted identifier: "token-value" (transformed to "token_value")
```

### Behavior

- **Cookie Parsing**: Cookies are parsed from the `Cookie` header (case-insensitive header name)
- **Value Transformation**: Cookie values are transformed using `TransformToLedgerId()` to ensure valid ledger ID format
- **JWT Decoding**: JWT cookies are decoded without signature verification (unverified) to extract claims
- **Error Handling**: Returns empty identifier if cookie is missing, invalid, or JWT decoding fails

### Technical Details

- **Cookie Header Support**: Handles both `Cookie` and `cookie` header names
- **Multiple Cookies**: Correctly parses cookies from multi-cookie header strings
- **Value Handling**: Properly handles cookie values that contain `=` characters
- **Case-Insensitive**: Cookie name matching is case-insensitive for better compatibility

### Testing

Added comprehensive test coverage:
- 22 test cases for `GetUserPrefix()` covering all cookie extraction scenarios
- 8 test cases for `GetCookieValue()` helper function
- 9 test cases for `ParseJwtCookieField()` helper function

### Migration Guide

No migration required. This is a new feature that doesn't affect existing functionality. To use cookie extraction:

1. Update your API configuration in UsageFlow dashboard to use `"cookie"` as `identityFieldLocation`
2. Set `identityFieldName` to either:
   - Cookie name (e.g., `"sessionId"`)
   - Cookie name with prefix (e.g., `"cookie.sessionId"`)
   - JWT cookie format (e.g., `"[technique=jwt]sessionToken[pick=userId]"`)

### Benefits

- **Session-Based Identification**: Easily identify users from session cookies
- **JWT Cookie Support**: Extract user information from JWT tokens stored in cookies
- **Flexible Configuration**: Multiple ways to specify cookie extraction (direct name, prefix format, JWT format)
- **Robust Parsing**: Handles edge cases like multiple cookies, case variations, and special characters

---

## v2.2.0

### Major Simplification: Server-Driven Configuration

This release significantly simplifies middleware initialization and configuration management by moving all route configuration to the server side. The middleware now only requires an API key to get started.

### New Features

#### Simplified Middleware Initialization

- **API Key Only**: Middleware initialization now only requires an API key - no need to manually configure routes or whitelist endpoints
- **Automatic Route Fetching**: Monitoring paths and whitelist endpoints are automatically fetched from UsageFlow servers every 30 seconds
- **Server-Driven Configuration**: All route configuration is now managed centrally on UsageFlow servers, eliminating the need for local route management

#### Server-Driven Route Configuration

- **Automatic Route Management**: Added `FetchApplicationConfig()` method that automatically fetches monitoring paths and whitelist endpoints from UsageFlow servers
- **Dynamic Route Updates**: Routes are automatically updated every 30 seconds from the server, ensuring your middleware always uses the latest configuration
- **Centralized Configuration**: Route configuration is now managed in one place (UsageFlow dashboard), making it easier to update and maintain across multiple services

### API Changes

#### Server-Driven Route Configuration

- **Automatic Route Fetching**: Routes and whitelist endpoints are now automatically fetched from UsageFlow servers. The `RequestInterceptor()` API remains the same (no parameters), but routes are now managed server-side:

  ```go
  // Usage remains the same
  uf.RequestInterceptor()
  ```

- **Zero Local Configuration**: No need to manually define routes in code - everything is fetched from the server automatically

#### New Methods

- **`FetchApplicationConfig()`**: Automatically fetches application configuration including monitoring paths and whitelist endpoints from UsageFlow servers

#### New Fields

- **`WhitelistEndpoints`**: Stores whitelist endpoints fetched from server
- **`MonitoringPaths`**: Stores monitoring paths fetched from server
- **`monitoringPathsMap`**: Internal map for efficient route lookup
- **`whitelistEndpointsMap`**: Internal map for efficient whitelist lookup

### Improvements

- **Simplified API**: Reduced complexity by removing the need to manually manage routes
- **Automatic Updates**: Route configuration is automatically synchronized with UsageFlow servers
- **Code Cleanup**: Removed all commented-out code for cleaner codebase
- **Background Updates**: Application config fetching runs in background goroutines to avoid blocking initialization

### Migration Guide

For users upgrading from v2.1.0:

1. **No code changes required**: The `RequestInterceptor()` API remains the same - no parameters needed

2. **Configure routes in UsageFlow dashboard**: Routes and whitelist endpoints should now be configured in your UsageFlow dashboard instead of being managed locally. The middleware will automatically fetch and use these routes.

3. **No action needed for**:
   - Basic middleware initialization (still just needs API key)
   - `RequestInterceptor()` usage (API unchanged)
   - Other middleware functionality (all remains the same)

### Benefits

- **Easier Setup**: Only need to provide API key - no route configuration needed
- **Centralized Management**: Update routes in one place (UsageFlow dashboard) instead of redeploying code
- **Automatic Synchronization**: Routes are always up-to-date with server configuration
- **Reduced Code Complexity**: Less code to write and maintain

### Technical Details

- **Config Update Interval**: Application config is fetched every 30 seconds along with API config and blocked endpoints
- **Thread-Safe**: All route maps are protected by mutex locks for concurrent access
- **Automatic Initialization**: Application config fetching starts automatically when middleware is initialized
- **Graceful Degradation**: Middleware continues to function even if server is temporarily unavailable

---

## v2.1.0

### New Features

#### Async Operation Support

- **Fire-and-Forget for Non-Rate-Limited Requests**: Non-rate-limited requests now use asynchronous fire-and-forget pattern, generating allocation IDs locally and sending requests without waiting for server response. This significantly improves request latency for high-throughput scenarios.
- **Rate-Limited Request Handling**: Rate-limited requests continue to use synchronous async pattern (waiting for server response) to ensure proper quota validation.
- **Automatic Rate Limit Detection**: Added `HasRateLimit` field to `ApiConfigStrategy` to automatically detect and handle rate-limited endpoints differently from non-rate-limited ones.
- **UUID-Based Allocation IDs**: Integrated `google/uuid` package for generating unique allocation IDs locally when using fire-and-forget pattern.

#### Blocked Endpoints Management

- **Automatic Blocked Endpoints Fetching**: New `FetchBlockedEndpoints()` method automatically fetches blocked endpoints from the server every 30 seconds.
- **Endpoint Blocking Detection**: Middleware now checks if an endpoint is blocked before processing requests, returning appropriate HTTP status codes.
- **Blocked Endpoints Storage**: Blocked endpoints are stored in a thread-safe map for fast lookup during request processing.
- **Identity-Based Blocking**: Blocked endpoints can be identity-specific, allowing fine-grained control per user or entity.

#### Enhanced Error Handling

- **Specific HTTP Status Codes**: Improved error responses with appropriate HTTP status codes:
  - `403 Forbidden`: Returned when an endpoint is blocked by policy rule
  - `402 Payment Required`: Returned when user has insufficient resources
  - `400 Bad Request`: Returned when request allocation fails
- **Structured Error Messages**: Error responses now include structured JSON with `error` and `message` fields for better client-side handling.
- **Endpoint Blocked Detection**: Automatic detection and handling of blocked endpoints with clear error messaging.

#### Configuration Enhancements

- **Rate Limit Configuration**: Added `HasRateLimit` boolean field to `ApiConfigStrategy` to indicate if an endpoint has rate limiting enabled.
- **Blocked Endpoints Types**: New types added:
  - `BlockedEndpointsResponse`: Response structure for blocked endpoints
  - `BlockedEndpoints`: Individual blocked endpoint structure with URL, method, and identity

#### Production Readiness

- **Background Config Updates**: Config and blocked endpoints fetching now run in background goroutines to avoid blocking initialization.

### API Changes

#### Modified Methods

1. **`GetUserPrefix()` Return Value**: Now returns both user identifier suffix and rate limit status:

   ```go
   // Old signature
   func (u *UsageFlowAPI) GetUserPrefix(c *gin.Context, method, url string) string

   // New signature
   func (u *UsageFlowAPI) GetUserPrefix(c *gin.Context, method, url string) (string, bool)
   ```

2. **`ExecuteRequestWithMetadata()` Parameter**: Added `rateLimited` parameter:

   ```go
   // Old signature
   func (u *UsageFlowAPI) ExecuteRequestWithMetadata(ledgerId, method, url string, metadata map[string]interface{}, c *gin.Context) (bool, error)

   // New signature
   func (u *UsageFlowAPI) ExecuteRequestWithMetadata(ledgerId, method, url string, metadata map[string]interface{}, c *gin.Context, rateLimited bool) (bool, error)
   ```

3. **`allocateRequest()` Parameter**: Added `rateLimited` parameter to control async behavior.

4. **`useAllocationRequest()` Parameter**: Added `rateLimited` parameter to control async behavior.

#### New Methods

- **`FetchBlockedEndpoints()`**: Fetches blocked endpoints from the server and updates the internal blocked endpoints map.

#### New Fields

- **`BlockedEndpoints`**: New field in `UsageFlowAPI` struct to store blocked endpoints map.
- **`HasRateLimit`**: New field in `ApiConfigStrategy` to indicate rate limiting status.
- **`AllocationID`**: New optional field in `RequestForAllocation` for fire-and-forget requests.

### Behavior Changes

1. **Async Request Handling**: Non-rate-limited requests no longer wait for server response, improving latency.
2. **Automatic Blocked Endpoints Updates**: Blocked endpoints are automatically fetched and updated every 30 seconds.
3. **Endpoint Blocking**: Requests to blocked endpoints are automatically rejected with 403 status code.
4. **Error Response Format**: Error responses now use structured JSON format with specific error codes.

### Migration Guide

For users upgrading from v2.0.0:

1. **Update `GetUserPrefix()` calls**:

   ```go
   // Old way
   suffix := uf.GetUserPrefix(c, method, url)

   // New way
   suffix, rateLimited := uf.GetUserPrefix(c, method, url)
   ```

2. **Update `ExecuteRequestWithMetadata()` calls**:

   ```go
   // Old way
   success, err := uf.ExecuteRequestWithMetadata(ledgerId, method, url, metadata, c)

   // New way
   success, err := uf.ExecuteRequestWithMetadata(ledgerId, method, url, metadata, c, rateLimited)
   ```

3. **Handle rate limit status**: The rate limit status is now available in the context:

   ```go
   rateLimited, _ := c.Get("rateLimited")
   isRateLimited, ok := rateLimited.(bool)
   ```

4. **No action needed for**:
   - Basic middleware usage (automatic handling)
   - Route configuration
   - Whitelist configuration

### Performance Improvements

- **Reduced Latency**: Fire-and-forget pattern for non-rate-limited requests eliminates wait time for server response.
- **Background Updates**: Config and blocked endpoints fetching run in background, not blocking request processing.
- **Fast Blocked Endpoint Lookup**: Thread-safe map-based storage for O(1) blocked endpoint lookups.

### Bug Fixes

- Fixed WebSocket URL to use production endpoint instead of localhost
- Improved error handling for blocked endpoints
- Fixed rate limit detection logic

### Dependencies

- Added `github.com/google/uuid v1.6.0` for UUID generation

---

## v2.0.0

### Major Changes

This is a major version release with significant architectural improvements and breaking changes. The middleware now uses WebSocket-based communication for better real-time performance and reliability.

### New Features

#### WebSocket-Based Communication

- **Real-time WebSocket Connection**: Replaced HTTP-based communication with WebSocket for faster, more efficient request handling
- **Connection Pooling**: Implemented connection pooling with configurable pool size (default: 10 connections)
- **Automatic Reconnection**: Added robust reconnection logic with exponential backoff when connections are lost
- **Connection Health Monitoring**: Implemented ping/pong mechanism to detect and handle dead connections proactively
- **Graceful Degradation**: Middleware continues to function normally when WebSocket is disconnected, ensuring application availability

#### Configuration Management

- **Simplified Config Structure**: Updated `ApiConfigStrategy` to match new `UsageFlowConfig` interface:
  - Required fields: `url`, `method`
  - Optional fields: `identityFieldName`, `identityFieldLocation`
- **Automatic Config Updates**: Config is automatically fetched and updated every 30 seconds
- **Improved Config Matching**: Configs are matched by method and URL for precise routing

#### Error Handling & Reliability

- **Enhanced Error Handling**: Added proper error handling for WebSocket responses, including error field checking
- **Connection Status Tracking**: Real-time connection status tracking with automatic updates
- **Request Continuity**: Requests continue processing even when socket is temporarily unavailable
- **Better Timeout Management**: Improved read deadline handling to prevent premature connection drops

#### Code Quality Improvements

- **Removed Deprecated Code**: Cleaned up unused methods (`Init`, `GetApplicationEndpointPolicies`, `updateConfig`)
- **Simplified API**: Streamlined middleware initialization - config updater starts automatically
- **Better Resource Management**: Improved mutex usage and lock management for better concurrency

### Breaking Changes

#### API Changes

1. **Removed `Init()` Method**: Initialization now happens automatically in `New()`

   ```go
   // Old way
   uf := ufmiddleware.New("api-key")
   uf.Init("api-key")

   // New way (automatic)
   uf := ufmiddleware.New("api-key")
   ```

2. **Removed `GetApplicationEndpointPolicies()` Method**: This method is no longer available

3. **Config Structure Changed**: `ApiConfigStrategy` now has a simplified structure:

   ```go
   // Old structure (removed fields)
   type ApiConfigStrategy struct {
       ID, Name, AccountId, ApplicationId string
       // ... many other fields
   }

   // New structure
   type ApiConfigStrategy struct {
       Url                   string
       Method                string
       IdentityFieldName     *string  // Optional
       IdentityFieldLocation *string  // Optional
   }
   ```

4. **Identity Field Location Change**: Changed from `"header"` to `"headers"` for header-based identity extraction

#### Behavior Changes

1. **Automatic Config Updates**: Config is now automatically updated every 30 seconds (previously required manual calls)
2. **WebSocket Communication**: All communication now uses WebSocket instead of HTTP
3. **Graceful Degradation**: Middleware no longer fails requests when socket is disconnected - it continues processing normally

### Migration Guide

For users upgrading from v1.x:

1. **Remove `Init()` calls**:

   ```go
   // Remove this
   uf.Init("api-key")
   ```

2. **Update identity field location** if using header-based extraction:

   ```go
   // If your config uses "header", update to "headers"
   // This is handled automatically in the new config structure
   ```

3. **Handle optional identity fields**:

   ```go
   // Identity fields are now optional pointers
   if cfg.IdentityFieldName != nil && cfg.IdentityFieldLocation != nil {
       // Use identity extraction
   }
   ```

4. **No action needed for**:
   - Route configuration (still works the same)
   - Whitelist configuration (still works the same)
   - Basic middleware usage (API remains compatible)

### Bug Fixes

- Fixed connection stability issues that caused frequent disconnections
- Fixed read deadline management that caused premature connection drops
- Fixed connection status not being updated properly
- Fixed lock management issues in `FetchApiConfig()`
- Fixed error handling in socket responses

### Performance Improvements

- Reduced latency with WebSocket communication
- Improved connection stability with ping/pong mechanism
- Better resource utilization with connection pooling
- Optimized config updates to run in background

### Technical Details

- **WebSocket Library**: Uses `gorilla/websocket` for WebSocket communication
- **Connection Pool**: Default pool size of 10 connections with least-busy selection
- **Ping Interval**: 30 seconds
- **Pong Timeout**: 60 seconds
- **Config Update Interval**: 30 seconds
- **Reconnection**: Exponential backoff with max 5 retries

---

## v0.3.0 (March 30, 2024)

### New Features

#### Endpoint-Specific Policy Management

- Added support for endpoint-specific policies that override base configuration
- Implemented policy-based rate limiting per endpoint
- Added support for multiple identity field locations (header, query, path, body, JWT)
- Added automatic policy updates every 30 seconds
- Implemented policy pattern matching with support for URL parameters

#### Request Body Handling

- Improved request body handling to prevent data loss
- Added request body preservation for subsequent handlers
- Implemented proper body restoration after reading
- Fixed issues with body consumption in handlers

#### Performance Improvements

- Optimized policy lookup using map-based storage
- Enhanced thread safety with proper mutex usage
- Improved error handling and logging
- Reduced memory allocations in policy matching

### Bug Fixes

#### Request Body Issues

- Fixed request body consumption issue that affected subsequent handlers
- Resolved body reading conflicts in multiple middleware layers
- Fixed body restoration after JSON parsing

#### Policy Management

- Fixed race conditions in policy updates
- Resolved policy matching edge cases with URL patterns
- Fixed policy map initialization issues
- Corrected policy update timing issues

#### General Improvements

- Enhanced error messages for better debugging
- Improved logging for policy updates
- Fixed memory leaks in long-running applications
- Added proper cleanup in error cases

### Breaking Changes

- Changed policy storage from slice to map for better performance
- Updated policy matching logic to be more strict
- Modified request body handling to require explicit restoration

### Migration Guide

For users upgrading from v1.0.0:

1. Update your policy handling code to use the new map-based storage:

```go
// Old way
policies := uf.ApplicationEndpointPolicies.Data.Items

// New way
policyMap := uf.policyMap
```

2. Update your request body handling:

```go
// Old way
if err := c.ShouldBindJSON(&bodyMap); err == nil {
    // Process body
}

// New way
bodyBytes, err := io.ReadAll(c.Request.Body)
if err == nil {
    c.Request.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))
    // Process body
}
```

3. Update your policy matching code to use the new pattern matching:

```go
// Old way
if policy.EndpointPattern == url {
    // Process policy
}

// New way
pattern := strings.ReplaceAll(policy.EndpointPattern, ":id", "[^/]+")
pattern = "^" + pattern + "$"
matched, err := regexp.MatchString(pattern, url)
if err == nil && matched {
    // Process policy
}
```

## v1.0.0 (Initial Release)

### Features

- Basic middleware functionality
- Route monitoring
- Whitelist support
- Basic configuration management
- JWT token handling
- Route whitelisting
- Basic request interception

# Release Notes

## v2.0.0 (Latest)

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

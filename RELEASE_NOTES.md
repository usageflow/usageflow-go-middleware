# Release Notes

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

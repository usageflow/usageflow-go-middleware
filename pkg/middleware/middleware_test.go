package middleware

import (
	"bytes"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/config"
)

func TestNew(t *testing.T) {
	api := New("test-api-key")
	assert.NotNil(t, api)
	// APIKey is not set in New(), only passed to socketManager
	assert.NotNil(t, api.socketManager)
	assert.NotNil(t, api.policyMap)
}

func TestUsageFlowAPI_GuessLedgerId(t *testing.T) {
	gin.SetMode(gin.TestMode)

	api := New("test-api-key")
	defer api.socketManager.Close()

	tests := []struct {
		name     string
		method   string
		url      string
		expected string
	}{
		{
			name:     "GET request",
			method:   "GET",
			url:      "/api/users",
			expected: "GET /api/users",
		},
		{
			name:     "POST request",
			method:   "POST",
			url:      "/api/orders",
			expected: "POST /api/orders",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(tt.method, tt.url, nil)

			result := api.GuessLedgerId(c)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUsageFlowAPI_GetUserPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	api := New("test-api-key")
	defer api.socketManager.Close()

	tests := []struct {
		name        string
		method      string
		url         string
		config      []config.ApiConfigStrategy
		setup       func(*gin.Context)
		expected    string
		rateLimited bool
		description string
	}{
		// Headers extraction
		{
			name:   "extract from headers",
			method: "GET",
			url:    "/api/users",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/users",
					Method:                "GET",
					IdentityFieldName:     stringPtr("userId"),
					IdentityFieldLocation: stringPtr("headers"),
				},
			},
			setup: func(c *gin.Context) {
				c.Request.Header.Set("userId", "user-123")
			},
			expected:    "user_123",
			rateLimited: false,
			description: "Should extract identifier from header",
		},
		{
			name:   "extract from headers case insensitive",
			method: "GET",
			url:    "/api/users",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/users",
					Method:                "GET",
					IdentityFieldName:     stringPtr("X-User-Id"),
					IdentityFieldLocation: stringPtr("headers"),
				},
			},
			setup: func(c *gin.Context) {
				c.Request.Header.Set("x-user-id", "user-456")
			},
			expected:    "user_456",
			rateLimited: false,
			description: "Should extract identifier from header (case insensitive)",
		},

		// Query extraction
		{
			name:   "extract from query",
			method: "POST",
			url:    "/api/orders",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/orders",
					Method:                "POST",
					IdentityFieldName:     stringPtr("orderId"),
					IdentityFieldLocation: stringPtr("query"),
				},
			},
			setup: func(c *gin.Context) {
				c.Request.URL.RawQuery = "orderId=order-789"
			},
			expected:    "order_789",
			rateLimited: false,
			description: "Should extract identifier from query parameter",
		},
		{
			name:   "extract from query_params",
			method: "GET",
			url:    "/api/search",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/search",
					Method:                "GET",
					IdentityFieldName:     stringPtr("sessionId"),
					IdentityFieldLocation: stringPtr("query_params"),
				},
			},
			setup: func(c *gin.Context) {
				c.Request.URL.RawQuery = "sessionId=session-abc"
			},
			expected:    "session_abc",
			rateLimited: false,
			description: "Should extract identifier from query_params (same as query)",
		},

		// Path params extraction
		{
			name:   "extract from path_params",
			method: "GET",
			url:    "/api/users/:userId",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/users/:userId",
					Method:                "GET",
					IdentityFieldName:     stringPtr("userId"),
					IdentityFieldLocation: stringPtr("path_params"),
				},
			},
			setup: func(c *gin.Context) {
				c.Params = gin.Params{gin.Param{Key: "userId", Value: "user-999"}}
			},
			expected:    "user_999",
			rateLimited: false,
			description: "Should extract identifier from path parameter",
		},

		// Body extraction
		{
			name:   "extract from body",
			method: "POST",
			url:    "/api/create",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/create",
					Method:                "POST",
					IdentityFieldName:     stringPtr("email"),
					IdentityFieldLocation: stringPtr("body"),
				},
			},
			setup: func(c *gin.Context) {
				body := `{"email":"test@example.com","name":"Test User"}`
				c.Request = httptest.NewRequest("POST", "/api/create", bytes.NewBufferString(body))
				c.Request.Header.Set("Content-Type", "application/json")
			},
			expected:    "test_example_com",
			rateLimited: false,
			description: "Should extract identifier from request body JSON",
		},
		{
			name:   "extract from body nested field",
			method: "POST",
			url:    "/api/create",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/create",
					Method:                "POST",
					IdentityFieldName:     stringPtr("user.id"),
					IdentityFieldLocation: stringPtr("body"),
				},
			},
			setup: func(c *gin.Context) {
				body := `{"user":{"id":"user-123","name":"Test"}}`
				c.Request = httptest.NewRequest("POST", "/api/create", bytes.NewBufferString(body))
				c.Request.Header.Set("Content-Type", "application/json")
			},
			expected:    "",
			rateLimited: false,
			description: "Body extraction doesn't support dot notation (returns empty)",
		},

		// Bearer token extraction
		{
			name:   "extract from bearer_token",
			method: "GET",
			url:    "/api/protected",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/protected",
					Method:                "GET",
					IdentityFieldName:     stringPtr("userId"),
					IdentityFieldLocation: stringPtr("bearer_token"),
				},
			},
			setup: func(c *gin.Context) {
				jwtToken := createTestJWT(`{"userId":"jwt-user-123","email":"jwt@example.com"}`)
				c.Request.Header.Set("Authorization", "Bearer "+jwtToken)
			},
			expected:    "jwt_user_123",
			rateLimited: false,
			description: "Should extract identifier from JWT bearer token claim",
		},
		{
			name:   "extract from bearer_token with sub claim",
			method: "GET",
			url:    "/api/protected",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/protected",
					Method:                "GET",
					IdentityFieldName:     stringPtr("sub"),
					IdentityFieldLocation: stringPtr("bearer_token"),
				},
			},
			setup: func(c *gin.Context) {
				jwtToken := createTestJWT(`{"sub":"sub-123","email":"test@example.com"}`)
				c.Request.Header.Set("Authorization", "Bearer "+jwtToken)
			},
			expected:    "sub_123",
			rateLimited: false,
			description: "Should extract sub claim from JWT bearer token",
		},

		// Cookie extraction - standard
		{
			name:   "extract from cookie standard",
			method: "GET",
			url:    "/api/session",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/session",
					Method:                "GET",
					IdentityFieldName:     stringPtr("sessionId"),
					IdentityFieldLocation: stringPtr("cookie"),
				},
			},
			setup: func(c *gin.Context) {
				c.Request.Header.Set("Cookie", "sessionId=session-123; other=value")
			},
			expected:    "session_123",
			rateLimited: false,
			description: "Should extract identifier from standard cookie",
		},
		{
			name:   "extract from cookie with cookie. prefix",
			method: "GET",
			url:    "/api/session",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/session",
					Method:                "GET",
					IdentityFieldName:     stringPtr("cookie.authToken"),
					IdentityFieldLocation: stringPtr("cookie"),
				},
			},
			setup: func(c *gin.Context) {
				c.Request.Header.Set("Cookie", "authToken=token-456; sessionId=session-123")
			},
			expected:    "token_456",
			rateLimited: false,
			description: "Should extract identifier from cookie with cookie. prefix",
		},
		{
			name:   "extract from cookie case insensitive",
			method: "GET",
			url:    "/api/session",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/session",
					Method:                "GET",
					IdentityFieldName:     stringPtr("SessionId"),
					IdentityFieldLocation: stringPtr("cookie"),
				},
			},
			setup: func(c *gin.Context) {
				c.Request.Header.Set("Cookie", "sessionid=session-789")
			},
			expected:    "session_789",
			rateLimited: false,
			description: "Should extract identifier from cookie (case insensitive)",
		},

		// Cookie extraction - JWT format
		{
			name:   "extract from cookie JWT format",
			method: "GET",
			url:    "/api/auth",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/auth",
					Method:                "GET",
					IdentityFieldName:     stringPtr("[technique=jwt]sessionToken[pick=userId]"),
					IdentityFieldLocation: stringPtr("cookie"),
				},
			},
			setup: func(c *gin.Context) {
				jwtToken := createTestJWT(`{"userId":"cookie-jwt-user-123","email":"cookie@example.com"}`)
				c.Request.Header.Set("Cookie", "sessionToken="+jwtToken)
			},
			expected:    "cookie_jwt_user_123",
			rateLimited: false,
			description: "Should extract identifier from JWT cookie with claim extraction",
		},
		{
			name:   "extract from cookie JWT format with sub claim",
			method: "GET",
			url:    "/api/auth",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/auth",
					Method:                "GET",
					IdentityFieldName:     stringPtr("[technique=jwt]authToken[pick=sub]"),
					IdentityFieldLocation: stringPtr("cookie"),
				},
			},
			setup: func(c *gin.Context) {
				jwtToken := createTestJWT(`{"sub":"cookie-sub-456","email":"test@example.com"}`)
				c.Request.Header.Set("Cookie", "authToken="+jwtToken+"; other=value")
			},
			expected:    "cookie_sub_456",
			rateLimited: false,
			description: "Should extract sub claim from JWT cookie",
		},
		{
			name:   "extract from cookie JWT format invalid JWT",
			method: "GET",
			url:    "/api/auth",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/auth",
					Method:                "GET",
					IdentityFieldName:     stringPtr("[technique=jwt]sessionToken[pick=userId]"),
					IdentityFieldLocation: stringPtr("cookie"),
				},
			},
			setup: func(c *gin.Context) {
				c.Request.Header.Set("Cookie", "sessionToken=invalid-jwt-token")
			},
			expected:    "",
			rateLimited: false,
			description: "Should return empty when JWT cookie is invalid",
		},

		// Rate limiting
		{
			name:   "rate limited flag",
			method: "GET",
			url:    "/api/limited",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/limited",
					Method:                "GET",
					IdentityFieldName:     stringPtr("userId"),
					IdentityFieldLocation: stringPtr("headers"),
					HasRateLimit:          true,
				},
			},
			setup: func(c *gin.Context) {
				c.Request.Header.Set("userId", "limited-user")
			},
			expected:    "limited_user",
			rateLimited: true,
			description: "Should set rateLimited flag when HasRateLimit is true",
		},

		// Edge cases
		{
			name:   "no matching config",
			method: "DELETE",
			url:    "/api/items",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/users",
					Method:                "GET",
					IdentityFieldName:     stringPtr("userId"),
					IdentityFieldLocation: stringPtr("headers"),
				},
			},
			setup:       func(c *gin.Context) {},
			expected:    "",
			rateLimited: false,
			description: "Should return empty when no matching config found",
		},
		{
			name:   "config without identity fields",
			method: "GET",
			url:    "/api/users",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/users",
					Method:                "GET",
					IdentityFieldName:     nil,
					IdentityFieldLocation: nil,
				},
			},
			setup:       func(c *gin.Context) {},
			expected:    "",
			rateLimited: false,
			description: "Should return empty when identity fields are not configured",
		},
		{
			name:   "missing header value",
			method: "GET",
			url:    "/api/users",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/users",
					Method:                "GET",
					IdentityFieldName:     stringPtr("userId"),
					IdentityFieldLocation: stringPtr("headers"),
				},
			},
			setup:       func(c *gin.Context) {},
			expected:    "",
			rateLimited: false,
			description: "Should return empty when header value is missing",
		},
		{
			name:   "missing cookie",
			method: "GET",
			url:    "/api/session",
			config: []config.ApiConfigStrategy{
				{
					Url:                   "/api/session",
					Method:                "GET",
					IdentityFieldName:     stringPtr("sessionId"),
					IdentityFieldLocation: stringPtr("cookie"),
				},
			},
			setup:       func(c *gin.Context) {},
			expected:    "",
			rateLimited: false,
			description: "Should return empty when cookie is missing",
		},
		{
			name:        "empty config",
			method:      "GET",
			url:         "/api/users",
			config:      []config.ApiConfigStrategy{},
			setup:       func(c *gin.Context) {},
			expected:    "",
			rateLimited: false,
			description: "Should return empty when config is empty",
		},
		{
			name:        "nil config",
			method:      "GET",
			url:         "/api/users",
			config:      nil,
			setup:       func(c *gin.Context) {},
			expected:    "",
			rateLimited: false,
			description: "Should return empty when config is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := New("test-api-key")
			defer api.socketManager.Close()

			// Set up config for this test
			api.mu.Lock()
			api.ApiConfig = tt.config
			api.mu.Unlock()

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(tt.method, tt.url, nil)
			tt.setup(c)

			result, rateLimited := api.GetUserPrefix(c, tt.method, tt.url)
			assert.Equal(t, tt.expected, result, tt.description)
			assert.Equal(t, tt.rateLimited, rateLimited, "Rate limited flag should match")
		})
	}
}

func TestUsageFlowAPI_collectRequestMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	api := New("test-api-key")
	api.ApplicationId = "app-123"
	defer api.socketManager.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/users", bytes.NewBufferString(`{"name":"test"}`))
	c.Request.Header.Set("User-Agent", "test-agent")
	c.Request.Header.Set("Authorization", "Bearer token123")

	metadata := api.collectRequestMetadata(c)

	assert.Equal(t, "app-123", metadata["applicationId"])
	assert.Equal(t, "POST", metadata["method"])
	assert.Equal(t, "test-agent", metadata["userAgent"])
	assert.NotNil(t, metadata["timestamp"])
	assert.NotNil(t, metadata["headers"])
}

func TestIsWhitelisted(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		url       string
		whitelist map[string]map[string]bool
		expected  bool
	}{
		{
			name:   "exact match",
			method: "GET",
			url:    "/api/health",
			whitelist: map[string]map[string]bool{
				"GET": {"/api/health": true},
			},
			expected: true,
		},
		{
			name:   "wildcard method",
			method: "POST",
			url:    "/api/health",
			whitelist: map[string]map[string]bool{
				"*": {"/api/health": true},
			},
			expected: true,
		},
		{
			name:   "wildcard URL",
			method: "GET",
			url:    "/api/health",
			whitelist: map[string]map[string]bool{
				"GET": {"*": true},
			},
			expected: true,
		},
		{
			name:   "not whitelisted",
			method: "GET",
			url:    "/api/users",
			whitelist: map[string]map[string]bool{
				"GET": {"/api/health": true},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isWhitelisted(tt.method, tt.url, tt.whitelist)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsRouteMonitored(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		url      string
		routes   map[string]map[string]bool
		expected bool
	}{
		{
			name:   "exact match",
			method: "GET",
			url:    "/api/users",
			routes: map[string]map[string]bool{
				"GET": {"/api/users": true},
			},
			expected: true,
		},
		{
			name:   "wildcard method",
			method: "POST",
			url:    "/api/users",
			routes: map[string]map[string]bool{
				"*": {"/api/users": true},
			},
			expected: true,
		},
		{
			name:   "wildcard URL",
			method: "GET",
			url:    "/api/users",
			routes: map[string]map[string]bool{
				"GET": {"*": true},
			},
			expected: true,
		},
		{
			name:   "not monitored",
			method: "GET",
			url:    "/api/other",
			routes: map[string]map[string]bool{
				"GET": {"/api/users": true},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRouteMonitored(tt.method, tt.url, tt.routes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUsageFlowAPI_ExecuteRequestWithMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	api := New("test-api-key")
	defer api.socketManager.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/users", nil)

	metadata := map[string]interface{}{
		"test": "value",
	}

	// This will fail because socket is not connected, but should return true
	success, err := api.ExecuteRequestWithMetadata("ledger-id", "GET", "/api/users", metadata, c, false)
	assert.NoError(t, err)
	assert.True(t, success)
}

func TestUsageFlowAPI_ExecuteFulfillRequestWithMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	api := New("test-api-key")
	defer api.socketManager.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/users", nil)
	c.Set("eventId", "allocation-123")
	c.Set("usageflowStartTime", time.Now())

	metadata := map[string]interface{}{
		"test": "value",
	}

	// This will fail because socket is not connected, but should return true
	success, err := api.ExecuteFulfillRequestWithMetadata("ledger-id", "GET", "/api/users", metadata, c)
	assert.NoError(t, err)
	assert.True(t, success)
}

// Helper function
func stringPtr(s string) *string {
	return &s
}

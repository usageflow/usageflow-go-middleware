package middleware

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/config"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/socket"
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
			expected:    "user-123",
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
			expected:    "user-456",
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
			expected:    "order-789",
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
			expected:    "session-abc",
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
			expected:    "user-999",
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
			expected:    "test@example.com",
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
			expected:    "jwt-user-123",
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
			expected:    "sub-123",
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
			expected:    "session-123",
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
			expected:    "token-456",
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
			expected:    "session-789",
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
			expected:    "cookie-jwt-user-123",
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
			expected:    "cookie-sub-456",
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
			expected:    "limited-user",
			rateLimited: true,
			description: "Should set rateLimited flag when HasRateLimit is true",
		},
		{
			name:   "rate limited without identity",
			method: "GET",
			url:    "/api/limited",
			config: []config.ApiConfigStrategy{
				{
					Url:          "/api/limited",
					Method:       "GET",
					HasRateLimit: true,
				},
			},
			setup:       func(c *gin.Context) {},
			expected:    "",
			rateLimited: true,
			description: "Should keep rateLimited when HasRateLimit is true even without identity",
		},
		{
			name:   "rate limited missing identity value",
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
			setup:       func(c *gin.Context) {},
			expected:    "",
			rateLimited: true,
			description: "Should keep rateLimited when identity header is missing",
		},
		{
			name:   "unrelated hasRateLimit does not apply",
			method: "GET",
			url:    "/api/open",
			config: []config.ApiConfigStrategy{
				{
					Url:          "/api/limited",
					Method:       "GET",
					HasRateLimit: true,
				},
				{
					Url:                   "/api/open",
					Method:                "GET",
					IdentityFieldName:     stringPtr("userId"),
					IdentityFieldLocation: stringPtr("headers"),
				},
			},
			setup: func(c *gin.Context) {
				c.Request.Header.Set("userId", "open-user")
			},
			expected:    "open-user",
			rateLimited: false,
			description: "HasRateLimit on another route must not force rateLimited",
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
					HasRateLimit:          true,
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
		{
			name:     "empty map monitors all (JS/Python parity)",
			method:   "POST",
			url:      "/api/chat",
			routes:   map[string]map[string]bool{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRouteMonitored(tt.method, tt.url, tt.routes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestApplyRouteConfig(t *testing.T) {
	reportAll := false
	api := &UsageFlowAPI{
		localWhitelist: []config.Route{
			{Method: "GET", URL: "/local-health"},
		},
		reportAllFunctionAllocations: true,
	}

	err := api.applyRouteConfig(config.ApplicationConfigResponse{
		MonitorPaths: []interface{}{
			map[string]interface{}{"method": "GET", "url": "/users"},
			map[string]interface{}{"method": "POST", "url": "/orders"},
			map[string]interface{}{"method": "", "url": "/ignored"},
		},
		WhitelistEndpoints: []interface{}{
			map[string]interface{}{"method": "GET", "url": "/health"},
		},
		ReportAllFunctionAllocations: &reportAll,
	})

	assert.NoError(t, err)
	assert.Equal(t, []config.Route{
		{Method: "GET", URL: "/health"},
	}, api.WhitelistEndpoints)
	assert.Equal(t, []config.Route{
		{Method: "GET", URL: "/users"},
		{Method: "POST", URL: "/orders"},
		{Method: "", URL: "/ignored"},
	}, api.MonitoringPaths)
	assert.True(t, isRouteMonitored("GET", "/users", api.monitoringPathsMap))
	assert.True(t, isRouteMonitored("POST", "/orders", api.monitoringPathsMap))
	assert.False(t, isRouteMonitored("GET", "/ignored", api.monitoringPathsMap))
	assert.True(t, isWhitelisted("GET", "/health", api.whitelistEndpointsMap))
	assert.True(t, isWhitelisted("GET", "/local-health", api.whitelistEndpointsMap))
	assert.False(t, api.reportAllFunctionAllocations)
}

func TestApplyRouteConfigDoesNotPartiallyUpdateInvalidConfig(t *testing.T) {
	api := &UsageFlowAPI{
		WhitelistEndpoints:    []config.Route{{Method: "GET", URL: "/old-health"}},
		MonitoringPaths:       []config.Route{{Method: "GET", URL: "/old-users"}},
		whitelistEndpointsMap: routesToMap([]config.Route{{Method: "GET", URL: "/old-health"}}),
		monitoringPathsMap:    routesToMap([]config.Route{{Method: "GET", URL: "/old-users"}}),
	}

	err := api.applyRouteConfig(config.ApplicationConfigResponse{
		WhitelistEndpoints: []interface{}{
			map[string]interface{}{"method": "GET", "url": "/new-health"},
		},
		MonitorPaths: []interface{}{"not-a-route"},
	})

	assert.ErrorContains(t, err, "failed to convert monitor paths")
	assert.Equal(t, []config.Route{{Method: "GET", URL: "/old-health"}}, api.WhitelistEndpoints)
	assert.Equal(t, []config.Route{{Method: "GET", URL: "/old-users"}}, api.MonitoringPaths)
	assert.True(t, isWhitelisted("GET", "/old-health", api.whitelistEndpointsMap))
	assert.True(t, isRouteMonitored("GET", "/old-users", api.monitoringPathsMap))
}

func TestForceMonitorAll_IgnoresRemoteMonitoringPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)

	api := New("test-api-key")
	defer api.socketManager.Close()
	api.ForceMonitorAll()
	api.monitoringPathsMap = map[string]map[string]bool{
		"POST": {"/api/chat": true},
	}

	r := gin.New()
	r.Use(api.RequestInterceptor())
	r.GET("/api/v1/accounts", func(c *gin.Context) {
		_, ok := c.Get("usageflowStartTime")
		assert.True(t, ok, "ForceMonitorAll should meter routes outside monitoringPaths")
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
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

	// A route with a configured rate limit must fail closed when authorization
	// cannot be obtained.
	success, err = api.ExecuteRequestWithMetadata("ledger-id", "GET", "/api/users", metadata, c, true)
	assert.ErrorContains(t, err, "rate-limit authorization unavailable")
	assert.False(t, success)
}

func TestRequestInterceptor_RateLimitedRouteFailsClosedWhenSocketUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	api := New("test-api-key")
	defer api.socketManager.Close()
	api.ForceMonitorAll()
	api.ApiConfig = []config.ApiConfigStrategy{
		{
			Method:       http.MethodPost,
			Url:          "/api/v1/discover",
			HasRateLimit: true,
		},
	}

	handlerCalled := false
	r := gin.New()
	r.Use(api.RequestInterceptor())
	r.POST("/api/v1/discover", func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/discover", strings.NewReader(`{"domain":"fivicon.com"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.False(t, handlerCalled)
	assert.JSONEq(t, `{"error":"rate_limit_exceeded","message":"UsageFlow could not authorize this rate-limited request."}`, w.Body.String())
}

func TestRequestInterceptor_RateLimitedRouteSettlesBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		useResponse   *socket.UsageFlowSocketResponse
		expectedCode  int
		handlerCalled bool
	}{
		{
			name:          "confirmed settlement runs handler",
			useResponse:   &socket.UsageFlowSocketResponse{Type: "success", Payload: map[string]interface{}{"eventId": "event-1"}},
			expectedCode:  http.StatusOK,
			handlerCalled: true,
		},
		{
			name:          "denied settlement blocks handler",
			useResponse:   &socket.UsageFlowSocketResponse{Type: "error", Error: "quota exceeded"},
			expectedCode:  http.StatusTooManyRequests,
			handlerCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &fakeSocketManager{
				connected: true,
				responses: []*socket.UsageFlowSocketResponse{
					{Type: "success", Payload: map[string]interface{}{"allocationId": "allocation-1"}},
					tt.useResponse,
				},
			}
			api := &UsageFlowAPI{
				ApiConfig: []config.ApiConfigStrategy{
					{Method: http.MethodPost, Url: "/api/v1/discover", HasRateLimit: true},
				},
				BlockedEndpoints: map[string]bool{},
				policyMap:        make(PolicyMap),
				socketManager:    manager,
				connected:        true,
				forceMonitorAll:  true,
				functionPolicies: make(map[string]config.ApiConfigStrategy),
			}

			handlerCalled := false
			r := gin.New()
			r.Use(api.RequestInterceptor())
			r.POST("/api/v1/discover", func(c *gin.Context) {
				handlerCalled = true
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/discover", strings.NewReader(`{"domain":"fivicon.com"}`))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedCode, w.Code)
			assert.Equal(t, tt.handlerCalled, handlerCalled)
			if assert.Len(t, manager.asyncMessages, 2) {
				use, ok := manager.asyncMessages[1].Payload.(*socket.UseAllocationRequest)
				if assert.True(t, ok) {
					assert.True(t, use.WaitForConfirmation)
					assert.Equal(t, "allocation-1", use.AllocationID)
				}
			}
		})
	}
}

func TestRequestInterceptor_NonRateLimitedRouteRemainsAsync(t *testing.T) {
	gin.SetMode(gin.TestMode)

	manager := &fakeSocketManager{connected: true}
	api := &UsageFlowAPI{
		ApiConfig:        []config.ApiConfigStrategy{},
		BlockedEndpoints: map[string]bool{},
		policyMap:        make(PolicyMap),
		socketManager:    manager,
		connected:        true,
		forceMonitorAll:  true,
		functionPolicies: make(map[string]config.ApiConfigStrategy),
	}

	handlerCalled := false
	r := gin.New()
	r.Use(api.RequestInterceptor())
	r.POST("/api/v1/discover", func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/discover", strings.NewReader(`{"domain":"fivicon.com"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, handlerCalled)
	assert.Empty(t, manager.asyncMessages, "non-rate-limited metering must not wait for a response")
	if assert.Len(t, manager.sentMessages, 2) {
		assert.Equal(t, "request_for_allocation", manager.sentMessages[0].Type)
		assert.Equal(t, "use_allocation", manager.sentMessages[1].Type)
	}
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

type fakeSocketManager struct {
	connected     bool
	responses     []*socket.UsageFlowSocketResponse
	asyncMessages []*socket.UsageFlowSocketMessage
	sentMessages  []*socket.UsageFlowSocketMessage
}

func (f *fakeSocketManager) Send(message *socket.UsageFlowSocketMessage) error {
	f.sentMessages = append(f.sentMessages, message)
	return nil
}

func (f *fakeSocketManager) SendAsync(message *socket.UsageFlowSocketMessage) (*socket.UsageFlowSocketResponse, error) {
	f.asyncMessages = append(f.asyncMessages, message)
	if len(f.responses) == 0 {
		return nil, errors.New("no fake response configured")
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

func (f *fakeSocketManager) IsConnected() bool {
	return f.connected
}

func (f *fakeSocketManager) Close() {}

// Helper function
func stringPtr(s string) *string {
	return &s
}

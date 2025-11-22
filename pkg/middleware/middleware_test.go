package middleware

import (
	"bytes"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/usageflow/usageflow-go-middleware/pkg/config"
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

	// Set up config
	api.mu.Lock()
	api.ApiConfig = []config.ApiConfigStrategy{
		{
			Url:                   "/api/users",
			Method:                "GET",
			IdentityFieldName:     stringPtr("userId"),
			IdentityFieldLocation: stringPtr("headers"),
		},
		{
			Url:                   "/api/orders",
			Method:                "POST",
			IdentityFieldName:     stringPtr("orderId"),
			IdentityFieldLocation: stringPtr("query"),
		},
	}
	api.mu.Unlock()

	tests := []struct {
		name     string
		method   string
		url      string
		setup    func(*gin.Context)
		expected string
	}{
		{
			name:   "extract from header",
			method: "GET",
			url:    "/api/users",
			setup: func(c *gin.Context) {
				c.Request.Header.Set("userId", "user-123")
			},
			expected: "user_123",
		},
		{
			name:   "extract from query",
			method: "POST",
			url:    "/api/orders",
			setup: func(c *gin.Context) {
				c.Request.URL.RawQuery = "orderId=order-456"
			},
			expected: "order_456",
		},
		{
			name:     "no matching config",
			method:   "DELETE",
			url:      "/api/items",
			setup:    func(c *gin.Context) {},
			expected: "",
		},
		{
			name:     "config without identity fields",
			method:   "GET",
			url:      "/api/users",
			setup:    func(c *gin.Context) {},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(tt.method, tt.url, nil)
			tt.setup(c)

			result := api.GetUserPrefix(c, tt.method, tt.url)
			assert.Equal(t, tt.expected, result)
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
	success, err := api.ExecuteRequestWithMetadata("ledger-id", "GET", "/api/users", metadata, c)
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

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/config"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/socket"
)

// Tests for identity.metadata / request.metadata support (USA-132).

func TestSanitizeMetadataValues(t *testing.T) {
	sanitized := sanitizeMetadataValues(map[string]any{
		"plan":   "pro",
		"seats":  10,
		"score":  4.5,
		"active": true,
		"nested": map[string]any{"a": 1},
		"arr":    []int{1, 2},
		"nil":    nil,
	})

	assert.Equal(t, map[string]any{
		"plan":   "pro",
		"seats":  10,
		"score":  4.5,
		"active": true,
	}, sanitized)
}

func TestSanitizeMetadataValues_EmptyOrAllDropped(t *testing.T) {
	assert.Nil(t, sanitizeMetadataValues(nil))
	assert.Nil(t, sanitizeMetadataValues(map[string]any{}))
	assert.Nil(t, sanitizeMetadataValues(map[string]any{"nested": map[string]any{"a": 1}}))
}

func TestSetIdentityMetadata_MergesAcrossCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := New("test-api-key")
	defer api.socketManager.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	api.SetIdentityMetadata(c, map[string]any{"plan": "pro"})
	api.SetIdentityMetadata(c, map[string]any{"region": "eu"})

	assert.Equal(t, map[string]any{"plan": "pro", "region": "eu"}, getContextMetadata(c, identityMetadataContextKey))
}

func TestSetIdentityAndRequestMetadata_StaySeparate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := New("test-api-key")
	defer api.socketManager.Close()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	api.SetIdentityMetadata(c, map[string]any{"plan": "pro"})
	api.SetRequestMetadata(c, map[string]any{"promoCode": "SUMMER"})

	assert.Equal(t, map[string]any{"plan": "pro"}, getContextMetadata(c, identityMetadataContextKey))
	assert.Equal(t, map[string]any{"promoCode": "SUMMER"}, getContextMetadata(c, requestMetadataContextKey))
}

// TestRequestInterceptor_IdentityAndRequestMetadataSentAsSeparateFields is an end-to-end check
// that metadata attached via SetIdentityMetadata/SetRequestMetadata in a handler registered
// before RequestInterceptor() reaches the wire payload as two distinct, never-merged fields.
func TestRequestInterceptor_IdentityAndRequestMetadataSentAsSeparateFields(t *testing.T) {
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

	r := gin.New()
	r.Use(func(c *gin.Context) {
		api.SetIdentityMetadata(c, map[string]any{"plan": "pro"})
		api.SetRequestMetadata(c, map[string]any{"promoCode": "SUMMER"})
		c.Next()
	})
	r.Use(api.RequestInterceptor())
	r.POST("/api/v1/discover", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/discover", strings.NewReader(`{"domain":"fivicon.com"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	if assert.Len(t, manager.sentMessages, 2) {
		alloc, ok := manager.sentMessages[0].Payload.(*socket.RequestForAllocation)
		if assert.True(t, ok) {
			assert.Equal(t, map[string]any{"plan": "pro"}, alloc.IdentityMetadata)
			assert.Equal(t, map[string]any{"promoCode": "SUMMER"}, alloc.RequestMetadata)
		}

		use, ok := manager.sentMessages[1].Payload.(*socket.UseAllocationRequest)
		if assert.True(t, ok) {
			assert.Equal(t, map[string]any{"plan": "pro"}, use.IdentityMetadata)
			assert.Equal(t, map[string]any{"promoCode": "SUMMER"}, use.RequestMetadata)
		}
	}
}

// TestRequestInterceptor_NoMetadataAttachedOmitsFields confirms the fields stay unset when the
// app never calls SetIdentityMetadata/SetRequestMetadata (backward compatibility).
func TestRequestInterceptor_NoMetadataAttachedOmitsFields(t *testing.T) {
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

	r := gin.New()
	r.Use(api.RequestInterceptor())
	r.POST("/api/v1/discover", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/discover", strings.NewReader(`{"domain":"fivicon.com"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	if assert.Len(t, manager.sentMessages, 2) {
		alloc, ok := manager.sentMessages[0].Payload.(*socket.RequestForAllocation)
		if assert.True(t, ok) {
			assert.Nil(t, alloc.IdentityMetadata)
			assert.Nil(t, alloc.RequestMetadata)
		}
	}
}

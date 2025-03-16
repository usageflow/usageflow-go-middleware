package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/usageflow/usageflow-go-middleware/pkg/config"
)

func TestRequestInterceptor(t *testing.T) {
	// Create a mock UsageFlow API server
	mockUsageFlowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/ledgers/measure/allocate":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"eventId": "test-event-id",
				"success": true,
			})
		case "/api/v1/ledgers/measure/allocate/use":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
			})
		default:
			t.Errorf("Unexpected request to path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockUsageFlowServer.Close()

	// Create test API instance
	usageFlowAPI := New("test-api-key")
	usageFlowAPI.ApplicationId = "test-app-id"

	// Create test Gin router
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Define routes to monitor
	routes := []config.Route{
		{Method: "GET", URL: "/test"},
		{Method: "POST", URL: "/test"},
	}

	// Define whitelist routes
	whiteList := []config.Route{
		{Method: "GET", URL: "/health"},
	}

	router.Use(usageFlowAPI.RequestInterceptor(routes, whiteList))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test cases
	tests := []struct {
		name           string
		method         string
		path           string
		headers        map[string]string
		body           interface{}
		expectedStatus int
	}{
		{
			name:           "successful request",
			method:         "GET",
			path:           "/test",
			headers:        map[string]string{"User-Id": "test-user"},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "request with body",
			method:         "POST",
			path:           "/test",
			headers:        map[string]string{"User-Id": "test-user"},
			body:           map[string]interface{}{"key": "value"},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyBytes []byte
			if tt.body != nil {
				var err error
				bodyBytes, err = json.Marshal(tt.body)
				if err != nil {
					t.Fatalf("Failed to marshal request body: %v", err)
				}
			}

			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBuffer(bodyBytes))
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			if tt.body != nil {
				req.Header.Set("Content-Type", "application/json")
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status code %d but got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestCollectRequestMetadata(t *testing.T) {
	// Create test API instance
	usageFlowAPI := New("test-api-key")
	usageFlowAPI.ApplicationId = "test-app-id"

	// Create test Gin context
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Set up request with various metadata
	body := map[string]interface{}{"key": "value"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/test?param=value", bytes.NewBuffer(bodyBytes))
	req.Header.Set("User-Agent", "test-agent")
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	// Collect metadata
	metadata := usageFlowAPI.collectRequestMetadata(c)

	// Verify metadata fields
	tests := []struct {
		name     string
		field    string
		expected interface{}
	}{
		{"method", "method", "POST"},
		{"url", "url", "/test"},
		{"applicationId", "applicationId", "test-app-id"},
		{"userAgent", "userAgent", "test-agent"},
		{"queryParams", "queryParams", map[string]interface{}{"param": "value"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, exists := metadata[tt.field]
			if !exists {
				t.Errorf("Expected metadata to contain field %q", tt.field)
				return
			}
			if value != tt.expected {
				t.Errorf("Expected %q to be %v but got %v", tt.field, tt.expected, value)
			}
		})
	}
}

func TestGetUserPrefix(t *testing.T) {
	// Create test API instance
	usageFlowAPI := New("test-api-key")
	usageFlowAPI.ApplicationId = "test-app-id"
	usageFlowAPI.ApiConfig = &config.ApiConfigStrategy{
		IdentityFieldName:     "userId",
		IdentityFieldLocation: "header",
	}

	tests := []struct {
		name           string
		setupContext   func(*gin.Context)
		expectedPrefix string
		expectError    bool
	}{
		{
			name: "user ID in header",
			setupContext: func(c *gin.Context) {
				c.Request.Header.Set("userId", "test-user")
			},
			expectedPrefix: "test_user",
			expectError:    false,
		},
		{
			name: "user ID in query",
			setupContext: func(c *gin.Context) {
				q := c.Request.URL.Query()
				q.Add("userId", "test-user")
				c.Request.URL.RawQuery = q.Encode()
			},
			expectedPrefix: "test_user",
			expectError:    false,
		},
		{
			name: "user ID in body",
			setupContext: func(c *gin.Context) {
				body := map[string]interface{}{"userId": "test-user"}
				bodyBytes, _ := json.Marshal(body)
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
				c.Request.Header.Set("Content-Type", "application/json")
			},
			expectedPrefix: "test_user",
			expectError:    false,
		},
		{
			name: "no user ID",
			setupContext: func(c *gin.Context) {
				// No setup needed
			},
			expectedPrefix: "",
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/test", nil)
			tt.setupContext(c)

			prefix := usageFlowAPI.GetUserPrefix(c, "GET", "/test")
			if tt.expectError {
				if prefix != "" {
					t.Error("Expected empty prefix but got non-empty")
				}
			} else {
				if prefix != tt.expectedPrefix {
					t.Errorf("Expected prefix %q but got %q", tt.expectedPrefix, prefix)
				}
			}
		})
	}
}

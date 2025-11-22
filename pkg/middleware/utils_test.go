package middleware

import (
	"bytes"
	"encoding/base64"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGetPatternedURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		setup    func(*gin.Context)
		expected string
	}{
		{
			name: "with full path",
			setup: func(c *gin.Context) {
				c.Request.URL.Path = "/api/users/123"
				// Simulate FullPath() returning pattern
				c.Params = gin.Params{gin.Param{Key: "id", Value: "123"}}
			},
			expected: "/api/users/123",
		},
		{
			name: "fallback to raw path",
			setup: func(c *gin.Context) {
				c.Request.URL.Path = "/api/users/123"
			},
			expected: "/api/users/123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/api/users/123", nil)
			tt.setup(c)

			result := GetPatternedURL(c)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		header      string
		expected    string
		expectError bool
	}{
		{
			name:        "valid bearer token",
			header:      "Bearer token123",
			expected:    "token123",
			expectError: false,
		},
		{
			name:        "missing header",
			header:      "",
			expected:    "",
			expectError: true,
		},
		{
			name:        "invalid format",
			header:      "Basic token123",
			expected:    "",
			expectError: true,
		},
		{
			name:        "no token",
			header:      "Bearer",
			expected:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/", nil)
			if tt.header != "" {
				c.Request.Header.Set("Authorization", tt.header)
			}

			token, err := ExtractBearerToken(c)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, token)
			}
		})
	}
}

func TestDecodeJWTUnverified(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		expectError bool
		checkClaim  func(map[string]interface{}) bool
	}{
		{
			name:        "valid JWT",
			token:       createTestJWT("{\"userId\":\"123\",\"email\":\"test@example.com\"}"),
			expectError: false,
			checkClaim: func(claims map[string]interface{}) bool {
				return claims["userId"] == "123"
			},
		},
		{
			name:        "invalid format - too few parts",
			token:       "header.payload",
			expectError: true,
		},
		{
			name:        "invalid format - too many parts",
			token:       "header.payload.signature.extra",
			expectError: true,
		},
		{
			name:        "invalid base64",
			token:       "header.invalid-base64!.signature",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := DecodeJWTUnverified(tt.token)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.checkClaim != nil {
					assert.True(t, tt.checkClaim(claims))
				}
			}
		})
	}
}

func TestTransformToLedgerId(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple string",
			input:    "User123",
			expected: "user123",
		},
		{
			name:     "with spaces",
			input:    "User Name",
			expected: "user_name",
		},
		{
			name:     "with special characters",
			input:    "User@Name#123",
			expected: "user_name_123",
		},
		{
			name:     "already lowercase",
			input:    "user_name",
			expected: "user_name",
		},
		{
			name:     "mixed case with numbers",
			input:    "User123Name456",
			expected: "user123name456",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TransformToLedgerId(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "JSON body",
			body:     `{"key":"value"}`,
			expected: `{"key":"value"}`,
		},
		{
			name:     "empty body",
			body:     "",
			expected: "",
		},
		{
			name:     "text body",
			body:     "plain text",
			expected: "plain text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/", bytes.NewBufferString(tt.body))

			result, err := GetRequestBody(c)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Helper function to create a test JWT
func createTestJWT(payload string) string {
	header := `{"alg":"HS256","typ":"JWT"}`
	headerEncoded := base64Encode(header)
	payloadEncoded := base64Encode(payload)
	signature := "signature"
	return headerEncoded + "." + payloadEncoded + "." + signature
}

func base64Encode(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

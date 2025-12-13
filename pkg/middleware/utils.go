package middleware

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	once sync.Once
)

// StartConfigUpdater begins periodic updates of the API configuration
func (u *UsageFlowAPI) StartConfigUpdater() {
	once.Do(func() {
		// Immediately fetch config
		go u.FetchApiConfig()
		go u.FetchBlockedEndpoints()
		go u.FetchApplicationConfig()
		// Start periodic updates every 30 seconds
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()

			for range ticker.C {
				u.FetchApiConfig()
				u.FetchBlockedEndpoints()
				u.FetchApplicationConfig()
			}
		}()
	})
}

// GetPatternedURL returns a standardized URL pattern for the current request
func GetPatternedURL(c *gin.Context) string {
	// You can implement custom URL pattern matching here
	// For example, convert dynamic segments to placeholders
	// Currently returning the raw path
	pattern := c.FullPath()
	if pattern == "" {
		return c.Request.URL.Path // Fallback to actual path if no pattern is found
	}
	return pattern
}

// ExtractBearerToken extracts the bearer token from the Authorization header
func ExtractBearerToken(c *gin.Context) (string, error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("Authorization header is missing")
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", fmt.Errorf("Invalid Authorization header format")
	}

	return parts[1], nil
}

// DecodeJWTUnverified decodes a JWT without verifying its signature
func DecodeJWTUnverified(token string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("Invalid JWT format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("Failed to decode JWT payload: %v", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("Failed to parse JWT payload: %v", err)
	}

	return claims, nil
}

// TransformToLedgerId converts an input string to a valid ledger ID format
func TransformToLedgerId(input string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	return re.ReplaceAllString(strings.ToLower(input), "_")
}

// GetRequestBody reads and returns the request body as a string
func GetRequestBody(c *gin.Context) (string, error) {
	if c.Request.Body == nil {
		return "", nil
	}

	body, err := c.GetRawData()
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func ConvertToType[T any](obj any) (T, error) {
	var zero T

	jsonData, err := json.Marshal(obj)
	if err != nil {
		return zero, err
	}

	var result T
	err = json.Unmarshal(jsonData, &result)
	if err != nil {
		return zero, err
	}

	return result, nil
}

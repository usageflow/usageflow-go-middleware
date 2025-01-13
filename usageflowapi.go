package usageflow

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Route defines an individual route configuration
type Route struct {
	Method string
	URL    string
}

type UsageFlowAPI struct {
	APIKey string `json:"apiKey"`
}

func (u *UsageFlowAPI) Init(apiKey string) {
	// Initialize your API client
	u.APIKey = apiKey
}

// Middleware for intercepting requests before they reach the user's routes
func (u UsageFlowAPI) RequestInterceptor(routes []Route) gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		url := c.Request.URL.Path

		for _, route := range routes {
			// Match the method and URL, including wildcards
			if (route.Method == "*" || strings.ToUpper(route.Method) == method) &&
				(route.URL == "*" || route.URL == url) {

				// Extract the ledgerId
				// ledgerId := u.GuessLedgerId(c)

				// Execute the request with metadata logging
				go func() {
					success, err := u.ExecuteRequestWithMetadata("", method, url)
					if err != nil {
						fmt.Printf("Error processing request for %s %s: %v\n", method, url, err)
					} else if success {
						fmt.Printf("Successfully processed request for %s %s\n", method, url)

					} else {
						fmt.Printf("Failed to process request for %s %s\n", method, url)
					}
				}()
			}
		}

		// Continue the regular flow without interference
		c.Next()
	}
}
func (u *UsageFlowAPI) GuessLedgerId(c *gin.Context) string {
	// 1. Check Authorization header for Bearer token
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if decodedToken := decodeBearerToken(token); decodedToken != "" {
			return transformToLedgerId(decodedToken)
		}
	}

	// 2. Check URL path for accountId
	// Assume accountId can be in the path, e.g., "/api/accounts/{accountId}/resource"
	pathSegments := strings.Split(c.Request.URL.Path, "/")
	for i, segment := range pathSegments {
		if segment == "accounts" && i+1 < len(pathSegments) {
			return transformToLedgerId(pathSegments[i+1])
		}
	}

	// 3. Check URL query parameters
	if userId := c.Query("userId"); userId != "" {
		return transformToLedgerId(userId)
	}
	if accountId := c.Query("accountId"); accountId != "" {
		return transformToLedgerId(accountId)
	}

	// 4. Check JSON body for accountId or userId
	var bodyData map[string]interface{}
	if err := c.ShouldBindJSON(&bodyData); err == nil {
		if userId, exists := bodyData["userId"].(string); exists {
			return transformToLedgerId(userId)
		}
		if accountId, exists := bodyData["accountId"].(string); exists {
			return transformToLedgerId(accountId)
		}
	}

	// 5. Fallback to default ledgerId
	return ""
}

// DecodeBearerToken decodes a base64-encoded Bearer token and extracts the user or account identifier
func decodeBearerToken(token string) string {
	// Example assumes the Bearer token is base64-encoded
	decoded, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		fmt.Println("Failed to decode Bearer token:", err)
		return ""
	}
	return string(decoded)
}

// TransformToLedgerId generates a ledgerId from the input string
func transformToLedgerId(input string) string {
	// Example transformation logic (can be customized)
	return fmt.Sprintf("ledger-%s", input)
}

// ExecuteRequest sends a POST request to your server and returns a success flag
func (u UsageFlowAPI) ExecuteRequestWithMetadata(ledgerId, method, url string) (bool, error) {
	apiURL := "https://api.usageflow.io/api/v1/ledgers/measure/use"

	// Set headers
	headers := map[string]string{
		"x-usage-key":  u.APIKey,
		"Content-Type": "application/json",
	}

	// Set body with metadata
	payload := map[string]interface{}{
		"alias":  ledgerId,
		"amount": 3,
		"metadata": map[string]string{
			"method": method,
			"url":    url,
		},
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return false, err
	}

	// Add headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Execute request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	// Log response
	fmt.Printf("Response from server (%s %s)\n", method, url)

	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

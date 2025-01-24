package usageflow

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Route defines an individual route configuration
type Route struct {
	Method string
	URL    string
}

type UsageFlowAPI struct {
	APIKey        string `json:"apiKey"`
	ApplicationId string `json:"applicationId"`
}

type verifyResponse struct {
	AccountId     string `json:"accountId"`
	ApplicationId string `json:"applicationId"`
}

func (u *UsageFlowAPI) Init(apiKey string) {
	// Initialize your API client
	u.APIKey = apiKey

}

func verifyAPIRequest(apiKey string) (*verifyResponse, error) { // Make the request
	// req, err := http.NewRequest("GET", "http://127.0.0.1:9000/api/v1/iam/account/api/verify", nil)
	req, err := http.NewRequest("GET", "https://api.usageflow.io/api/v1/iam/account/api/verify", nil)

	if err != nil {
		return nil, err
	}

	req.Header.Set("x-usage-key", apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, errors.New("failed to verify: " + string(body))
	}

	// Parse response
	var verifyResp verifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&verifyResp); err != nil {
		return nil, err
	}

	return &verifyResp, nil
}

// Middleware for intercepting requests before they reach the user's routes
func (u UsageFlowAPI) RequestInterceptor(routes, whiteListRoutes []Route) gin.HandlerFunc {
	defaultWhiteListRoutes := []Route{
		{Method: "POST", URL: "/api/v1/ledgers/measure/allocate/use"},
		{Method: "POST", URL: "/api/v1/ledgers/measure/allocate"},
	}

	// Combine provided whiteListRoutes with the default ones
	whiteListRoutes = append(whiteListRoutes, defaultWhiteListRoutes...)

	apiVerifyResponse, err := verifyAPIRequest(u.APIKey)

	if err != nil {
		u.ApplicationId = apiVerifyResponse.ApplicationId
	}

	// Convert routes and whi
	routesMap := make(map[string]map[string]bool) // Method -> URL -> exists
	whiteListRoutesMap := make(map[string]map[string]bool)

	// Helper function to populate the map
	populateMap := func(targetMap map[string]map[string]bool, routes []Route) {
		for _, route := range routes {
			if _, exists := targetMap[route.Method]; !exists {
				targetMap[route.Method] = make(map[string]bool)
			}
			targetMap[route.Method][route.URL] = true
		}
	}

	// Populate the maps
	populateMap(routesMap, routes)
	populateMap(whiteListRoutesMap, whiteListRoutes)

	return func(c *gin.Context) {
		method := c.Request.Method
		url := GetPatternedURL(c)

		if len(routesMap) == 0 {
			c.Next()
			return
		}

		// Check if the current request matches any route in the whitelist
		if methodRoutes, exists := whiteListRoutesMap[method]; exists {
			if methodRoutes[url] || methodRoutes["*"] {
				c.Next() // Skip processing for whitelisted routes
				return
			}
		}

		// Check for "*" method in the whitelist
		if allMethodsRoutes, exists := whiteListRoutesMap["*"]; exists {
			if allMethodsRoutes[url] || allMethodsRoutes["*"] {
				c.Next()
				return
			}
		}

		routeFound := false
		if methodRoutes, exists := routesMap[method]; exists {
			if methodRoutes[url] || methodRoutes["*"] {
				// Add any custom logic for matched routes here
				// c.Next()
				routeFound = true
			}
		}

		// Check for "*" method in the routesMap
		if !routeFound {
			if allMethodsRoutes, exists := routesMap["*"]; exists {
				if allMethodsRoutes[url] || allMethodsRoutes["*"] {
					// Add any custom logic for matched routes here
					// c.Next()
					routeFound = true
				}
			}
		}

		// Skip specific hardcoded routes
		// if method == "POST" && (url == "/api/v1/ledgers/measure/allocate/use" || url == "/api/v1/ledgers/measure/allocate") {
		// 	c.Next() // Skip this route
		// 	return
		// }

		if !routeFound {
			c.Next()
		}
		// Collect metadata
		metadata := map[string]interface{}{
			"applicationId": u.ApplicationId,
			"method":        method,
			"url":           url,                // Route pattern (e.g., /api/v1/ledgers/:id)
			"rawUrl":        c.Request.URL.Path, // Raw URL (e.g., /api/v1/ledgers/123)
			"clientIP":      c.ClientIP(),
			"userAgent":     c.GetHeader("User-Agent"),       // User-Agent header
			"timestamp":     time.Now().Format(time.RFC3339), // Timestamp of the request
		}

		// Extract query parameters
		queryParams := c.DefaultQuery("params", "")
		if queryParams != "" {
			metadata["queryParams"] = queryParams
		}

		// Extract all query parameters (key-value pairs)
		allQueryParams := c.Request.URL.Query()
		if len(allQueryParams) > 0 {
			metadata["allQueryParams"] = allQueryParams
		}

		// Capture request body (only if necessary)
		var requestBody map[string]interface{}
		if method == "POST" || method == "PUT" {
			bodyBytes, _ := io.ReadAll(c.Request.Body)                // Read the body
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes)) // Reset the body to allow further processing
			json.Unmarshal(bodyBytes, &requestBody)                   // Parse the body to metadata
			if len(requestBody) > 0 {
				metadata["body"] = requestBody
			}
		}

		// Add headers to metadata (e.g., Authorization, X-Request-ID)
		headers := c.Request.Header
		if len(headers) > 0 {
			// Create a copy of headers to avoid modifying the original
			sanitizedHeaders := make(map[string][]string)

			// Compile the regular expression for matching keys
			keyRegex := regexp.MustCompile(`(?i)^x-.*key$`) // (?i) makes it case-insensitive

			for key, values := range headers {
				// Normalize the header key to lowercase for comparison
				keyLower := strings.ToLower(key)

				// Mask specific headers based on conditions
				switch keyLower {
				case "authorization":
					// Mask "Authorization" header
					if len(values) > 0 {
						sanitizedHeaders[key] = []string{"Bearer ****"}
					}
				default:
					// Check if the key matches the regex for x-*key
					if keyRegex.MatchString(key) {
						// Mask headers matching the regex
						if len(values) > 0 {
							sanitizedHeaders[key] = []string{"****"}
						}
					} else {
						// For other headers, include them as is
						sanitizedHeaders[key] = values
					}
				}
			}

			// Add sanitized headers to metadata
			metadata["headers"] = sanitizedHeaders
		}

		// Add location (X-Forwarded-For header, if available)
		if forwardedFor := c.GetHeader("X-Forwarded-For"); forwardedFor != "" {
			metadata["forwardedFor"] = forwardedFor
		}

		// Capture the route variables (e.g., from /api/v1/ledgers/:id)
		if params := c.Params; len(params) > 0 {
			paramsMap := make(map[string]string)
			for _, param := range params {
				paramsMap[param.Key] = param.Value
			}
			metadata["pathParams"] = paramsMap
		}

		ledgerId := u.GuessLedgerId(c)
		for _, route := range routes {
			// Match the method and URL, including wildcards
			if (route.Method == "*" || strings.ToUpper(route.Method) == method) &&
				(route.URL == "*" || route.URL == url) {

				// Extract the ledgerId
				// ledgerId := u.GuessLedgerId(c)

				// Execute the request with metadata logging
				// go func() {
				success, err := u.ExecuteRequestWithMetadata(ledgerId, method, url, metadata, c)
				if success == false {
					return
				}
				if err != nil {
					fmt.Printf("Error processing request for %s %s: %v\n", method, url, err)
				} else if success {
					fmt.Printf("Successfully processed request for %s %s\n", method, url)
				} else {
					fmt.Printf("Failed to process request for %s %s\n", method, url)
				}
				// c.Next()
				// }()
			}
		}

		// Continue the regular flow without interference

	}
}
func (u *UsageFlowAPI) GuessLedgerId(c *gin.Context) string {
	// Helper function to extract sub from Bearer token
	method := c.Request.Method
	url := GetPatternedURL(c)

	if true {
		return fmt.Sprintf("%s %s", method, url)
	}

	getSubFromBearerToken := func(token string) string {
		parsedToken, _, err := jwt.NewParser().ParseUnverified(token, jwt.MapClaims{})
		if err != nil {
			return ""
		}

		if claims, ok := parsedToken.Claims.(jwt.MapClaims); ok {
			if sub, exists := claims["sub"].(string); exists {
				return sub
			}
		}
		return ""
	}

	// 1. Check Authorization header for Bearer token
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if sub := getSubFromBearerToken(token); sub != "" {
			return transformToLedgerId(sub)
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

	// 4. Check JSON body for accountId or userId without consuming the body
	var bodyData map[string]interface{}
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err == nil {
		// Restore the body so it can be read later
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		// Attempt to parse the body
		if err := json.Unmarshal(bodyBytes, &bodyData); err == nil {
			if userId, exists := bodyData["userId"].(string); exists {
				return transformToLedgerId(userId)
			}
			if accountId, exists := bodyData["accountId"].(string); exists {
				return transformToLedgerId(accountId)
			}
		}
	}

	// 5. Fallback to default ledgerId
	return ""
}

func GetPatternedURL(c *gin.Context) string {
	// Get the route pattern (e.g., "/api/v1/ledgers/allocate/:ledgerId/consume")
	pattern := c.FullPath()
	if pattern == "" {
		return c.Request.URL.Path // Fallback to actual path if no pattern is found
	}
	return pattern
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
func (u UsageFlowAPI) ExecuteRequestWithMetadata(ledgerId, method, url string, metadata map[string]interface{}, c *gin.Context) (bool, error) {
	apiURL := "https://api.usageflow.io/api/v1/ledgers/measure/allocate"
	// apiURL := "http://127.0.0.1:9000/api/v1/ledgers/measure/allocate"
	// Set headers
	headers := map[string]string{
		"x-usage-key":  u.APIKey,
		"Content-Type": "application/json",
	}

	// Set body with metadata
	payload := map[string]interface{}{
		"alias":  ledgerId,
		"amount": 1,
		// "metadata": metadata,
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	// Optionally, unmarshal the response to check for eventId or other keys
	var responseData map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &responseData); err == nil {
		// Store the data in Gin context if "eventId" key exists
		if eventId, exists := responseData["eventId"]; exists {
			c.Set("eventId", eventId)
		}
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.Next()
		u.ExecuteFulfillRequestWithMetadata(ledgerId, method, url, metadata, c)
		return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
	} else {
		c.AbortWithStatusJSON(400, gin.H{
			"error": "Request fulfillment failed",
		})

		return false, nil
	}

	return false, c.Error(err)
}

func (u UsageFlowAPI) ExecuteFulfillRequestWithMetadata(ledgerId, method, url string, metadata map[string]interface{}, c *gin.Context) (bool, error) {
	apiURL := "https://api.usageflow.io/api/v1/ledgers/measure/allocate/use"
	// apiURL := "http://127.0.0.1:9000/api/v1/ledgers/measure/allocate/use"

	allocationId, _ := c.Get("eventId")
	// Set headers
	headers := map[string]string{
		"x-usage-key":  u.APIKey,
		"Content-Type": "application/json",
	}

	// Set body with metadata
	payload := map[string]interface{}{
		"alias":        ledgerId,
		"amount":       1,
		"allocationId": allocationId.(string),
		"metadata":     metadata,
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	// Optionally, unmarshal the response to check for eventId or other keys
	var responseData map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &responseData); err == nil {
		// Store the data in Gin context if "eventId" key exists
		if eventId, exists := responseData["eventId"]; exists {
			c.Set("eventId", eventId)
		}
	}

	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

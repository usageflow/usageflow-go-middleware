package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/usageflow/usageflow-go-middleware/pkg/api"
	"github.com/usageflow/usageflow-go-middleware/pkg/config"
)

// PolicyMap represents a map of endpoint patterns to their corresponding policies
type PolicyMap map[string]*config.ApplicationEndpointPolicy

// UsageFlowAPI represents the main middleware structure
type UsageFlowAPI struct {
	APIKey                      string                    `json:"apiKey"`
	ApplicationId               string                    `json:"applicationId"`
	ApiConfig                   *config.ApiConfigStrategy `json:"apiConfig"`
	ApplicationEndpointPolicies *config.PolicyResponse    `json:"applicationEndpointPolicies"`
	policyMap                   PolicyMap
	mu                          sync.RWMutex
}

const (
	baseURL = "https://api.usageflow.io/api/v1"
)

// New creates a new instance of UsageFlowAPI
func New(apiKey string) *UsageFlowAPI {
	api := &UsageFlowAPI{
		policyMap: make(PolicyMap),
	}
	api.Init(apiKey)
	return api
}

// Init initializes the UsageFlowAPI with the provided API key
func (u *UsageFlowAPI) Init(apiKey string) {
	u.APIKey = apiKey
	u.StartConfigUpdater()
}

// RequestInterceptor creates a Gin middleware for intercepting requests
func (u *UsageFlowAPI) RequestInterceptor(routes, whiteListRoutes []config.Route) gin.HandlerFunc {
	defaultWhiteListRoutes := []config.Route{
		{Method: "POST", URL: "/api/v1/ledgers/measure/allocate/use"},
		{Method: "POST", URL: "/api/v1/ledgers/measure/allocate"},
	}

	// Combine provided whiteListRoutes with the default ones
	whiteListRoutes = append(whiteListRoutes, defaultWhiteListRoutes...)

	routesMap := make(map[string]map[string]bool)
	whiteListRoutesMap := make(map[string]map[string]bool)

	populateMap := func(targetMap map[string]map[string]bool, routes []config.Route) {
		for _, route := range routes {
			if _, exists := targetMap[route.Method]; !exists {
				targetMap[route.Method] = make(map[string]bool)
			}
			targetMap[route.Method][route.URL] = true
		}
	}

	populateMap(routesMap, routes)
	populateMap(whiteListRoutesMap, whiteListRoutes)

	// Initial config fetch
	newConfig, _ := api.FetchApiConfig(u.APIKey)
	u.mu.Lock()
	u.ApiConfig = newConfig
	u.mu.Unlock()

	return func(c *gin.Context) {
		method := c.Request.Method
		url := GetPatternedURL(c)

		if len(routesMap) == 0 {
			c.Next()
			return
		}

		// Check whitelist
		if isWhitelisted(method, url, whiteListRoutesMap) {
			c.Next()
			return
		}

		// Check if route should be monitored
		if !isRouteMonitored(method, url, routesMap) {
			c.Next()
			return
		}

		//Capture the time before the request is processed
		startTime := time.Now()
		c.Set("usageflowStartTime", startTime)

		// Process request with UsageFlow logic
		metadata := u.collectRequestMetadata(c)
		ledgerId := u.GuessLedgerId(c)
		userIdentifierSuffix := u.GetUserPrefix(c, method, url)

		if userIdentifierSuffix != "" {
			ledgerId = fmt.Sprintf("%s %s", ledgerId, userIdentifierSuffix)
		}

		// Execute initial allocation request
		success, err := u.ExecuteRequestWithMetadata(ledgerId, method, url, metadata, c)
		if err != nil {
			c.AbortWithStatusJSON(500, gin.H{"error": "Failed to process request"})
			return
		}

		if !success {
			c.AbortWithStatusJSON(400, gin.H{"error": "Request allocation failed"})
			return
		}

		// Process the original request
		c.Next()

		// After the request is processed, execute the fulfill request
		metadata["responseStatusCode"] = c.Writer.Status()

		// Store the request duration in milliseconds

		if _, err := u.ExecuteFulfillRequestWithMetadata(ledgerId, method, url, metadata, c); err != nil {
			// Log the error but don't abort since the main request has already been processed
			fmt.Printf("Failed to fulfill request: %v\n", err)
		}
	}
}

// ExecuteRequestWithMetadata executes the initial allocation request
func (u *UsageFlowAPI) ExecuteRequestWithMetadata(ledgerId, method, url string, metadata map[string]interface{}, c *gin.Context) (bool, error) {
	apiURL := baseURL + "/ledgers/measure/allocate"

	payload := map[string]interface{}{
		"alias":  ledgerId,
		"amount": 1,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("failed to marshal request payload: %v", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return false, err
	}

	req.Header.Set("x-usage-key", u.APIKey)
	req.Header.Set("Content-Type", "application/json")

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

	var responseData map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &responseData); err == nil {
		if eventId, exists := responseData["eventId"]; exists {
			c.Set("eventId", eventId)
		}
	}

	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

// ExecuteFulfillRequestWithMetadata executes the fulfill request after the main request is processed
func (u *UsageFlowAPI) ExecuteFulfillRequestWithMetadata(ledgerId, method, url string, metadata map[string]interface{}, c *gin.Context) (bool, error) {
	apiURL := baseURL + "/ledgers/measure/allocate/use"

	allocationId, exists := c.Get("eventId")
	if !exists {
		return false, fmt.Errorf("no allocation ID found")
	}

	startTime, exists := c.Get("usageflowStartTime")
	if exists {
		requestDuration := time.Since(startTime.(time.Time))
		metadata["requestDuration"] = requestDuration.Milliseconds() // Store as milliseconds
	}

	payload := map[string]interface{}{
		"alias":        ledgerId,
		"amount":       1,
		"allocationId": allocationId.(string),
		"metadata":     metadata,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("failed to marshal fulfill payload: %v", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return false, err
	}

	req.Header.Set("x-usage-key", u.APIKey)
	req.Header.Set("Content-Type", "application/json")

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

	var responseData map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &responseData); err == nil {
		if eventId, exists := responseData["eventId"]; exists {
			c.Set("eventId", eventId)
		}
	}

	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

// collectRequestMetadata gathers metadata from the request
func (u *UsageFlowAPI) collectRequestMetadata(c *gin.Context) map[string]interface{} {
	metadata := map[string]interface{}{
		"applicationId": u.ApplicationId,
		"method":        c.Request.Method,
		"url":           GetPatternedURL(c), // Route pattern
		"rawUrl":        c.Request.URL.Path, // Raw URL
		"clientIP":      c.ClientIP(),
		"userAgent":     c.GetHeader("User-Agent"),
		"timestamp":     time.Now().Format(time.RFC3339),
	}

	// Collect headers
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

	// Collect query parameters
	queryParams := make(map[string]string)
	for k, v := range c.Request.URL.Query() {
		if len(v) > 0 {
			queryParams[k] = v[0]
		}
	}
	metadata["queryParams"] = queryParams

	if params := c.Params; len(params) > 0 {
		if params := c.Params; len(params) > 0 {
			paramsMap := make(map[string]string)
			for _, param := range params {
				paramsMap[param.Key] = param.Value
			}
			metadata["pathParams"] = paramsMap
		}
	}

	// Collect request body if present
	if c.Request.Body != nil && c.Request.Body != http.NoBody {
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err == nil {
			// Restore the body for further processing
			c.Request.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))

			// Try to parse as JSON
			var bodyJSON map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &bodyJSON); err == nil {
				metadata["body"] = bodyJSON
			} else {
				// Store as string if not JSON
				metadata["body"] = string(bodyBytes)
			}
		}
	}

	return metadata
}

// GuessLedgerId attempts to extract a ledger ID from various sources
func (u *UsageFlowAPI) GuessLedgerId(c *gin.Context) string {
	// Try to get from header

	method := c.Request.Method
	url := GetPatternedURL(c)

	return fmt.Sprintf("%s %s", method, url)
}

func isWhitelisted(method, url string, whiteListMap map[string]map[string]bool) bool {
	// Check exact method match
	if methodRoutes, exists := whiteListMap[method]; exists {
		if methodRoutes[url] || methodRoutes["*"] {
			return true
		}
	}

	// Check wildcard method
	if allMethodsRoutes, exists := whiteListMap["*"]; exists {
		if allMethodsRoutes[url] || allMethodsRoutes["*"] {
			return true
		}
	}

	return false
}

func isRouteMonitored(method, url string, routesMap map[string]map[string]bool) bool {
	// Check exact method match
	if methodRoutes, exists := routesMap[method]; exists {
		if methodRoutes[url] || methodRoutes["*"] {
			return true
		}
	}

	// Check wildcard method
	if allMethodsRoutes, exists := routesMap["*"]; exists {
		if allMethodsRoutes[url] || allMethodsRoutes["*"] {
			return true
		}
	}

	return false
}

func (u *UsageFlowAPI) processRequest(c *gin.Context, method, url string) error {
	// Implementation of request processing logic
	// This would include your existing logic for handling requests
	return nil
}

// GetUserPrefix attempts to extract a user identifier prefix based on the API configuration
func (u *UsageFlowAPI) GetUserPrefix(c *gin.Context, method, url string) string {
	u.mu.RLock()
	config := u.ApiConfig
	policyMap := u.policyMap
	u.mu.RUnlock()

	if config == nil {
		return ""
	}

	// First try to find a matching policy using the map
	policyKey := fmt.Sprintf("%s:%s", method, url)
	policy, ok := policyMap[policyKey]
	if ok {
		policyMethod := strings.Split(policyKey, ":")[0]
		policyPattern := strings.Split(policyKey, ":")[1]

		if policyMethod == method && policyPattern == url {
			// Found matching policy, use its identity configuration
			var identifier string
			switch policy.IdentityLocation {
			case "header":
				identifier = c.GetHeader(policy.IdentityField)
			case "query":
				identifier = c.Query(policy.IdentityField)
			case "path_params":
				identifier = c.Param(policy.IdentityField)
			case "query_params":
				identifier = c.Query(policy.IdentityField)
			case "body":
				bodyBytes, err := io.ReadAll(c.Request.Body)
				if err == nil {
					// Restore the body for further processing
					c.Request.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))

					// Try to parse as JSON for policy matching
					var bodyMap map[string]interface{}
					if err := json.Unmarshal(bodyBytes, &bodyMap); err == nil {
						// Use bodyMap for your logic
						if policy.IdentityLocation == "body" {
							if val, ok := bodyMap[policy.IdentityField]; ok {
								if strVal, ok := val.(string); ok {
									identifier = strVal
								}
							}
						}
					}
				}
			case "bearer_token":
				if token, err := ExtractBearerToken(c); err == nil {
					if claims, err := DecodeJWTUnverified(token); err == nil {
						if val, ok := claims[policy.IdentityField]; ok {
							if strVal, ok := val.(string); ok {
								identifier = strVal
							}
						}
					}
				}
			}

			if identifier != "" {
				return TransformToLedgerId(identifier)
			}
		}
	}

	// If no matching policy found or no identifier from policy, fall back to base config
	var identifier string
	switch config.IdentityFieldLocation {
	case "header":
		identifier = c.GetHeader(config.IdentityFieldName)
	case "query":
		identifier = c.Query(config.IdentityFieldName)
	case "path_params":
		identifier = c.Param(config.IdentityFieldName)
	case "query_params":
		identifier = c.Query(config.IdentityFieldName)
	case "body":
		var bodyMap map[string]interface{}
		if err := c.ShouldBindJSON(&bodyMap); err == nil {
			if val, ok := bodyMap[config.IdentityFieldName]; ok {
				if strVal, ok := val.(string); ok {
					identifier = strVal
				}
			}
		}
		// Restore the body for further processing
		if body, err := GetRequestBody(c); err == nil {
			c.Request.Body = ioutil.NopCloser(bytes.NewBufferString(body))
		}
	case "bearer_token":
		if token, err := ExtractBearerToken(c); err == nil {
			if claims, err := DecodeJWTUnverified(token); err == nil {
				if val, ok := claims[config.IdentityFieldName]; ok {
					if strVal, ok := val.(string); ok {
						identifier = strVal
					}
				}
			}
		}
	}

	if identifier != "" {
		return TransformToLedgerId(identifier)
	}

	return ""
}

// GetApplicationEndpointPolicies fetches the endpoint policies for the current application
func (u *UsageFlowAPI) GetApplicationEndpointPolicies() *config.PolicyResponse {
	u.mu.RLock()
	applicationId := u.ApplicationId
	u.mu.RUnlock()

	if applicationId == "" {
		return nil
	}

	policies, err := api.GetApplicationEndpointPolicies(u.APIKey, applicationId)
	if err != nil {
		return nil
	}

	return policies
}

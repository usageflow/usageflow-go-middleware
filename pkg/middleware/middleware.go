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
	"github.com/usageflow/usageflow-go-middleware/pkg/config"
	"github.com/usageflow/usageflow-go-middleware/pkg/socket"
)

type PolicyMap map[string]*config.ApplicationEndpointPolicy

type UsageFlowAPI struct {
	APIKey                      string                     `json:"apiKey"`
	ApplicationId               string                     `json:"applicationId"`
	ApiConfig                   []config.ApiConfigStrategy `json:"apiConfig"`
	ApplicationEndpointPolicies *config.PolicyResponse     `json:"applicationEndpointPolicies"`
	policyMap                   PolicyMap
	mu                          sync.RWMutex
	socketManager               *socket.UsageFlowSocketManager
	connected                   bool // Tracks socket connection status
}

// New creates a new instance of UsageFlowAPI
func New(apiKey string) *UsageFlowAPI {
	socketManager := socket.NewUsageFlowSocketManager(apiKey)
	api := &UsageFlowAPI{
		policyMap:     make(PolicyMap),
		socketManager: socketManager,
		connected:     socketManager.IsConnected(), // Initialize connection status
	}
	api.StartConfigUpdater()
	return api
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

		success, err := u.ExecuteRequestWithMetadata(ledgerId, method, url, metadata, c)
		if err != nil {
			// If socket is not connected, continue normally instead of aborting
			u.mu.RLock()
			connected := u.connected
			u.mu.RUnlock()
			if !connected {
				c.Next()
				return
			}
			c.AbortWithStatusJSON(500, gin.H{"error": "Failed to process request"})
			return
		}
		if !success {
			// If socket is not connected, continue normally instead of aborting
			u.mu.RLock()
			connected := u.connected
			u.mu.RUnlock()
			if !connected {
				c.Next()
				return
			}
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

func (u *UsageFlowAPI) FetchApiConfig() ([]config.ApiConfigStrategy, error) {
	response, err := u.socketManager.SendAsync(&socket.UsageFlowSocketMessage{
		Type: "get_application_policies",
	})

	if err != nil {
		return nil, err
	}

	// The response payload is a map[string]interface{} with "policies" and "total" keys
	payloadMap, ok := response.Payload.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response payload type: %T", response.Payload)
	}

	// Convert the map to PolicyListResponse
	var policyList config.PolicyListResponse

	// Handle policies array
	if policiesVal, ok := payloadMap["policies"]; ok {
		policiesBytes, err := json.Marshal(policiesVal)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal policies: %v", err)
		}
		if err := json.Unmarshal(policiesBytes, &policyList.Policies); err != nil {
			return nil, fmt.Errorf("failed to unmarshal policies: %v", err)
		}
	}

	// Handle total
	if totalVal, ok := payloadMap["total"]; ok {
		if totalFloat, ok := totalVal.(float64); ok {
			policyList.Total = int(totalFloat)
		}
	}

	// Update ApiConfig with lock
	u.mu.Lock()
	u.ApiConfig = policyList.Policies
	u.mu.Unlock()

	return policyList.Policies, nil
}

func (u *UsageFlowAPI) allocateRequest(ledgerId string, amount *float64, metadata map[string]interface{}) (string, error) {
	// Check if socket is connected (this updates the status)
	connected := u.isConnected()

	// If not connected, skip and return empty allocation ID (continue normally)
	if !connected {
		return "", nil
	}

	var amt float64 = 1

	if amount != nil {
		amt = *amount
	}

	payload := &socket.RequestForAllocation{
		Alias:    ledgerId,
		Amount:   amt,
		Metadata: metadata,
	}
	response, err := u.socketManager.SendAsync(&socket.UsageFlowSocketMessage{
		Type:    "request_for_allocation",
		Payload: payload,
	})
	if err != nil {
		// Update connection status on error
		u.mu.Lock()
		u.connected = false
		u.mu.Unlock()
		// Return empty string to continue normally
		return "", nil
	}

	if response.Error != "" {
		return "", fmt.Errorf("failed to allocate request: %s", response.Error)
	}

	// The response payload is a map[string]interface{} with "allocationId" key
	payloadMap, ok := response.Payload.(map[string]interface{})
	if !ok {
		return "", nil // Continue normally on unexpected response
	}

	allocationId, ok := payloadMap["allocationId"].(string)
	if !ok {
		return "", nil // Continue normally if allocationId not found
	}

	return allocationId, nil
}

func (u *UsageFlowAPI) useAllocationRequest(ledgerId string, amount *float64, allocationId string, metadata map[string]interface{}) (bool, error) {
	// Check if socket is connected
	connected := u.isConnected()

	// If not connected, skip and return success (continue normally)
	if !connected {
		return true, nil
	}

	// If no allocationId was provided (because we skipped allocation), just return success
	if allocationId == "" {
		return true, nil
	}

	var amt float64 = 1

	if amount != nil {
		amt = *amount
	}

	payload := &socket.UseAllocationRequest{
		Alias:        ledgerId,
		Amount:       amt,
		AllocationID: allocationId,
		Metadata:     metadata,
	}
	response, err := u.socketManager.SendAsync(&socket.UsageFlowSocketMessage{
		Type:    "use_allocation",
		Payload: payload,
	})
	if err != nil {
		// Update connection status on error
		u.mu.Lock()
		u.connected = false
		u.mu.Unlock()
		// Return success to continue normally
		return true, nil
	}

	// The response payload is a map[string]interface{}
	// We just need to verify the response was successful
	_, ok := response.Payload.(map[string]interface{})
	if !ok {
		// Continue normally on unexpected response
		return true, nil
	}

	return true, nil
}

// ExecuteRequestWithMetadata executes the initial allocation request
func (u *UsageFlowAPI) ExecuteRequestWithMetadata(ledgerId, method, url string, metadata map[string]interface{}, c *gin.Context) (bool, error) {
	amount := float64(1)
	allocationId, err := u.allocateRequest(ledgerId, &amount, metadata)
	if err != nil {
		return false, err
	}
	c.Set("eventId", allocationId)

	return true, nil
}

func (u *UsageFlowAPI) isConnected() bool {
	// Always check the actual connection status from socket manager
	if u.socketManager != nil {
		connected := u.socketManager.IsConnected()
		u.mu.Lock()
		u.connected = connected
		u.mu.Unlock()
		return connected
	}

	u.mu.RLock()
	connected := u.connected
	u.mu.RUnlock()
	return connected
}

// ExecuteFulfillRequestWithMetadata executes the fulfill request after the main request is processed
func (u *UsageFlowAPI) ExecuteFulfillRequestWithMetadata(ledgerId, method, url string, metadata map[string]interface{}, c *gin.Context) (bool, error) {
	// Check if socket is connected
	connected := u.isConnected()

	// If not connected, skip and return success (continue normally)
	if !connected {
		return true, nil
	}

	allocationId, exists := c.Get("eventId")
	if !exists {
		// No allocation ID means we skipped allocation (socket was not connected)
		// Return success to continue normally
		return true, nil
	}

	startTime, exists := c.Get("usageflowStartTime")
	if !exists {
		// No start time, but continue normally
		return true, nil
	}
	requestDuration := time.Since(startTime.(time.Time))

	metadata["requestDuration"] = requestDuration.Milliseconds()

	amount := float64(1)

	success, err := u.useAllocationRequest(ledgerId, &amount, allocationId.(string), metadata)
	if err != nil {
		// On error, return success to continue normally
		return true, nil
	}
	return success, nil
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

// GetUserPrefix attempts to extract a user identifier prefix based on the API configuration
func (u *UsageFlowAPI) GetUserPrefix(c *gin.Context, method, url string) string {
	u.mu.RLock()
	config := u.ApiConfig
	u.mu.RUnlock()

	if config == nil {
		return ""
	}

	var identifier string

	// Find matching config for current method and url
	for _, cfg := range config {
		// Check if this config matches the current method and url
		if cfg.Method != method || cfg.Url != url {
			continue
		}

		// Skip if identity fields are not configured
		if cfg.IdentityFieldLocation == nil || cfg.IdentityFieldName == nil {
			continue
		}

		// If no matching policy found or no identifier from policy, fall back to base config
		switch *cfg.IdentityFieldLocation {
		case "headers":
			identifier = c.GetHeader(*cfg.IdentityFieldName)
		case "query":
			identifier = c.Query(*cfg.IdentityFieldName)
		case "path_params":
			identifier = c.Param(*cfg.IdentityFieldName)
		case "query_params":
			identifier = c.Query(*cfg.IdentityFieldName)
		case "body":
			var bodyMap map[string]interface{}
			if err := c.ShouldBindJSON(&bodyMap); err == nil {
				if val, ok := bodyMap[*cfg.IdentityFieldName]; ok {
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
					if val, ok := claims[*cfg.IdentityFieldName]; ok {
						if strVal, ok := val.(string); ok {
							identifier = strVal
						}
					}
				}
			}
		}

		// If we found an identifier, break out of the loop
		if identifier != "" {
			break
		}
	}

	if identifier != "" {
		return TransformToLedgerId(identifier)
	}

	return ""
}

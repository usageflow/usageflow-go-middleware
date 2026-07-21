package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/config"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/socket"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/tracker"
)

type PolicyMap map[string]*config.ApplicationEndpointPolicy

type socketManager interface {
	Send(*socket.UsageFlowSocketMessage) error
	SendAsync(*socket.UsageFlowSocketMessage) (*socket.UsageFlowSocketResponse, error)
	IsConnected() bool
	Close()
}

type UsageFlowAPI struct {
	APIKey                      string                     `json:"apiKey"`
	ApplicationId               string                     `json:"applicationId"`
	ApiConfig                   []config.ApiConfigStrategy `json:"apiConfig"`
	BlockedEndpoints            map[string]bool            `json:"blockedEndpoints"`
	ApplicationEndpointPolicies *config.PolicyResponse     `json:"applicationEndpointPolicies"`
	WhitelistEndpoints          []config.Route             `json:"whitelistEndpoints"`
	MonitoringPaths             []config.Route             `json:"monitoringPaths"`
	policyMap                   PolicyMap
	mu                          sync.RWMutex
	updaterOnce                 sync.Once
	socketManager               socketManager
	connected                   bool // Tracks socket connection status
	monitoringPathsMap          map[string]map[string]bool
	whitelistEndpointsMap       map[string]map[string]bool
	localWhitelist              []config.Route
	// reportAllFunctionAllocations meters every discovered function (JS default true).
	reportAllFunctionAllocations bool
	// forceMonitorAll ignores remote monitoringPaths and meters every non-whitelisted route.
	forceMonitorAll bool
	// functionPolicies indexes FUNCTION strategies by "METHOD url func:path:name".
	functionPolicies map[string]config.ApiConfigStrategy
}

// New creates a new instance of UsageFlowAPI
func New(apiKey string) *UsageFlowAPI {
	socketManager := socket.NewUsageFlowSocketManager(apiKey)
	api := &UsageFlowAPI{
		policyMap:                    make(PolicyMap),
		socketManager:                socketManager,
		connected:                    socketManager.IsConnected(), // Initialize connection status
		reportAllFunctionAllocations: true,
		functionPolicies:             make(map[string]config.ApiConfigStrategy),
	}
	api.wireFunctionAllocationCallbacks()
	api.StartConfigUpdater()
	return api
}

// Whitelist adds routes that bypass metering (merged with server whitelist on each config refresh).
func (u *UsageFlowAPI) Whitelist(routes ...config.Route) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.localWhitelist = append(u.localWhitelist, routes...)
	if u.whitelistEndpointsMap == nil {
		u.whitelistEndpointsMap = make(map[string]map[string]bool)
	}
	for _, route := range routes {
		if route.Method == "" || route.URL == "" {
			continue
		}
		if _, exists := u.whitelistEndpointsMap[route.Method]; !exists {
			u.whitelistEndpointsMap[route.Method] = make(map[string]bool)
		}
		u.whitelistEndpointsMap[route.Method][route.URL] = true
	}
}

// ForceMonitorAll meters every non-whitelisted HTTP route even when the Console
// application has a narrower monitoringPaths list (useful when dogfooding a
// second service under an existing API key).
func (u *UsageFlowAPI) ForceMonitorAll() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.forceMonitorAll = true
}

// RequestInterceptor creates a Gin middleware for intercepting requests
func (u *UsageFlowAPI) RequestInterceptor() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		url := GetPatternedURL(c)

		// Establish request-scoped tracking for Track/Wrap (fail soft).
		trackingStore := u.beginTracking(c, method, url)
		defer u.finishTracking(c, method, url, trackingStore)

		// Route maps are replaced during config refreshes and may also be updated
		// by Whitelist, so evaluate both decisions under the same read lock.
		u.mu.RLock()
		whitelisted := isWhitelisted(method, url, u.whitelistEndpointsMap)
		forceAll := u.forceMonitorAll
		monitored := isRouteMonitored(method, url, u.monitoringPathsMap)
		u.mu.RUnlock()

		if whitelisted {
			c.Next()
			return
		}

		// JS/Python parity: empty monitoringPaths => monitor all routes.
		// ForceMonitorAll ignores remote monitoringPaths entirely.
		if !forceAll && !monitored {
			c.Next()
			return
		}

		//Capture the time before the request is processed
		startTime := time.Now()
		usageflowRequestId := uuid.New().String()
		c.Set("usageflowStartTime", startTime)
		tracker.SetUsageflowRequestID(c.Request.Context(), usageflowRequestId)

		// Process request with UsageFlow logic
		metadata := u.collectRequestMetadata(c)
		metadata["usageflowRequestId"] = usageflowRequestId
		ledgerId := u.GuessLedgerId(c)
		userIdentifierSuffix, rateLimited := u.GetUserPrefix(c, method, url)

		if userIdentifierSuffix != "" {
			ledgerId = fmt.Sprintf("%s %s", ledgerId, userIdentifierSuffix)
		}

		success, err := u.ExecuteRequestWithMetadata(ledgerId, method, url, metadata, c, rateLimited)
		if field := u.lookupAPIResponseTrackingField(method, url); field != "" {
			c.Set("responseTrackingField", field)
		}
		if err != nil {
			errorMessage := err.Error()
			if errorMessage == "endpoints is blocked" {
				c.AbortWithStatusJSON(403, gin.H{"error": "endpoint_blocked", "message": "UsageFlow blocked this endpoint by policy rule."})
				return
			}

			// A configured rate limit must fail closed. Otherwise a WebSocket
			// timeout can let the handler return 2xx while UsageFlow later
			// records a quota denial.
			if rateLimited {
				c.AbortWithStatusJSON(429, gin.H{"error": "rate_limit_exceeded", "message": "UsageFlow could not authorize this rate-limited request."})
				return
			}

			// Non-rate-limited metering remains fail-open.
			u.mu.RLock()
			connected := u.connected
			u.mu.RUnlock()
			if !connected {
				c.Next()
				return
			}

			c.AbortWithStatusJSON(429, gin.H{"error": "rate_limit_exceeded", "message": "UsageFlow blocked this request because the rate limit or quota was exceeded."})
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

		// Process the original request (capture body for responseSchema / metering).
		blw := attachBodyCapture(c)
		c.Next()

		// After the request is processed, execute the fulfill request
		metadata["responseStatusCode"] = c.Writer.Status()

		responseTrackingField := ""
		if field, ok := c.Get("responseTrackingField"); ok {
			if s, ok := field.(string); ok {
				responseTrackingField = s
			}
		}
		amount := enrichFulfillMetadataWithResponse(metadata, blw, responseTrackingField)
		c.Set("usageflowAmount", amount)

		if _, err := u.ExecuteFulfillRequestWithMetadata(ledgerId, method, url, metadata, c); err != nil {
			// Fail soft after the handler already completed.
			_ = err
		}
	}
}

// beginTracking attaches a per-request tracking context when discovery is enabled.
func (u *UsageFlowAPI) beginTracking(c *gin.Context, method, url string) *tracker.TrackingContext {
	defer func() {
		_ = recover()
	}()
	if !tracker.IsEnabled() {
		return nil
	}
	ctx, store := tracker.WithTracking(c.Request.Context(), &tracker.RequestContext{
		Method: method,
		URL:    url,
	}, "")
	c.Request = c.Request.WithContext(ctx)
	return store
}

// finishTracking sends report_call_chain after the handler (fail soft).
func (u *UsageFlowAPI) finishTracking(c *gin.Context, method, url string, store *tracker.TrackingContext) {
	defer func() {
		_ = recover()
	}()
	if store == nil || !tracker.IsEnabled() {
		return
	}
	callChain := store.Snapshot()
	if len(callChain) == 0 {
		return
	}
	u.reportCallChain(method, url, store.RequestID(), callChain)
}

// reportCallChain sends the call chain over the WebSocket when connected.
func (u *UsageFlowAPI) reportCallChain(method, url, usageflowRequestID string, callChain []tracker.FunctionCallRecord) {
	defer func() {
		_ = recover()
	}()
	if len(callChain) == 0 || !u.isConnected() {
		return
	}
	_ = u.socketManager.Send(&socket.UsageFlowSocketMessage{
		Type: "report_call_chain",
		Payload: &socket.ReportCallChainPayload{
			Method:             method,
			URL:                url,
			CallChain:          callChain,
			Timestamp:          time.Now().UTC().Format(time.RFC3339),
			UsageflowRequestID: usageflowRequestID,
		},
	})
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
	u.syncFunctionPolicies(policyList.Policies)

	return policyList.Policies, nil
}

func (u *UsageFlowAPI) FetchApplicationConfig() (config.ApplicationConfigResponse, error) {
	response, err := u.socketManager.SendAsync(&socket.UsageFlowSocketMessage{
		Type: "get_application_config",
	})

	var applicationConfigResponse config.ApplicationConfigResponse
	if err != nil {
		return applicationConfigResponse, err
	}

	// The response payload is a map[string]interface{}
	payloadMap, ok := response.Payload.(map[string]interface{})
	if !ok {
		return applicationConfigResponse, fmt.Errorf("unexpected payload type for application config")
	}

	// Marshal the map to JSON bytes, then unmarshal into the struct
	payloadBytes, err := json.Marshal(payloadMap)
	if err != nil {
		return applicationConfigResponse, fmt.Errorf("failed to marshal application config payload: %v", err)
	}

	if err := json.Unmarshal(payloadBytes, &applicationConfigResponse); err != nil {
		return applicationConfigResponse, fmt.Errorf("failed to unmarshal application config: %v", err)
	}

	if err := u.applyRouteConfig(applicationConfigResponse); err != nil {
		return applicationConfigResponse, err
	}

	// Honor server discoveryDisabled (JS/Python parity). Env USAGEFLOW_DISCOVERY_DISABLED also disables.
	if applicationConfigResponse.DiscoveryDisabled != nil {
		tracker.SetEnabled(!*applicationConfigResponse.DiscoveryDisabled)
	} else {
		tracker.Enable()
	}

	return applicationConfigResponse, nil
}

func (u *UsageFlowAPI) applyRouteConfig(applicationConfigResponse config.ApplicationConfigResponse) error {
	whitelistEndpoints, err := ConvertToType[[]config.Route](applicationConfigResponse.WhitelistEndpoints)
	if err != nil {
		return fmt.Errorf("failed to convert whitelist endpoints: %v", err)
	}
	monitorPaths, err := ConvertToType[[]config.Route](applicationConfigResponse.MonitorPaths)
	if err != nil {
		return fmt.Errorf("failed to convert monitor paths: %v", err)
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	u.WhitelistEndpoints = whitelistEndpoints
	u.MonitoringPaths = monitorPaths

	u.monitoringPathsMap = routesToMap(u.MonitoringPaths)
	u.whitelistEndpointsMap = routesToMap(u.WhitelistEndpoints, u.localWhitelist)

	if applicationConfigResponse.ReportAllFunctionAllocations != nil {
		u.reportAllFunctionAllocations = *applicationConfigResponse.ReportAllFunctionAllocations
	} else {
		u.reportAllFunctionAllocations = true
	}

	return nil
}

func routesToMap(routeSets ...[]config.Route) map[string]map[string]bool {
	routesMap := make(map[string]map[string]bool)
	for _, routes := range routeSets {
		for _, route := range routes {
			if route.Method == "" || route.URL == "" {
				continue
			}
			if routesMap[route.Method] == nil {
				routesMap[route.Method] = make(map[string]bool)
			}
			routesMap[route.Method][route.URL] = true
		}
	}
	return routesMap
}

func (u *UsageFlowAPI) FetchBlockedEndpoints() error {
	response, err := u.socketManager.SendAsync(&socket.UsageFlowSocketMessage{
		Type: "get_blocked_endpoints",
	})

	if err != nil {
		return nil
	}

	// Convert the response payload to BlockedEndpoints
	var blockedEndpointsResponse config.BlockedEndpointsResponse

	// The response payload is a map[string]interface{}
	payloadMap, ok := response.Payload.(map[string]interface{})
	if !ok {
		return fmt.Errorf("unexpected payload type for blocked endpoints")
	}

	// Marshal the map to JSON bytes, then unmarshal into the struct
	payloadBytes, err := json.Marshal(payloadMap)
	if err != nil {
		return fmt.Errorf("failed to marshal blocked endpoints payload: %v", err)
	}

	if err := json.Unmarshal(payloadBytes, &blockedEndpointsResponse); err != nil {
		return fmt.Errorf("failed to unmarshal blocked endpoints: %v", err)
	}

	if len(blockedEndpointsResponse.Endpoints) == 0 {
		u.mu.Lock()
		u.BlockedEndpoints = make(map[string]bool)
		u.mu.Unlock()

		return nil
	}

	blockedEndpointsMap := make(map[string]bool)

	for _, endpoint := range blockedEndpointsResponse.Endpoints {
		blockKey := fmt.Sprintf("%s %s", endpoint.Method, endpoint.Url)
		if endpoint.Identity != "" {
			blockKey = fmt.Sprintf("%s %s", blockKey, endpoint.Identity)
		}
		blockedEndpointsMap[blockKey] = true
	}

	u.mu.Lock()
	u.BlockedEndpoints = blockedEndpointsMap
	u.mu.Unlock()

	return nil
}

func (u *UsageFlowAPI) allocateRequest(ledgerId string, amount *float64, metadata map[string]interface{}, rateLimited bool) (string, error) {
	// Check if socket is connected (this updates the status)
	connected := u.isConnected()

	// Rate-limited requests must not reach the handler without authorization.
	if !connected {
		if rateLimited {
			return "", fmt.Errorf("rate-limit authorization unavailable: WebSocket not connected")
		}
		return "", nil
	}

	found := u.BlockedEndpoints[ledgerId]
	if found {
		return "", fmt.Errorf("endpoints is blocked")
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

	if !rateLimited {
		allocationId := uuid.New().String()
		payload.AllocationID = &allocationId

		u.socketManager.Send(&socket.UsageFlowSocketMessage{
			Type:    "request_for_allocation",
			Payload: payload,
		})

		return allocationId, nil
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
		return "", fmt.Errorf("rate-limit authorization failed: %w", err)
	}

	// Match function metering: server may deny via error field and/or type:"error".
	if response.Error != "" || strings.EqualFold(response.Type, "error") {
		msg := response.Error
		if msg == "" {
			msg = response.Message
		}
		if msg == "" {
			msg = "allocation denied"
		}
		return "", fmt.Errorf("failed to allocate request: %s", msg)
	}

	// The response payload is a map[string]interface{} with "allocationId" key
	payloadMap, ok := response.Payload.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("rate-limit authorization returned unexpected payload type %T", response.Payload)
	}

	allocationId, ok := payloadMap["allocationId"].(string)
	if !ok || allocationId == "" {
		return "", fmt.Errorf("rate-limit authorization response is missing allocationId")
	}

	return allocationId, nil
}

func (u *UsageFlowAPI) useAllocationRequest(ledgerId string, amount *float64, allocationId string, metadata map[string]interface{}, rateLimited bool) (bool, error) {
	// Check if socket is connected
	connected := u.isConnected()

	// Rate-limited requests must settle before the handler is authorized.
	if !connected {
		if rateLimited {
			return false, fmt.Errorf("rate-limit settlement unavailable: WebSocket not connected")
		}
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
		Alias:               ledgerId,
		Amount:              amt,
		AllocationID:        allocationId,
		WaitForConfirmation: rateLimited,
		Metadata:            metadata,
	}

	if rateLimited {
		response, err := u.socketManager.SendAsync(&socket.UsageFlowSocketMessage{
			Type:    "use_allocation",
			Payload: payload,
		})
		if err != nil {
			u.mu.Lock()
			u.connected = false
			u.mu.Unlock()
			return false, fmt.Errorf("rate-limit settlement failed: %w", err)
		}
		if response.Error != "" || strings.EqualFold(response.Type, "error") {
			msg := response.Error
			if msg == "" {
				msg = response.Message
			}
			if msg == "" {
				msg = "settlement denied"
			}
			return false, fmt.Errorf("rate-limit settlement denied: %s", msg)
		}

		return true, nil
	}

	u.socketManager.Send(&socket.UsageFlowSocketMessage{
		Type:    "use_allocation",
		Payload: payload,
	})

	return true, nil
}

// ExecuteRequestWithMetadata executes the initial allocation request
func (u *UsageFlowAPI) ExecuteRequestWithMetadata(ledgerId, method, url string, metadata map[string]interface{}, c *gin.Context, rateLimited bool) (bool, error) {
	amount := float64(1)
	allocationId, err := u.allocateRequest(ledgerId, &amount, metadata, rateLimited)
	if err != nil {
		return false, err
	}

	c.Set("eventId", allocationId)
	c.Set("rateLimited", rateLimited)

	// Propagate HTTP allocation context so function-level metering can correlate.
	if store := tracker.FromContext(c.Request.Context()); store != nil {
		metaCopy := make(map[string]interface{}, len(metadata))
		for k, v := range metadata {
			metaCopy[k] = v
		}
		store.SetRequestAllocation(allocationId, ledgerId, metaCopy)
	}

	// Rate limits protect handler execution, so settle the default request unit
	// synchronously before c.Next(). The post-handler fulfill path is reserved
	// for non-rate-limited or response-derived metering.
	if rateLimited {
		success, err := u.useAllocationRequest(ledgerId, &amount, allocationId, metadata, true)
		if err != nil {
			return false, err
		}
		if !success {
			return false, fmt.Errorf("rate-limit settlement failed")
		}
		c.Set("usageflowSettledBeforeHandler", true)
	}

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

// IsConnected reports whether at least one UsageFlow WebSocket connection is active.
// It is safe to use for health checks and local integration demos.
func (u *UsageFlowAPI) IsConnected() bool {
	return u.isConnected()
}

// ExecuteFulfillRequestWithMetadata executes the fulfill request after the main request is processed
func (u *UsageFlowAPI) ExecuteFulfillRequestWithMetadata(ledgerId, method, url string, metadata map[string]interface{}, c *gin.Context) (bool, error) {
	if settled, ok := c.Get("usageflowSettledBeforeHandler"); ok {
		if settledBeforeHandler, ok := settled.(bool); ok && settledBeforeHandler {
			return true, nil
		}
	}

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

	rateLimited, _ := c.Get("rateLimited")
	isRateLimited, ok := rateLimited.(bool)
	if !ok {
		isRateLimited = false
	}

	amount := float64(1)
	if v, ok := c.Get("usageflowAmount"); ok {
		if n, ok := v.(float64); ok {
			amount = n
		}
	}

	success, err := u.useAllocationRequest(ledgerId, &amount, allocationId.(string), metadata, isRateLimited)
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
		paramsMap := make(map[string]string)
		for _, param := range params {
			paramsMap[param.Key] = param.Value
		}
		metadata["pathParams"] = paramsMap
	}

	// Collect request body if present
	if c.Request.Body != nil && c.Request.Body != http.NoBody {
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err == nil {
			// Restore the body for further processing
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			// Try to parse as JSON — store as requestBody (Console expects body = response).
			var bodyJSON interface{}
			if err := json.Unmarshal(bodyBytes, &bodyJSON); err == nil {
				metadata["requestBody"] = bodyJSON
			} else {
				metadata["requestBody"] = string(bodyBytes)
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
	// JS/Python parity: when monitoringPaths is empty, monitor every route.
	if len(routesMap) == 0 {
		return true
	}

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
func (u *UsageFlowAPI) GetUserPrefix(c *gin.Context, method, url string) (string, bool) {
	u.mu.RLock()
	config := u.ApiConfig
	u.mu.RUnlock()

	if config == nil {
		return "", false
	}

	var identifier string
	var rateLimited bool
	var matched bool

	// Find matching config for current method and url
	for _, cfg := range config {
		// Check if this config matches the current method and url
		if cfg.Method != method || cfg.Url != url {
			continue
		}
		matched = true
		if cfg.HasRateLimit {
			rateLimited = true
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
				c.Request.Body = io.NopCloser(bytes.NewBufferString(body))
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
		case "cookie":
			// Handle JWT cookie format: '[technique=jwt]cookieName[pick=claim]'
			jwtCookieInfo := ParseJwtCookieField(*cfg.IdentityFieldName)
			if jwtCookieInfo != nil {
				cookieValue := GetCookieValue(c, jwtCookieInfo.CookieName)
				if cookieValue != "" {
					claims, err := DecodeJWTUnverified(cookieValue)
					if err == nil {
						if val, ok := claims[jwtCookieInfo.Claim]; ok {
							if strVal, ok := val.(string); ok {
								identifier = strVal
								break
							}
						}
					}
				}
				break
			}

			// Handle standard cookie access (e.g., "cookie.session" or "session")
			var cookieValue string
			fieldName := strings.ToLower(*cfg.IdentityFieldName)
			if strings.HasPrefix(fieldName, "cookie.") {
				// Remove "cookie." prefix
				cookieName := (*cfg.IdentityFieldName)[7:]
				cookieValue = GetCookieValue(c, cookieName)
			} else {
				// Use the field name directly as cookie name
				cookieValue = GetCookieValue(c, *cfg.IdentityFieldName)
			}

			if cookieValue != "" {
				identifier = cookieValue
			}
		}

		// If we found an identifier, break out of the loop
		if identifier != "" {
			break
		}
	}

	if !matched {
		return "", false
	}

	// Keep rateLimited even when identity is missing so hasRateLimit policies
	// still wait on allocation and can return 429 instead of fire-and-forget.
	if identifier != "" {
		return TransformToLedgerId(identifier), rateLimited
	}

	return "", rateLimited
}

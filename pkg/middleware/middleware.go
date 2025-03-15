package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/usageflow/usageflow-go-middleware/pkg/api"
	"github.com/usageflow/usageflow-go-middleware/pkg/config"
)

// UsageFlowAPI represents the main middleware structure
type UsageFlowAPI struct {
	APIKey        string                    `json:"apiKey"`
	ApplicationId string                    `json:"applicationId"`
	ApiConfig     *config.ApiConfigStrategy `json:"apiConfig"`
	mu            sync.RWMutex
}

// New creates a new instance of UsageFlowAPI
func New(apiKey string) *UsageFlowAPI {
	api := &UsageFlowAPI{}
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

		// Process request with UsageFlow logic
		metadata := collectRequestMetadata(c)
		ledgerId := u.GuessLedgerId(c)
		userIdentifierPrefix := u.GetUserPrefix(c, method, url)

		if userIdentifierPrefix != "" {
			ledgerId = fmt.Sprintf("%s %s", userIdentifierPrefix, ledgerId)
		}

		if err := api.ExecuteRequest(u.APIKey, ledgerId, method, url, metadata); err != nil {
			c.AbortWithStatusJSON(500, gin.H{"error": "Failed to process request"})
			return
		}

		c.Next()
	}
}

// collectRequestMetadata gathers metadata from the request
func collectRequestMetadata(c *gin.Context) map[string]interface{} {
	metadata := make(map[string]interface{})

	// Collect headers
	headers := make(map[string]string)
	for k, v := range c.Request.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	metadata["headers"] = headers

	// Collect query parameters
	queryParams := make(map[string]string)
	for k, v := range c.Request.URL.Query() {
		if len(v) > 0 {
			queryParams[k] = v[0]
		}
	}
	metadata["queryParams"] = queryParams

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

	// if true {
	return fmt.Sprintf("%s %s", method, url)
	// }

	// if ledgerId := c.GetHeader("x-ledger-id"); ledgerId != "" {
	// 	return TransformToLedgerId(ledgerId)
	// }

	// // Try to get from query parameter
	// if ledgerId := c.Query("ledgerId"); ledgerId != "" {
	// 	return TransformToLedgerId(ledgerId)
	// }

	// // Try to get from JWT token
	// if token, err := ExtractBearerToken(c); err == nil {
	// 	if claims, err := DecodeJWTUnverified(token); err == nil {
	// 		if ledgerId, ok := claims["sub"].(string); ok {
	// 			return TransformToLedgerId(ledgerId)
	// 		}
	// 	}
	// }

	// // Default to empty string if no ledger ID found
	// return ""
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
	u.mu.RUnlock()

	if config == nil {
		return ""
	}

	// Get the identifier based on the configured location
	var identifier string
	switch config.IdentityFieldLocation {
	case "header":
		identifier = c.GetHeader(config.IdentityFieldName)
	case "query":
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
	case "jwt":
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

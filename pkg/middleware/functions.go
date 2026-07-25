package middleware

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/config"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/socket"
	"github.com/usageflow/usageflow-go-middleware/v2/pkg/tracker"
)

func (u *UsageFlowAPI) wireFunctionAllocationCallbacks() {
	tracker.SetAllocationCallbacks(u.onDiscoveredFunctionStart, u.onDiscoveredFunctionEnd)
}

func (u *UsageFlowAPI) onDiscoveredFunctionStart(store *tracker.TrackingContext, funcName, filePath string) (string, error) {
	defer func() { _ = recover() }()

	u.mu.RLock()
	reportAll := u.reportAllFunctionAllocations
	policy, hasPolicy := u.lookupFunctionPolicyLocked(store, filePath, funcName)
	u.mu.RUnlock()

	if !reportAll && !hasPolicy {
		return "", nil
	}
	if !u.isConnected() {
		return "", nil
	}
	if store == nil || (store.RequestID() == "" && store.RequestContext == nil) {
		return "", nil
	}

	_, ledgerID, reqMeta := store.GetRequestAllocation()
	usageflowRequestID := store.RequestID()
	ledgerBase := ledgerID
	if ledgerBase == "" {
		ledgerBase = usageflowRequestID
	}
	if ledgerBase == "" {
		ledgerBase = "unknown"
	}
	functionLedgerID := fmt.Sprintf("%s func:%s:%s", ledgerBase, filePath, funcName)

	metadata := functionMetadataBase(store, reqMeta, funcName, filePath, usageflowRequestID)

	amount := 1.0
	if hasPolicy && policy.IsResponseTrackingEnabled && policy.ResponseTrackingField != nil && *policy.ResponseTrackingField != "" {
		amount = 1_000_000
	}

	payload := &socket.RequestForAllocation{
		Alias:    functionLedgerID,
		Amount:   amount,
		Metadata: metadata,
	}

	if hasPolicy && policy.HasRateLimit {
		response, err := u.socketManager.SendAsync(&socket.UsageFlowSocketMessage{
			Type:    "request_for_allocation",
			Payload: payload,
		})
		if err != nil {
			// Transport failure — fail open like JS/Python.
			return "", nil
		}
		if response.Error != "" || strings.EqualFold(response.Type, "error") {
			msg := response.Error
			if msg == "" {
				msg = response.Message
			}
			if isTransportFailure(msg) {
				return "", nil
			}
			return "", &tracker.FunctionBlockedError{Message: msg, Code: quotaCode(msg)}
		}
		payloadMap, _ := response.Payload.(map[string]interface{})
		if id, ok := payloadMap["allocationId"].(string); ok && id != "" {
			return id, nil
		}
		return "", nil
	}

	allocationID := uuid.New().String()
	payload.AllocationID = &allocationID
	_ = u.socketManager.Send(&socket.UsageFlowSocketMessage{
		Type:    "request_for_allocation",
		Payload: payload,
	})
	return allocationID, nil
}

func (u *UsageFlowAPI) onDiscoveredFunctionEnd(store *tracker.TrackingContext, info tracker.FunctionEndInfo) {
	defer func() { _ = recover() }()
	if info.AllocationID == "" || !u.isConnected() {
		return
	}

	u.mu.RLock()
	policy, hasPolicy := u.lookupFunctionPolicyLocked(store, info.FilePath, info.FuncName)
	u.mu.RUnlock()

	amount := 1.0
	if !info.IsError && hasPolicy && policy.IsResponseTrackingEnabled && policy.ResponseTrackingField != nil {
		if extracted, ok := getValueByPath(info.Result, *policy.ResponseTrackingField); ok {
			if n, ok := toFloat64(extracted); ok {
				amount = n
			}
		}
	}

	_, _, reqMeta := store.GetRequestAllocation()
	usageflowRequestID := ""
	if store != nil {
		usageflowRequestID = store.RequestID()
	}
	metadata := functionMetadataBase(store, reqMeta, info.FuncName, info.FilePath, usageflowRequestID)
	metadata["durationMs"] = info.DurationMs

	// Console ARGUMENTS / RETURN VALUE prefer runtime args + returnValue over schemas.
	if args := jsonSafe(info.Args); args != nil {
		metadata["args"] = args
		metadata["input"] = args
	}
	if info.ParamsSchema != nil {
		metadata["paramsSchema"] = info.ParamsSchema
	}
	if !info.IsError {
		if ret := jsonSafe(info.Result); ret != nil {
			metadata["returnValue"] = ret
			metadata["output"] = ret
		}
		if info.ResultSchema != nil {
			metadata["resultSchema"] = info.ResultSchema
		}
	} else if info.Result != nil {
		metadata["error"] = fmt.Sprint(info.Result)
	}
	if info.Usage != nil {
		metadata["usage"] = info.Usage
		if info.Usage.PromptTokens != nil {
			metadata["promptTokens"] = *info.Usage.PromptTokens
			metadata["prompt_tokens"] = *info.Usage.PromptTokens
		}
		if info.Usage.CompletionTokens != nil {
			metadata["completionTokens"] = *info.Usage.CompletionTokens
			metadata["completion_tokens"] = *info.Usage.CompletionTokens
		}
		if info.Usage.TotalTokens != nil {
			metadata["totalTokens"] = *info.Usage.TotalTokens
			metadata["total_tokens"] = *info.Usage.TotalTokens
		}
	}
	if info.AIModel != "" {
		metadata["aiModel"] = info.AIModel
		metadata["model"] = info.AIModel
	}
	if amount != 1 {
		metadata["amount"] = amount
	}

	_ = u.socketManager.Send(&socket.UsageFlowSocketMessage{
		Type: "use_allocation",
		Payload: &socket.UseAllocationRequest{
			Alias:        "",
			Amount:       amount,
			AllocationID: info.AllocationID,
			Metadata:     metadata,
		},
	})
}

// functionMetadataBase copies only Console-safe correlation fields from the HTTP request.
// Do NOT copy body/headers/objects — Console chips use String(v) and show "[object Object]".
func functionMetadataBase(store *tracker.TrackingContext, reqMeta map[string]interface{}, funcName, filePath, usageflowRequestID string) map[string]interface{} {
	metadata := map[string]interface{}{
		"type":         "FUNCTION_CALL",
		"functionName": funcName,
		"functionPath": filePath,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}
	for _, key := range []string{
		"method", "url", "rawUrl", "clientIP", "identity",
		"usageflowRequestId", "applicationId", "userAgent",
	} {
		if v, ok := reqMeta[key]; ok && isScalarMeta(v) {
			metadata[key] = v
		}
	}
	if store != nil && store.RequestContext != nil {
		if _, ok := metadata["method"]; !ok {
			metadata["method"] = store.RequestContext.Method
		}
		if _, ok := metadata["url"]; !ok {
			metadata["url"] = store.RequestContext.URL
		}
	}
	if usageflowRequestID != "" {
		metadata["usageflowRequestId"] = usageflowRequestID
	}
	return metadata
}

func isScalarMeta(v interface{}) bool {
	switch v.(type) {
	case nil, string, bool, float64, float32, int, int32, int64, uint, uint32, uint64:
		return true
	default:
		return false
	}
}

func jsonSafe(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	var out interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return string(raw)
	}
	return out
}

func (u *UsageFlowAPI) lookupFunctionPolicyLocked(store *tracker.TrackingContext, filePath, funcName string) (config.ApiConfigStrategy, bool) {
	if len(u.functionPolicies) == 0 {
		return config.ApiConfigStrategy{}, false
	}
	method, url := "FUNC", ""
	if store != nil && store.RequestContext != nil {
		method = store.RequestContext.Method
		url = store.RequestContext.URL
	}
	key := fmt.Sprintf("%s %s func:%s:%s", method, url, filePath, funcName)
	if policy, ok := u.functionPolicies[key]; ok {
		return policy, true
	}
	// Fallback: match by function identity only (path:name), any route.
	suffix := fmt.Sprintf("func:%s:%s", filePath, funcName)
	for k, policy := range u.functionPolicies {
		if strings.HasSuffix(k, suffix) {
			return policy, true
		}
	}
	// Console stores FUNCTION identity as name (+ location), not file path.
	nameSuffix := ":" + funcName
	for k, policy := range u.functionPolicies {
		if strings.HasSuffix(k, nameSuffix) {
			return policy, true
		}
	}
	return config.ApiConfigStrategy{}, false
}

func (u *UsageFlowAPI) syncFunctionPolicies(policies []config.ApiConfigStrategy) {
	next := make(map[string]config.ApiConfigStrategy)
	for _, policy := range policies {
		if !strings.EqualFold(policy.Type, "FUNCTION") {
			continue
		}
		if policy.IdentityFieldName == nil || policy.IdentityFieldLocation == nil {
			continue
		}
		key := fmt.Sprintf("%s %s func:%s:%s", policy.Method, policy.Url, *policy.IdentityFieldLocation, *policy.IdentityFieldName)
		next[key] = policy
	}
	u.mu.Lock()
	u.functionPolicies = next
	u.mu.Unlock()
}

func isTransportFailure(msg string) bool {
	m := strings.ToLower(msg)
	for _, token := range []string{"not connected", "timeout", "send failed", "closed", "unavailable"} {
		if strings.Contains(m, token) {
			return true
		}
	}
	return false
}

func quotaCode(msg string) string {
	if strings.Contains(strings.ToLower(msg), "quota") || strings.Contains(strings.ToLower(msg), "rate") {
		return "NOT_ENOUGH_QUOTA"
	}
	return ""
}

func getValueByPath(obj interface{}, path string) (interface{}, bool) {
	if obj == nil || path == "" {
		return nil, false
	}
	// Scalar function returns are advertised as path "return" in NormalizeResultSchema.
	if path == "return" {
		if _, ok := toFloat64(obj); ok {
			return obj, true
		}
		switch obj.(type) {
		case string, bool:
			return obj, true
		}
	}
	cur := obj
	for _, part := range strings.Split(path, ".") {
		switch typed := cur.(type) {
		case map[string]interface{}:
			next, ok := typed[part]
			if !ok {
				return nil, false
			}
			cur = next
		default:
			// Struct / other typed results: JSON round-trip for path walk.
			raw, err := json.Marshal(cur)
			if err != nil {
				return nil, false
			}
			var asMap map[string]interface{}
			if err := json.Unmarshal(raw, &asMap); err != nil {
				return nil, false
			}
			next, ok := asMap[part]
			if !ok {
				return nil, false
			}
			cur = next
		}
	}
	return cur, true
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	default:
		return 0, false
	}
}

// lookupAPIResponseTrackingField returns the response tracking path for an API (non-FUNCTION) policy.
func (u *UsageFlowAPI) lookupAPIResponseTrackingField(method, url string) string {
	u.mu.RLock()
	defer u.mu.RUnlock()
	for _, policy := range u.ApiConfig {
		if strings.EqualFold(policy.Type, "FUNCTION") {
			continue
		}
		if policy.Method != method || policy.Url != url {
			continue
		}
		if policy.IsResponseTrackingEnabled && policy.ResponseTrackingField != nil {
			return *policy.ResponseTrackingField
		}
	}
	return ""
}

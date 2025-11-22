package config

import (
	"encoding/json"
	"testing"
)

func TestApiConfigStrategy_JSONSerialization(t *testing.T) {
	tests := []struct {
		name     string
		config   ApiConfigStrategy
		expected string
	}{
		{
			name: "complete config",
			config: ApiConfigStrategy{
				Url:                   "/api/users",
				Method:                "GET",
				IdentityFieldName:     stringPtr("userId"),
				IdentityFieldLocation: stringPtr("headers"),
			},
			expected: `{"url":"/api/users","method":"GET","identityFieldName":"userId","identityFieldLocation":"headers"}`,
		},
		{
			name: "minimal config",
			config: ApiConfigStrategy{
				Url:    "/api/users",
				Method: "POST",
			},
			expected: `{"url":"/api/users","method":"POST"}`,
		},
		{
			name: "with nil optional fields",
			config: ApiConfigStrategy{
				Url:                   "/api/users",
				Method:                "GET",
				IdentityFieldName:     nil,
				IdentityFieldLocation: nil,
			},
			expected: `{"url":"/api/users","method":"GET"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.config)
			if err != nil {
				t.Fatalf("Failed to marshal: %v", err)
			}

			if string(data) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(data))
			}

			// Test unmarshaling
			var unmarshaled ApiConfigStrategy
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}

			if unmarshaled.Url != tt.config.Url {
				t.Errorf("Url mismatch: expected %s, got %s", tt.config.Url, unmarshaled.Url)
			}
			if unmarshaled.Method != tt.config.Method {
				t.Errorf("Method mismatch: expected %s, got %s", tt.config.Method, unmarshaled.Method)
			}
		})
	}
}

func TestRoute_Validation(t *testing.T) {
	tests := []struct {
		name  string
		route Route
		valid bool
	}{
		{
			name:  "valid route",
			route: Route{Method: "GET", URL: "/api/users"},
			valid: true,
		},
		{
			name:  "wildcard method",
			route: Route{Method: "*", URL: "/api/users"},
			valid: true,
		},
		{
			name:  "wildcard URL",
			route: Route{Method: "GET", URL: "*"},
			valid: true,
		},
		{
			name:  "both wildcards",
			route: Route{Method: "*", URL: "*"},
			valid: true,
		},
		{
			name:  "empty method",
			route: Route{Method: "", URL: "/api/users"},
			valid: false,
		},
		{
			name:  "empty URL",
			route: Route{Method: "GET", URL: ""},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := tt.route.Method != "" && tt.route.URL != ""
			if valid != tt.valid {
				t.Errorf("Expected valid=%v, got %v", tt.valid, valid)
			}
		})
	}
}

func TestPolicyListResponse_JSONSerialization(t *testing.T) {
	response := PolicyListResponse{
		Policies: []ApiConfigStrategy{
			{
				Url:    "/api/users",
				Method: "GET",
			},
			{
				Url:    "/api/orders",
				Method: "POST",
			},
		},
		Total: 2,
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var unmarshaled PolicyListResponse
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if unmarshaled.Total != response.Total {
		t.Errorf("Total mismatch: expected %d, got %d", response.Total, unmarshaled.Total)
	}

	if len(unmarshaled.Policies) != len(response.Policies) {
		t.Errorf("Policies length mismatch: expected %d, got %d", len(response.Policies), len(unmarshaled.Policies))
	}
}

func TestApplicationEndpointPolicy_JSONSerialization(t *testing.T) {
	policy := ApplicationEndpointPolicy{
		PolicyId:          "policy-123",
		AccountId:         "account-456",
		ApplicationId:     "app-789",
		EndpointPattern:   "/api/users/:id",
		EndpointMethod:    "GET",
		IdentityField:     "userId",
		IdentityLocation:  "headers",
		RateLimit:         100,
		RateLimitInterval: "1h",
		CreatedAt:         1234567890,
		UpdatedAt:         1234567890,
	}

	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var unmarshaled ApplicationEndpointPolicy
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if unmarshaled.PolicyId != policy.PolicyId {
		t.Errorf("PolicyId mismatch: expected %s, got %s", policy.PolicyId, unmarshaled.PolicyId)
	}
}

func TestVerifyResponse_JSONSerialization(t *testing.T) {
	response := VerifyResponse{
		AccountId:     "account-123",
		ApplicationId: "app-456",
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var unmarshaled VerifyResponse
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if unmarshaled.AccountId != response.AccountId {
		t.Errorf("AccountId mismatch: expected %s, got %s", response.AccountId, unmarshaled.AccountId)
	}
}

// Helper function
func stringPtr(s string) *string {
	return &s
}

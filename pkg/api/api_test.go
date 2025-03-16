package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/usageflow/usageflow-go-middleware/pkg/config"
)

func TestFetchApiConfig(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if r.Header.Get("x-usage-key") != "test-api-key" {
			t.Error("API key header missing or incorrect")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("Content-Type header not set to application/json")
		}

		// Send response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(config.ApiConfigStrategy{
			ID:                    "test-id",
			Name:                  "test-strategy",
			AccountId:             "acc-123",
			IdentityFieldName:     "user_id",
			IdentityFieldLocation: "header",
			ConfigData: map[string]interface{}{
				"key1": "value1",
				"key2": 123,
			},
		})
	}))
	defer server.Close()

	// Override base URL for testing
	originalBaseURL := BaseURL
	BaseURL = server.URL
	defer func() { BaseURL = originalBaseURL }()

	// Test cases
	tests := []struct {
		name    string
		apiKey  string
		wantErr bool
	}{
		{
			name:    "valid API key",
			apiKey:  "test-api-key",
			wantErr: false,
		},
		{
			name:    "empty API key",
			apiKey:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := FetchApiConfig(tt.apiKey)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Verify config fields
			if config.ID != "test-id" {
				t.Errorf("Expected ID %q but got %q", "test-id", config.ID)
			}
			if config.Name != "test-strategy" {
				t.Errorf("Expected Name %q but got %q", "test-strategy", config.Name)
			}
			if config.AccountId != "acc-123" {
				t.Errorf("Expected AccountId %q but got %q", "acc-123", config.AccountId)
			}
		})
	}
}

func TestExecuteRequest(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if r.Header.Get("x-usage-key") != "test-api-key" {
			t.Error("API key header missing or incorrect")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("Content-Type header not set to application/json")
		}

		// Read and verify request body
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
			return
		}

		// Verify required fields
		if reqBody["ledgerId"] == nil {
			t.Error("ledgerId missing from request body")
		}
		if reqBody["method"] == nil {
			t.Error("method missing from request body")
		}
		if reqBody["url"] == nil {
			t.Error("url missing from request body")
		}
		if reqBody["metadata"] == nil {
			t.Error("metadata missing from request body")
		}

		// Send response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})
	}))
	defer server.Close()

	// Override base URL for testing
	originalBaseURL := BaseURL
	BaseURL = server.URL
	defer func() { BaseURL = originalBaseURL }()

	// Test cases
	tests := []struct {
		name     string
		apiKey   string
		ledgerId string
		method   string
		url      string
		metadata map[string]interface{}
		wantErr  bool
	}{
		{
			name:     "valid request",
			apiKey:   "test-api-key",
			ledgerId: "test-ledger",
			method:   "GET",
			url:      "/api/test",
			metadata: map[string]interface{}{
				"key": "value",
			},
			wantErr: false,
		},
		{
			name:     "empty API key",
			apiKey:   "",
			ledgerId: "test-ledger",
			method:   "GET",
			url:      "/api/test",
			metadata: map[string]interface{}{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ExecuteRequest(tt.apiKey, tt.ledgerId, tt.method, tt.url, tt.metadata)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestExecuteFulfillRequest(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if r.Header.Get("x-usage-key") != "test-api-key" {
			t.Error("API key header missing or incorrect")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("Content-Type header not set to application/json")
		}

		// Read and verify request body
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
			return
		}

		// Verify required fields
		if reqBody["ledgerId"] == nil {
			t.Error("ledgerId missing from request body")
		}
		if reqBody["method"] == nil {
			t.Error("method missing from request body")
		}
		if reqBody["url"] == nil {
			t.Error("url missing from request body")
		}
		if reqBody["metadata"] == nil {
			t.Error("metadata missing from request body")
		}

		// Send response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})
	}))
	defer server.Close()

	// Override base URL for testing
	originalBaseURL := BaseURL
	BaseURL = server.URL
	defer func() { BaseURL = originalBaseURL }()

	// Test cases
	tests := []struct {
		name     string
		apiKey   string
		ledgerId string
		method   string
		url      string
		metadata map[string]interface{}
		wantErr  bool
	}{
		{
			name:     "valid request",
			apiKey:   "test-api-key",
			ledgerId: "test-ledger",
			method:   "GET",
			url:      "/api/test",
			metadata: map[string]interface{}{
				"statusCode": 200,
				"response":   "success",
			},
			wantErr: false,
		},
		{
			name:     "empty API key",
			apiKey:   "",
			ledgerId: "test-ledger",
			method:   "GET",
			url:      "/api/test",
			metadata: map[string]interface{}{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ExecuteFulfillRequest(tt.apiKey, tt.ledgerId, tt.method, tt.url, tt.metadata)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

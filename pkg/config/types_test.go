package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestApiConfigStrategyJSON(t *testing.T) {
	now := time.Now().Unix()
	deletedAt := now + 3600 // 1 hour later

	tests := []struct {
		name     string
		config   ApiConfigStrategy
		wantJSON string
	}{
		{
			name: "full config",
			config: ApiConfigStrategy{
				ID:                    "test-id",
				Name:                  "test-strategy",
				AccountId:             "acc-123",
				IdentityFieldName:     "user_id",
				IdentityFieldLocation: "header",
				ConfigData: map[string]interface{}{
					"key1": "value1",
					"key2": 123,
				},
				CreatedAt: now,
				UpdatedAt: now,
				DeletedAt: &deletedAt,
			},
			wantJSON: `{"_id":"test-id","name":"test-strategy","accountId":"acc-123","identityFieldName":"user_id","identityFieldLocation":"header","configData":{"key1":"value1","key2":123},"createdAt":TIMESTAMP,"updatedAt":TIMESTAMP,"deletedAt":DELETED_TIMESTAMP}`,
		},
		{
			name: "minimal config",
			config: ApiConfigStrategy{
				ID:        "test-id",
				Name:      "test-strategy",
				AccountId: "acc-123",
			},
			wantJSON: `{"_id":"test-id","name":"test-strategy","accountId":"acc-123","identityFieldName":"","identityFieldLocation":"","configData":null,"createdAt":0,"updatedAt":0,"deletedAt":null}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.config)
			if err != nil {
				t.Errorf("Failed to marshal config: %v", err)
				return
			}

			// Replace timestamps in expected JSON for comparison
			expectedJSON := tt.wantJSON
			if tt.config.CreatedAt > 0 {
				expectedJSON = strings.Replace(expectedJSON, "TIMESTAMP", fmt.Sprintf("%d", tt.config.CreatedAt), -1)
			}
			if tt.config.DeletedAt != nil {
				expectedJSON = strings.Replace(expectedJSON, "DELETED_TIMESTAMP", fmt.Sprintf("%d", *tt.config.DeletedAt), -1)
			}

			if string(got) != expectedJSON {
				t.Errorf("JSON mismatch\nwant: %s\ngot:  %s", expectedJSON, string(got))
			}
		})
	}
}

func TestRouteValidation(t *testing.T) {
	tests := []struct {
		name    string
		route   Route
		wantErr bool
	}{
		{
			name: "valid route",
			route: Route{
				Method: "GET",
				URL:    "/api/test",
			},
			wantErr: false,
		},
		{
			name: "valid route with wildcard method",
			route: Route{
				Method: "*",
				URL:    "/api/test",
			},
			wantErr: false,
		},
		{
			name: "valid route with wildcard URL",
			route: Route{
				Method: "POST",
				URL:    "/api/*",
			},
			wantErr: false,
		},
		{
			name: "empty method",
			route: Route{
				Method: "",
				URL:    "/api/test",
			},
			wantErr: true,
		},
		{
			name: "empty URL",
			route: Route{
				Method: "GET",
				URL:    "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.route.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Route.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

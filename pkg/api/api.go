package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/usageflow/usageflow-go-middleware/pkg/config"
)

var (
	// BaseURL is the base URL for the UsageFlow API
	BaseURL = "https://api.usageflow.io/api/v1"
)

// FetchApiConfig retrieves the API configuration from the UsageFlow service
func FetchApiConfig(apiKey string) (*config.ApiConfigStrategy, error) {
	req, err := http.NewRequest("GET", BaseURL+"/strategies/application", nil)
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

	var verifyResp config.ApiConfigStrategy
	if err := json.NewDecoder(resp.Body).Decode(&verifyResp); err != nil {
		return nil, err
	}

	return &verifyResp, nil
}

// ExecuteRequest sends a request to the UsageFlow API
func ExecuteRequest(apiKey, ledgerId, method, url string, metadata map[string]interface{}) error {
	requestBody := map[string]interface{}{
		"ledgerId": ledgerId,
		"method":   method,
		"url":      url,
		"metadata": metadata,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %v", err)
	}

	req, err := http.NewRequest("POST", BaseURL+"/ledgers/measure/allocate", bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}

	req.Header.Set("x-usage-key", apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("failed to execute request: %s", string(body))
	}

	return nil
}

// ExecuteFulfillRequest sends a fulfill request to the UsageFlow API
func ExecuteFulfillRequest(apiKey, ledgerId, method, url string, metadata map[string]interface{}) error {
	requestBody := map[string]interface{}{
		"ledgerId": ledgerId,
		"method":   method,
		"url":      url,
		"metadata": metadata,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %v", err)
	}

	req, err := http.NewRequest("POST", BaseURL+"/ledgers/measure/allocate/use", bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}

	req.Header.Set("x-usage-key", apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("failed to execute fulfill request: %s", string(body))
	}

	return nil
}

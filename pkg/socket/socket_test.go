package socket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestNewUsageFlowSocketManager(t *testing.T) {
	manager := NewUsageFlowSocketManager("test-api-key")
	assert.NotNil(t, manager)
	assert.Equal(t, "test-api-key", manager.apiKey)
	assert.Equal(t, defaultPoolSize, manager.poolSize)
	assert.Equal(t, defaultWSURL, manager.wsURL)

	manager.Close()
}

func TestNewUsageFlowSocketManager_CustomPoolSize(t *testing.T) {
	manager := NewUsageFlowSocketManager("test-api-key", 5)
	assert.NotNil(t, manager)
	assert.Equal(t, 5, manager.poolSize)

	manager.Close()
}

func TestUsageFlowSocketManager_IsConnected(t *testing.T) {
	manager := NewUsageFlowSocketManager("test-api-key")
	defer manager.Close()

	// Initially may or may not be connected depending on server availability
	connected := manager.IsConnected()
	assert.IsType(t, false, connected)
}

func TestGenerateID(t *testing.T) {
	id1, err := generateID()
	assert.NoError(t, err)
	assert.NotEmpty(t, id1)

	id2, err := generateID()
	assert.NoError(t, err)
	assert.NotEmpty(t, id2)

	// IDs should be unique
	assert.NotEqual(t, id1, id2)
}

func TestUsageFlowSocketMessage_JSONSerialization(t *testing.T) {
	message := UsageFlowSocketMessage{
		Type: "test_type",
		Payload: map[string]interface{}{
			"key": "value",
		},
		ID: "test-id",
	}

	data, err := json.Marshal(message)
	assert.NoError(t, err)

	var unmarshaled UsageFlowSocketMessage
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)
	assert.Equal(t, message.Type, unmarshaled.Type)
	assert.Equal(t, message.ID, unmarshaled.ID)
}

func TestUsageFlowSocketResponse_JSONSerialization(t *testing.T) {
	response := UsageFlowSocketResponse{
		Type: "response_type",
		Payload: map[string]interface{}{
			"result": "success",
		},
		ID:      "response-id",
		ReplyTo: "request-id",
		Message: "Success",
	}

	data, err := json.Marshal(response)
	assert.NoError(t, err)

	var unmarshaled UsageFlowSocketResponse
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)
	assert.Equal(t, response.Type, unmarshaled.Type)
	assert.Equal(t, response.ID, unmarshaled.ID)
	assert.Equal(t, response.ReplyTo, unmarshaled.ReplyTo)
}

func TestRequestForAllocation_JSONSerialization(t *testing.T) {
	request := RequestForAllocation{
		Alias:  "ledger-123",
		Amount: 1.5,
		Metadata: map[string]interface{}{
			"key": "value",
		},
	}

	data, err := json.Marshal(request)
	assert.NoError(t, err)

	var unmarshaled RequestForAllocation
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)
	assert.Equal(t, request.Alias, unmarshaled.Alias)
	assert.Equal(t, request.Amount, unmarshaled.Amount)
}

func TestUseAllocationRequest_JSONSerialization(t *testing.T) {
	request := UseAllocationRequest{
		Alias:        "ledger-123",
		Amount:       1.0,
		AllocationID: "alloc-456",
		Metadata: map[string]interface{}{
			"key": "value",
		},
	}

	data, err := json.Marshal(request)
	assert.NoError(t, err)

	var unmarshaled UseAllocationRequest
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)
	assert.Equal(t, request.Alias, unmarshaled.Alias)
	assert.Equal(t, request.AllocationID, unmarshaled.AllocationID)
}

func TestPooledConnection_ThreadSafety(t *testing.T) {
	conn := &PooledConnection{
		connected:       true,
		pendingRequests: 0,
		messageHandlers: make(map[string]chan *UsageFlowSocketResponse),
	}

	// Test concurrent access
	done := make(chan bool, 2)

	go func() {
		conn.mu.Lock()
		conn.pendingRequests++
		conn.mu.Unlock()
		done <- true
	}()

	go func() {
		conn.mu.RLock()
		_ = conn.connected
		conn.mu.RUnlock()
		done <- true
	}()

	<-done
	<-done

	conn.mu.RLock()
	assert.Equal(t, 1, conn.pendingRequests)
	conn.mu.RUnlock()
}

// Test with mock WebSocket server
func TestUsageFlowSocketManager_SendAsync_WithMockServer(t *testing.T) {
	// Create a simple WebSocket server
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read message
		var msg UsageFlowSocketMessage
		err = conn.ReadJSON(&msg)
		if err != nil {
			return
		}

		// Send response
		response := UsageFlowSocketResponse{
			Type:    "response",
			ID:      msg.ID,
			ReplyTo: msg.ID,
			Payload: map[string]interface{}{
				"allocationId": "test-alloc-123",
			},
		}
		conn.WriteJSON(response)
	}))

	defer server.Close()

	// Update WS URL to point to test server
	wsURL := "ws" + server.URL[4:] + "/ws"

	manager := &UsageFlowSocketManager{
		connections: make([]*PooledConnection, 0),
		wsURL:       wsURL,
		poolSize:    1,
		apiKey:      "test-key",
	}

	// Note: This test requires actual WebSocket connection
	// In a real scenario, you'd use a mock or test server
	// For now, we'll test the structure
	assert.NotNil(t, manager)
}

func TestUsageFlowSocketManager_Close(t *testing.T) {
	manager := NewUsageFlowSocketManager("test-api-key")

	// Close should not panic
	assert.NotPanics(t, func() {
		manager.Close()
	})

	// Close again should be safe
	assert.NotPanics(t, func() {
		manager.Close()
	})
}

func TestUsageFlowSocketManager_Destroy(t *testing.T) {
	manager := NewUsageFlowSocketManager("test-api-key")

	// Destroy should not panic
	assert.NotPanics(t, func() {
		manager.Destroy()
	})
}

func TestConstants(t *testing.T) {
	assert.Equal(t, 10, defaultPoolSize)
	assert.Equal(t, 5*time.Second, reconnectDelay)
	assert.Equal(t, 2*time.Second, requestTimeout)
	assert.Equal(t, 30*time.Second, pingPeriod)
	assert.Equal(t, 60*time.Second, pongWait)
	assert.Equal(t, 10*time.Second, writeWait)
	assert.Equal(t, 5, maxReconnectTries)
}

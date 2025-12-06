package socket

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultWSURL      = "wss://api.usageflow.io/ws"
	defaultPoolSize   = 10
	reconnectDelay    = 5 * time.Second
	requestTimeout    = 2 * time.Second
	pingPeriod        = 30 * time.Second
	pongWait          = 60 * time.Second
	writeWait         = 10 * time.Second
	maxReconnectTries = 5
)

// PooledConnection represents a single WebSocket connection in the pool
type PooledConnection struct {
	ws              *websocket.Conn
	connected       bool
	pendingRequests int
	index           int
	mu              sync.RWMutex
	messageHandlers map[string]chan *UsageFlowSocketResponse
}

// UsageFlowSocketManager manages a pool of WebSocket connections to UsageFlow
type UsageFlowSocketManager struct {
	connections     []*PooledConnection
	wsURL           string
	poolSize        int
	currentIndex    int
	connecting      bool
	connectionMutex sync.Mutex
	apiKey          string
	mu              sync.RWMutex
}

// NewUsageFlowSocketManager creates a new WebSocket manager instance
func NewUsageFlowSocketManager(apiKey string, poolSize ...int) *UsageFlowSocketManager {
	size := defaultPoolSize
	if len(poolSize) > 0 && poolSize[0] > 0 {
		size = poolSize[0]
	}

	socket := &UsageFlowSocketManager{
		connections: make([]*PooledConnection, 0),
		wsURL:       defaultWSURL,
		poolSize:    size,
		apiKey:      apiKey,
	}

	socket.Connect()
	return socket
}

// Connect establishes all WebSocket connections in the pool
func (m *UsageFlowSocketManager) Connect() error {
	if m.apiKey == "" {
		return errors.New("API key not available")
	}

	m.connectionMutex.Lock()
	defer m.connectionMutex.Unlock()

	if m.connecting {
		// Wait for existing connection attempts
		return nil
	}

	if len(m.connections) > 0 && m.IsConnected() {
		// Already connected
		return nil
	}

	m.connecting = true
	defer func() {
		m.connecting = false
	}()

	// Create all connections in parallel
	type connResult struct {
		conn  *PooledConnection
		index int
		err   error
	}

	results := make(chan connResult, m.poolSize)

	for i := 0; i < m.poolSize; i++ {
		go func(index int) {
			conn, err := m.createConnection(index)
			results <- connResult{conn: conn, index: index, err: err}
		}(i)
	}

	// Collect results
	successful := make([]*PooledConnection, 0, m.poolSize)
	failed := make([]int, 0)

	for i := 0; i < m.poolSize; i++ {
		result := <-results
		if result.err != nil {
			failed = append(failed, result.index)
		} else {
			successful = append(successful, result.conn)
		}
	}

	// Sort by index
	for i := 0; i < len(successful)-1; i++ {
		for j := i + 1; j < len(successful); j++ {
			if successful[i].index > successful[j].index {
				successful[i], successful[j] = successful[j], successful[i]
			}
		}
	}

	m.mu.Lock()
	m.connections = successful
	m.mu.Unlock()

	// Retry failed connections in background
	for _, index := range failed {
		go func(idx int) {
			time.Sleep(reconnectDelay)
			m.reconnectConnectionWithRetry(idx, 0)
		}(index)
	}

	return nil
}

// createConnection creates a single WebSocket connection
func (m *UsageFlowSocketManager) createConnection(index int) (*PooledConnection, error) {
	if m.apiKey == "" {
		return nil, errors.New("API key not available")
	}

	headers := make(map[string][]string)
	headers["x-usage-key"] = []string{m.apiKey}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(m.wsURL, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to dial WebSocket: %w", err)
	}

	pooledConn := &PooledConnection{
		ws:              conn,
		connected:       true,
		pendingRequests: 0,
		index:           index,
		messageHandlers: make(map[string]chan *UsageFlowSocketResponse),
	}

	// Set pong handler to extend read deadline on pong
	conn.SetPongHandler(func(string) error {
		// Extend read deadline when pong is received
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Set initial read deadline
	conn.SetReadDeadline(time.Now().Add(pongWait))

	// Start message handler goroutine
	go m.handleMessages(pooledConn)

	// Start ping goroutine
	go m.pingConnection(pooledConn)

	// Set up close handler
	conn.SetCloseHandler(func(code int, text string) error {
		pooledConn.mu.Lock()
		pooledConn.connected = false
		pooledConn.mu.Unlock()

		// Attempt to reconnect after a delay
		go func() {
			time.Sleep(reconnectDelay)
			if m.apiKey != "" {
				m.reconnectConnectionWithRetry(index, 0)
			}
		}()

		return nil
	})

	return pooledConn, nil
}

// handleMessages processes incoming messages for a connection
func (m *UsageFlowSocketManager) handleMessages(conn *PooledConnection) {
	defer func() {
		conn.mu.Lock()
		conn.connected = false
		// Clear all pending handlers on disconnect
		for id := range conn.messageHandlers {
			delete(conn.messageHandlers, id)
		}
		conn.mu.Unlock()

		// Trigger reconnection when read fails (server restart, network issue, etc.)
		go func() {
			time.Sleep(reconnectDelay)
			if m.apiKey != "" {
				m.reconnectConnectionWithRetry(conn.index, 0)
			}
		}()
	}()

	for {
		conn.mu.RLock()
		if !conn.connected || conn.ws == nil {
			conn.mu.RUnlock()
			return
		}
		ws := conn.ws
		conn.mu.RUnlock()

		// Read message
		_, message, err := ws.ReadMessage()
		if err != nil {
			// Check if it's a timeout - this means pong wasn't received
			if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				// Read deadline expired - connection is likely dead
				return
			}
			// Check if it's a close error
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				// Connection lost, will trigger reconnection in defer
				return
			}
			// For other errors, also trigger reconnection
			return
		}

		// Extend read deadline after successful read (connection is alive)
		ws.SetReadDeadline(time.Now().Add(pongWait))

		var response UsageFlowSocketResponse
		if err := json.Unmarshal(message, &response); err != nil {
			continue
		}

		// Find matching handler
		conn.mu.Lock()
		var handler chan *UsageFlowSocketResponse
		var handlerID string

		// Check by ID or ReplyTo
		if response.ID != "" {
			handler = conn.messageHandlers[response.ID]
			handlerID = response.ID
		} else if response.ReplyTo != "" {
			handler = conn.messageHandlers[response.ReplyTo]
			handlerID = response.ReplyTo
		}

		if handler != nil {
			delete(conn.messageHandlers, handlerID)
		}
		conn.mu.Unlock()

		if handler != nil {
			select {
			case handler <- &response:
			default:
			}
		}
	}
}

// reconnectConnection attempts to reconnect a specific connection
func (m *UsageFlowSocketManager) reconnectConnection(index int) {
	m.reconnectConnectionWithRetry(index, 0)
}

// reconnectConnectionWithRetry attempts to reconnect with exponential backoff
func (m *UsageFlowSocketManager) reconnectConnectionWithRetry(index int, attempt int) {
	if attempt >= maxReconnectTries {
		// Max retries reached, give up for now
		// Will retry on next connection attempt
		return
	}

	m.connectionMutex.Lock()
	defer m.connectionMutex.Unlock()

	m.mu.RLock()
	var existingConn *PooledConnection
	for _, conn := range m.connections {
		if conn.index == index {
			existingConn = conn
			break
		}
	}
	m.mu.RUnlock()

	if existingConn != nil {
		existingConn.mu.Lock()
		if existingConn.ws != nil {
			existingConn.ws.Close()
		}
		existingConn.connected = false
		// Clear all pending handlers
		for id := range existingConn.messageHandlers {
			delete(existingConn.messageHandlers, id)
		}
		existingConn.mu.Unlock()
	}

	// Create new connection
	newConn, err := m.createConnection(index)
	if err != nil {
		// Retry with exponential backoff
		backoff := reconnectDelay * time.Duration(1<<uint(attempt))
		if backoff > 60*time.Second {
			backoff = 60 * time.Second
		}
		go func() {
			time.Sleep(backoff)
			if m.apiKey != "" {
				m.reconnectConnectionWithRetry(index, attempt+1)
			}
		}()
		return
	}

	m.mu.Lock()
	// Replace or add the connection
	found := false
	for i, conn := range m.connections {
		if conn.index == index {
			m.connections[i] = newConn
			found = true
			break
		}
	}
	if !found {
		m.connections = append(m.connections, newConn)
	}
	m.mu.Unlock()
}

// pingConnection sends periodic ping messages to keep the connection alive
func (m *UsageFlowSocketManager) pingConnection(conn *PooledConnection) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for range ticker.C {
		conn.mu.Lock()
		if !conn.connected || conn.ws == nil {
			conn.mu.Unlock()
			return
		}
		ws := conn.ws
		conn.mu.Unlock()

		// Send ping with write deadline
		ws.SetWriteDeadline(time.Now().Add(writeWait))
		if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
			// Ping failed, connection is dead
			conn.mu.Lock()
			conn.connected = false
			conn.mu.Unlock()
			return
		}
	}
}

// getConnection returns the least-busy connected connection
func (m *UsageFlowSocketManager) getConnection() *PooledConnection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.connections) == 0 {
		return nil
	}

	// Filter to only connected connections
	connected := make([]*PooledConnection, 0)
	for _, conn := range m.connections {
		conn.mu.Lock()
		if conn.connected {
			connected = append(connected, conn)
		}
		conn.mu.Unlock()
	}

	if len(connected) == 0 {
		return nil
	}

	// Use least-busy connection strategy
	selected := connected[0]
	for _, conn := range connected {
		conn.mu.Lock()
		if conn.pendingRequests < selected.pendingRequests {
			selected = conn
		}
		conn.mu.Unlock()
	}

	// If all connections have the same load, use round-robin for better distribution
	sameLoad := true
	for _, conn := range connected {
		conn.mu.Lock()
		if conn.pendingRequests != selected.pendingRequests {
			sameLoad = false
		}
		conn.mu.Unlock()
		if !sameLoad {
			break
		}
	}

	if sameLoad && len(connected) > 1 {
		m.currentIndex = (m.currentIndex + 1) % len(connected)
		selected = connected[m.currentIndex]
	}

	return selected
}

// SendAsync sends a message and waits for a response
func (m *UsageFlowSocketManager) SendAsync(payload *UsageFlowSocketMessage) (*UsageFlowSocketResponse, error) {
	conn := m.getConnection()
	if conn == nil {
		return nil, errors.New("WebSocket not connected")
	}

	return m.asyncSend(payload, conn)
}

// Send sends a message without waiting for a response
func (m *UsageFlowSocketManager) Send(payload *UsageFlowSocketMessage) error {
	conn := m.getConnection()
	if conn == nil {
		return errors.New("WebSocket not connected")
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()

	if !conn.connected || conn.ws == nil {
		return errors.New("WebSocket not connected")
	}

	messageBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	return conn.ws.WriteMessage(websocket.TextMessage, messageBytes)
}

// asyncSend sends a message and waits for a response with timeout
func (m *UsageFlowSocketManager) asyncSend(payload *UsageFlowSocketMessage, conn *PooledConnection) (*UsageFlowSocketResponse, error) {
	conn.mu.Lock()
	if !conn.connected || conn.ws == nil {
		conn.mu.Unlock()
		return nil, errors.New("WebSocket not connected")
	}
	conn.mu.Unlock()

	// Generate unique ID
	id, err := generateID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate ID: %w", err)
	}

	// Create message with ID
	message := &UsageFlowSocketMessage{
		Type:    payload.Type,
		Payload: payload.Payload,
		ID:      id,
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	// Create response channel
	responseChan := make(chan *UsageFlowSocketResponse, 1)

	conn.mu.Lock()
	conn.pendingRequests++
	conn.messageHandlers[id] = responseChan
	conn.mu.Unlock()

	// Cleanup function
	cleanup := func() {
		conn.mu.Lock()
		conn.pendingRequests--
		delete(conn.messageHandlers, id)
		conn.mu.Unlock()
	}

	// Send message
	conn.mu.Lock()
	err = conn.ws.WriteMessage(websocket.TextMessage, messageBytes)
	conn.mu.Unlock()

	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	// Wait for response with timeout
	select {
	case response := <-responseChan:
		cleanup()
		return response, nil
	case <-time.After(requestTimeout):
		cleanup()

		return nil, errors.New("WebSocket request timeout")
	}
}

// generateID generates a unique ID for message correlation
func generateID() (string, error) {
	// Generate random bytes
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	// Encode to base64
	randomPart := base64.URLEncoding.EncodeToString(randomBytes)

	// Add timestamp
	timestamp := time.Now().UnixNano()

	// Generate random number for additional uniqueness
	bigInt, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s_%d_%d", randomPart, timestamp, bigInt.Int64()), nil
}

// IsConnected returns whether at least one connection is active
func (m *UsageFlowSocketManager) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, conn := range m.connections {
		conn.mu.Lock()
		if conn.connected {
			conn.mu.Unlock()
			return true
		}
		conn.mu.Unlock()
	}
	return false
}

// Close closes all WebSocket connections
func (m *UsageFlowSocketManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, conn := range m.connections {
		conn.mu.Lock()
		if conn.ws != nil {
			conn.ws.Close()
		}
		conn.connected = false
		conn.mu.Unlock()
	}
	m.connections = make([]*PooledConnection, 0)
}

// Destroy cleans up all resources
func (m *UsageFlowSocketManager) Destroy() {
	m.Close()
}

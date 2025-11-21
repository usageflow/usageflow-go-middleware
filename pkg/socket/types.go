package socket

// UsageFlowSocketMessage represents a message sent to UsageFlow via WebSocket
type UsageFlowSocketMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
	ID      string      `json:"id,omitempty"`
}

// UsageFlowSocketResponse represents a response from UsageFlow via WebSocket
type UsageFlowSocketResponse struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
	ID      string      `json:"id,omitempty"`
	ReplyTo string      `json:"replyTo,omitempty"`
	Message string      `json:"message,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// RequestForAllocation represents the payload for allocation requests
type RequestForAllocation struct {
	Alias    string                 `json:"alias"`
	Amount   float64                `json:"amount"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// UseAllocationRequest represents the payload for using an allocation
type UseAllocationRequest struct {
	Alias        string                 `json:"alias"`
	Amount       float64                `json:"amount"`
	AllocationID string                 `json:"allocationId"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// PolicyResponse represents the response for get_application_policies
type PolicyResponse struct {
	Policies []Policy `json:"policies"`
	Total    int      `json:"total"`
}

// Policy represents an application policy
type Policy struct {
	PolicyID           string `json:"policyId"`
	AccountID          string `json:"accountId"`
	ApplicationID      string `json:"applicationId"`
	EndpointPattern    string `json:"endpointPattern"`
	EndpointMethod     string `json:"endpointMethod"`
	IdentityField      string `json:"identityField"`
	IdentityLocation   string `json:"identityLocation"`
	RateLimit          int    `json:"rateLimit"`
	RateLimitInterval  string `json:"rateLimitInterval"`
	MeteringExpression string `json:"meteringExpression"`
	MeteringTrigger    string `json:"meteringTrigger"`
	StripePriceID      string `json:"stripePriceId"`
	StripeCustomerID   string `json:"stripeCustomerId"`
	CreatedAt          int64  `json:"createdAt"`
	UpdatedAt          int64  `json:"updatedAt"`
}

// AllocationResponse represents the response for request_for_allocation
type AllocationResponse struct {
	AllocationID string `json:"allocationId"`
}

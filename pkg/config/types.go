package config

type PolicyListResponse struct {
	Policies []ApiConfigStrategy `json:"policies"`
	Total    int                 `json:"total"`
}

type ApplicationConfigResponse struct {
	MonitorPaths       []interface{} `json:"monitoringPaths"`
	WhitelistEndpoints []interface{} `json:"whitelistEndpoints"`
	// DiscoveryDisabled, when true, disables function call-chain tracking (Track/Wrap).
	// Mirrors JS/Python discoveryDisabled from get_application_config.
	DiscoveryDisabled *bool `json:"discoveryDisabled,omitempty"`
	// ReportAllFunctionAllocations controls per-function request_for_allocation / use_allocation.
	ReportAllFunctionAllocations *bool `json:"reportAllFunctionAllocations,omitempty"`
}

// ApiConfigStrategy represents the configuration strategy for the API
// Matches UsageFlowConfig interface: url, method, identityFieldName?, identityFieldLocation?
type ApiConfigStrategy struct {
	Url                       string  `bson:"url" json:"url"`
	Method                    string  `bson:"method" json:"method"`
	Type                      string  `bson:"type,omitempty" json:"type,omitempty"` // API | FUNCTION
	IdentityFieldName         *string `bson:"identityFieldName,omitempty" json:"identityFieldName,omitempty"`
	IdentityFieldLocation     *string `bson:"identityFieldLocation,omitempty" json:"identityFieldLocation,omitempty"`
	HasRateLimit              bool    `bson:"hasRateLimit,omitempty" json:"hasRateLimit,omitempty"`
	ResponseTrackingField     *string `bson:"responseTrackingField,omitempty" json:"responseTrackingField,omitempty"`
	IsResponseTrackingEnabled bool    `bson:"isResponseTrackingEnabled,omitempty" json:"isResponseTrackingEnabled,omitempty"`
}

type BlockedEndpointsResponse struct {
	Endpoints []BlockedEndpoints `bson:"endpoints" json:"endpoints"`
	Total     int                `bson:"total" json:"total"`
}

type BlockedEndpoints struct {
	Url      string `bson:"url" json:"url"`
	Method   string `bson:"method" json:"method"`
	Identity string `bson:"identity" json:"identity"`
}

type ApplicationEndpointPolicy struct {
	PolicyId           string `bson:"policyId" json:"policyId"`
	AccountId          string `bson:"accountId" json:"accountId"`
	ApplicationId      string `bson:"applicationId" json:"applicationId"`
	EndpointPattern    string `bson:"endpointPattern" json:"endpointPattern"`
	EndpointMethod     string `bson:"endpointMethod" json:"endpointMethod"`
	IdentityField      string `bson:"identityField" json:"identityField"`
	IdentityLocation   string `bson:"identityLocation" json:"identityLocation"`
	RateLimit          int    `bson:"rateLimit" json:"rateLimit"`
	RateLimitInterval  string `bson:"rateLimitInterval" json:"rateLimitInterval"`
	MeteringExpression string `bson:"meteringExpression" json:"meteringExpression"`
	MeteringTrigger    string `bson:"meteringTrigger" json:"meteringTrigger"`
	StripePriceId      string `bson:"stripePriceId" json:"stripePriceId"`
	StripeCustomerId   string `bson:"stripeCustomerId" json:"stripeCustomerId"`
	CreatedAt          int64  `bson:"createdAt" json:"createdAt"`
	UpdatedAt          int64  `bson:"updatedAt" json:"updatedAt"`
}

type PolicyResponse struct {
	Data PolicyListResponse `json:"data"`
}

// Route defines an individual route configuration (JS/Python camelCase wire shape).
type Route struct {
	Method string `json:"method"`
	URL    string `json:"url"`
}

// VerifyResponse represents the response from the verification endpoint
type VerifyResponse struct {
	AccountId     string `json:"accountId"`
	ApplicationId string `json:"applicationId"`
}

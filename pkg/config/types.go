package config

// ApiConfigStrategy represents the configuration strategy for the API
type ApiConfigStrategy struct {
	ID                    string                 `bson:"_id" json:"_id"`
	Name                  string                 `bson:"name" json:"name"`
	AccountId             string                 `bson:"accountId" json:"accountId"`
	ApplicationId         string                 `bson:"applicationId" json:"applicationId"`
	IdentityFieldName     string                 `bson:"identityFieldName" json:"identityFieldName"`
	IdentityFieldLocation string                 `bson:"identityFieldLocation" json:"identityFieldLocation"`
	ConfigData            map[string]interface{} `bson:"configData" json:"configData"`
	CreatedAt             int64                  `bson:"createdAt" json:"createdAt"`
	UpdatedAt             int64                  `bson:"updatedAt" json:"updatedAt"`
	DeletedAt             *int64                 `bson:"deletedAt" json:"deletedAt"`
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
	Data struct {
		Total int                         `json:"total"`
		Items []ApplicationEndpointPolicy `json:"items"`
	} `json:"data"`
}

// Route defines an individual route configuration
type Route struct {
	Method string
	URL    string
}

// VerifyResponse represents the response from the verification endpoint
type VerifyResponse struct {
	AccountId     string `json:"accountId"`
	ApplicationId string `json:"applicationId"`
}

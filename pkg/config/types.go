package config

import "errors"

// ApiConfigStrategy represents the configuration strategy for the API
type ApiConfigStrategy struct {
	ID                    string                 `bson:"_id" json:"_id"`
	Name                  string                 `bson:"name" json:"name"`
	AccountId             string                 `bson:"accountId" json:"accountId"`
	IdentityFieldName     string                 `bson:"identityFieldName" json:"identityFieldName"`
	IdentityFieldLocation string                 `bson:"identityFieldLocation" json:"identityFieldLocation"`
	ConfigData            map[string]interface{} `bson:"configData" json:"configData"`
	CreatedAt             int64                  `bson:"createdAt" json:"createdAt"`
	UpdatedAt             int64                  `bson:"updatedAt" json:"updatedAt"`
	DeletedAt             *int64                 `bson:"deletedAt" json:"deletedAt"`
}

// Route defines an individual route configuration
type Route struct {
	Method string
	URL    string
}

// Validate checks if the route configuration is valid
func (r *Route) Validate() error {
	if r.Method == "" {
		return errors.New("method is required")
	}
	if r.URL == "" {
		return errors.New("URL is required")
	}
	return nil
}

// VerifyResponse represents the response from the verification endpoint
type VerifyResponse struct {
	AccountId     string `json:"accountId"`
	ApplicationId string `json:"applicationId"`
}

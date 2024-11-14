package reloadlymodels

// / Authentication config for token retrieval
type AuthConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	GrantType    string `json:"grant_type"`
	Audience     string `json:"audience"`
}

// TokenApiResponse represents the structure of the API response for token retrieval
type TokenApiResponse struct {
	AccessToken string `json:"access_token"`
	Scope       string `json:"scope"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

// ProductQueryParams represents the available query parameters for the products endpoint
type ProductQueryParams struct {
	Size         int
	Page         int
	IncludeRange bool
	IncludeFixed bool
}

// PageResponse represents a generic paginated response
type PageResponse[T any] struct {
	Content          []T   `json:"content"`
	First            bool  `json:"first"`
	Last             bool  `json:"last"`
	Number           int   `json:"number"`
	NumberOfElements int   `json:"numberOfElements"`
	Size             int   `json:"size"`
	TotalElements    int64 `json:"totalElements"`
	TotalPages       int   `json:"totalPages"`
}

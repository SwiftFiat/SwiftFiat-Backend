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

// Product represents a gift card product with all its details
type Product struct {
	ProductID   int    `json:"productId"`
	ProductName string `json:"productName"`
	Global      bool   `json:"global"`

	// Ordering capabilities
	SupportsPreOrder bool    `json:"supportsPreOrder"`
	SenderFee        float64 `json:"senderFee"`

	// Pricing details
	DiscountPercentage float64 `json:"discountPercentage"`
	DenominationType   string  `json:"denominationType"`

	// Currency and denomination details
	RecipientCurrencyCode       string    `json:"recipientCurrencyCode"`
	MinRecipientDenomination    *float64  `json:"minRecipientDenomination"`
	MaxRecipientDenomination    *float64  `json:"maxrecipientDenomination"`
	SenderCurrencyCode          string    `json:"senderCurrencyCode"`
	MinSenderDenomination       *float64  `json:"minSenderDenomination"`
	MaxSenderDenomination       *float64  `json:"maxSenderDenomination"`
	FixedRecipientDenominations []float64 `json:"fixedRecipientDenominations"`
	FixedSenderDenominations    []float64 `json:"fixedSenderDenominations"`

	// Denomination mapping
	FixedRecipientToSenderDenominationsMap []map[string]float64 `json:"fixedRecipientToSenderDenominationsMap"`

	// Media
	LogoURLs []string `json:"logoUrls"`

	// Related entities
	Brand    Brand    `json:"brand"`
	Category Category `json:"category"`
	Country  Country  `json:"country"`

	// Instructions
	RedeemInstruction RedeemInstruction `json:"redeemInstruction"`
}

// Brand represents the brand information
type Brand struct {
	BrandID   int    `json:"brandId"`
	BrandName string `json:"brandName"`
}

// Category represents the product category
type Category struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Country represents the country information
type Country struct {
	ISOName string `json:"isoName"`
	Name    string `json:"name"`
	FlagURL string `json:"flagUrl"`
}

// RedeemInstruction contains redemption instructions
type RedeemInstruction struct {
	Concise string `json:"concise"`
	Verbose string `json:"verbose"`
}

// Sort represents the sorting information in the pagination response
type Sort struct {
	Empty    bool `json:"empty"`
	Sorted   bool `json:"sorted"`
	Unsorted bool `json:"unsorted"`
}

// Pageable represents the pagination information
type Pageable struct {
	Offset     int  `json:"offset"`
	PageNumber int  `json:"pageNumber"`
	PageSize   int  `json:"pageSize"`
	Paged      bool `json:"paged"`
	Sort       Sort `json:"sort"`
	Unpaged    bool `json:"unpaged"`
}

// PageResponse represents a generic paginated response
type PageResponse[T any] struct {
	Content          []T      `json:"content"`
	Empty            bool     `json:"empty"`
	First            bool     `json:"first"`
	Last             bool     `json:"last"`
	Number           int      `json:"number"`
	NumberOfElements int      `json:"numberOfElements"`
	Pageable         Pageable `json:"pageable"`
	Size             int      `json:"size"`
	Sort             Sort     `json:"sort"`
	TotalElements    int64    `json:"totalElements"`
	TotalPages       int      `json:"totalPages"`
}

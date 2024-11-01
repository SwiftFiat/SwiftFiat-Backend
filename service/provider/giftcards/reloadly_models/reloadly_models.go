package reloadlymodels

// ProductQueryParams represents the available query parameters for the products endpoint
type ProductQueryParams struct {
	Size              int
	Page              int
	ProductName       string
	CountryCode       string
	ProductCategoryID int
	IncludeRange      bool
	IncludeFixed      bool
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

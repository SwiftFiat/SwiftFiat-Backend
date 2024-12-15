package reloadlymodels

type GiftCardPurchaseResponse struct {
	TransactionID    int64   `json:"transactionId"`
	Amount           float64 `json:"amount"`
	Discount         float64 `json:"discount"`
	CurrencyCode     string  `json:"currencyCode"`
	Fee              float64 `json:"fee"`
	SMSFee           float64 `json:"smsFee"`
	RecipientEmail   string  `json:"recipientEmail"`
	RecipientPhone   string  `json:"recipientPhone"`
	CustomIdentifier string  `json:"customIdentifier"`
	Status           string  `json:"status"`
	// TransactionCreatedTime time.Time `json:"transactionCreatedTime"`
	Product Product `json:"product"`
}

type Product struct {
	ProductID    int64   `json:"productId"`
	ProductName  string  `json:"productName"`
	CountryCode  string  `json:"countryCode"`
	Quantity     int64   `json:"quantity"`
	UnitPrice    float64 `json:"unitPrice"`
	TotalPrice   float64 `json:"totalPrice"`
	CurrencyCode string  `json:"currencyCode"`
	Brand        Brand   `json:"brand"`
}

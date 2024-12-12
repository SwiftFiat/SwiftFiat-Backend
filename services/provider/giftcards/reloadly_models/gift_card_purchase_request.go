package reloadlymodels

type GiftCardPurchaseRequest struct {
	ProductID             int64                 `json:"productId"`
	CountryCode           string                `json:"countryCode"`
	Quantity              int64                 `json:"quantity"`
	UnitPrice             int64                 `json:"unitPrice"`
	CustomIdentifier      string                `json:"customIdentifier"`
	SenderName            string                `json:"senderName"`
	RecipientEmail        string                `json:"recipientEmail"`
	RecipientPhoneDetails RecipientPhoneDetails `json:"recipientPhoneDetails"`
}

type RecipientPhoneDetails struct {
	CountryCode string `json:"countryCode"`
	PhoneNumber string `json:"phoneNumber"`
}

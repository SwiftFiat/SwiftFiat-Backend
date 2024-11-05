package reloadlymodels

type GiftCardCollection []GiftCardCollectionElement

type GiftCardCollectionElement struct {
	Brand                                  *Brand             `json:"brand,omitempty"`
	Category                               *Category          `json:"category,omitempty"`
	Country                                *Country           `json:"country,omitempty"`
	DenominationType                       *string            `json:"denominationType,omitempty"`
	DiscountPercentage                     *float64           `json:"discountPercentage,omitempty"`
	FixedRecipientDenominations            []float64          `json:"fixedRecipientDenominations,omitempty"`
	FixedRecipientToSenderDenominationsMap map[string]float64 `json:"fixedRecipientToSenderDenominationsMap"`
	FixedSenderDenominations               []float64          `json:"fixedSenderDenominations"`
	Global                                 *bool              `json:"global,omitempty"`
	LogoUrls                               []string           `json:"logoUrls,omitempty"`
	MaxRecipientDenomination               *float64           `json:"maxRecipientDenomination"`
	MaxSenderDenomination                  *float64           `json:"maxSenderDenomination"`
	Metadata                               interface{}        `json:"metadata"`
	MinRecipientDenomination               *float64           `json:"minRecipientDenomination"`
	MinSenderDenomination                  *float64           `json:"minSenderDenomination"`
	ProductID                              *int64             `json:"productId,omitempty"`
	ProductName                            *string            `json:"productName,omitempty"`
	RecipientCurrencyCode                  *string            `json:"recipientCurrencyCode,omitempty"`
	RedeemInstruction                      *RedeemInstruction `json:"redeemInstruction,omitempty"`
	SenderCurrencyCode                     *string            `json:"senderCurrencyCode,omitempty"`
	SenderFee                              *float64           `json:"senderFee,omitempty"`
	SenderFeePercentage                    *float64           `json:"senderFeePercentage,omitempty"`
	SupportsPreOrder                       *bool              `json:"supportsPreOrder,omitempty"`
}

type Brand struct {
	BrandID   *int64  `json:"brandId,omitempty"`
	BrandName *string `json:"brandName,omitempty"`
}

type Category struct {
	ID   *int64  `json:"id,omitempty"`
	Name *string `json:"name,omitempty"`
}

type Country struct {
	FlagURL *string `json:"flagUrl,omitempty"`
	ISOName *string `json:"isoName,omitempty"`
	Name    *string `json:"name,omitempty"`
}

type RedeemInstruction struct {
	Concise *string `json:"concise,omitempty"`
	Verbose *string `json:"verbose,omitempty"`
}

package reloadlymodels

type BrandCollection []BrandElement

type BrandElement struct {
	BrandName string                      `json:"brandName"`
	ID        int                         `json:"brandId"`
	LogoURL   string                      `json:"logo_url"`
	Products  []GiftCardCollectionElement `json:"gift_cards"`
}

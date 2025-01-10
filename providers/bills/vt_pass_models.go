package bills

type VTPassResponse[T any] struct {
	ResponseDescription string `json:"response_description"`
	Code                string `json:"code"`
	Content             T      `json:"content"`
}

type ServiceCategory struct {
	Identifier string `json:"identifier"`
	Name       string `json:"name"`
}

type ServiceIdentifier struct {
	ServiceID      string `json:"serviceID"`
	Name           string `json:"name"`
	MinimiumAmount string `json:"minimium_amount"`
	MaximumAmount  string `json:"maximum_amount"`
	ConvinienceFee string `json:"convinience_fee"`
	ProductType    string `json:"product_type"`
	Image          string `json:"image"`
}

type ServiceContentWithVariation struct {
	ServiceName    string      `json:"ServiceName"`
	ServiceID      string      `json:"serviceID"`
	ConvinienceFee string      `json:"convinience_fee"`
	Variations     []Variation `json:"varations"`
}

type Variation struct {
	VariationCode   string `json:"variation_code"`
	Name            string `json:"name"`
	VariationAmount string `json:"variation_amount"`
	FixedPrice      string `json:"fixedPrice"`
}

package bills

import (
	"time"

	"github.com/shopspring/decimal"
)

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
	ServiceID      string          `json:"serviceID"`
	Name           string          `json:"name"`
	MinimiumAmount decimal.Decimal `json:"minimium_amount"`
	MaximumAmount  decimal.Decimal `json:"maximum_amount"`
	ConvinienceFee string          `json:"convinience_fee"`
	ProductType    string          `json:"product_type"`
	Image          string          `json:"image"`
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

type PurchaseAirtimeResponse struct {
	Code                string    `json:"code"`
	Content             Content   `json:"content"`
	ResponseDescription string    `json:"response_description"`
	RequestID           string    `json:"requestId"`
	Amount              string    `json:"amount"`
	TransactionDate     time.Time `json:"transaction_date"`
	PurchasedCode       string    `json:"purchased_code"`
}

type Content struct {
	Transaction Transaction `json:"transactions"`
}

type Transaction struct {
	Status              string      `json:"status"`
	ProductName         string      `json:"product_name"`
	UniqueElement       string      `json:"unique_element"`
	UnitPrice           int64       `json:"unit_price"`
	Quantity            int64       `json:"quantity"`
	ServiceVerification interface{} `json:"service_verification"`
	Channel             string      `json:"channel"`
	Commission          int64       `json:"commission"`
	TotalAmount         float64     `json:"total_amount"`
	Discount            interface{} `json:"discount"`
	Type                string      `json:"type"`
	Email               string      `json:"email"`
	Phone               string      `json:"phone"`
	Name                interface{} `json:"name"`
	ConvinienceFee      int64       `json:"convinience_fee"`
	Amount              int64       `json:"amount"`
	Platform            string      `json:"platform"`
	Method              string      `json:"method"`
	TransactionID       string      `json:"transactionId"`
}

type PurchaseAirtimeRequest struct {
	ServiceID string `json:"serviceID"`
	Phone     string `json:"phone"`
	RequestID string `json:"request_id"`
	Amount    int64  `json:"amount"`
}

type PurchaseDataRequest struct {
	ServiceID     string `json:"serviceID"`
	BillersCode   string `json:"billersCode"`
	RequestID     string `json:"request_id"`
	VariationCode string `json:"variation_code"`
	Phone         string `json:"phone"`
	Amount        int64  `json:"amount"`
}

package bills

import (
	"time"
)

type VTPassResponse[T any] struct {
	ResponseDescription string `json:"response_description"`
	Code                string `json:"code"`
	Content             T      `json:"content"`
}

type VTPassError[T any] struct {
	Errors T `json:"errors"`
}

type VTPassErrorItem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ServiceCategory struct {
	Identifier string `json:"identifier"`
	Name       string `json:"name"`
}

type ServiceIdentifier struct {
	ServiceID      string      `json:"serviceID"`
	Name           string      `json:"name"`
	MinimiumAmount interface{} `json:"minimium_amount"`
	MaximumAmount  interface{} `json:"maximum_amount"`
	ConvinienceFee string      `json:"convinience_fee"`
	ProductType    string      `json:"product_type"`
	Image          string      `json:"image"`
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

type PayResponse struct {
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
	UnitPrice           *float64    `json:"unit_price"`
	Quantity            *float64    `json:"quantity"`
	ServiceVerification interface{} `json:"service_verification"`
	Channel             string      `json:"channel"`
	Commission          *float64    `json:"commission"`
	TotalAmount         *float64    `json:"total_amount"`
	Discount            interface{} `json:"discount"`
	Type                string      `json:"type"`
	Email               string      `json:"email"`
	Phone               string      `json:"phone"`
	Name                interface{} `json:"name"`
	ConvinienceFee      *float64    `json:"convinience_fee"`
	Amount              *float64    `json:"amount"`
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
type CustomerInfo struct {
	CustomerName       string      `json:"Customer_Name"`
	Status             string      `json:"Status"`
	DueDate            string      `json:"Due_Date"`
	CustomerNumber     interface{} `json:"Customer_Number"`
	CustomerType       string      `json:"Customer_Type"`
	CurrentBouquet     string      `json:"Current_Bouquet"`
	CurrentBouquetCode string      `json:"Current_Bouquet_Code"`
	RenewalAmount      interface{} `json:"Renewal_Amount"`
}

type GetCustomerInfoRequest struct {
	ServiceID   string `json:"serviceID"`
	BillersCode string `json:"billersCode"`
}

type BuyTVSubscriptionRequest struct {
	ServiceID        string `json:"serviceID"`          // The unique identifier for the TV service provider (e.g. DSTV, GOTV)
	BillersCode      string `json:"billersCode"`        // The customer's smartcard/IUC number
	VariationCode    string `json:"variation_code"`     // The code for the selected subscription package/bouquet
	Amount           int64  `json:"amount,omitempty"`   // The cost of the subscription (optional - derived from variation)
	Phone            string `json:"phone"`              // Customer's phone number for notifications
	SubscriptionType string `json:"subscription_type"`  // Type of subscription (e.g. "renew", "change")
	RequestID        string `json:"request_id"`         // Unique identifier for this transaction
	Quantity         int64  `json:"quantity,omitempty"` // Number of subscriptions to purchase (optional) - months
}

type GetCustomerMeterInfoRequest struct {
	ServiceID   string `json:"serviceID"`
	BillersCode string `json:"billersCode"`
	Type        string `json:"type"` // "postpaid" or "prepaid"
}

type GetCustomerMeterInfoResponse struct {
	Error               string `json:"error"`
	CustomerName        string `json:"Customer_Name"`
	Address             string `json:"Address"`
	MeterNumber         string `json:"Meter_Number"`
	CustomerArrears     any    `json:"Customer_Arrears"`
	MinimumAmount       any    `json:"Minimum_Amount"`      // it's either float64 or string
	MinPurchaseAmount   any    `json:"Min_Purchase_Amount"` // it's either float64 or string
	CanVend             string `json:"Can_Vend"`
	BusinessUnit        string `json:"Business_Unit"`
	CustomerAccountType string `json:"Customer_Account_Type"`
	MeterType           string `json:"Meter_Type"`
	WrongBillersCode    bool   `json:"WrongBillersCode"`
}

type PurchaseElectricityResponse struct {
	Code                string      `json:"code"`
	Content             Content     `json:"content"`
	ResponseDescription string      `json:"response_description"`
	RequestID           string      `json:"requestId"`
	Amount              string      `json:"amount"`
	TransactionDate     string      `json:"transaction_date"`
	PurchasedCode       string      `json:"purchased_code"`
	CustomerName        *string     `json:"customerName"`
	CustomerAddress     *string     `json:"customerAddress"`
	Token               string      `json:"token"`
	TokenAmount         interface{} `json:"tokenAmount"`
	ExchangeReference   string      `json:"exchangeReference"`
	ResetToken          *string     `json:"resetToken"`
	ConfigureToken      *string     `json:"configureToken"`
	Units               interface{} `json:"units"`
	FixChargeAmount     *string     `json:"fixChargeAmount"`
	Tariff              string      `json:"tariff"`
	TaxAmount           *string     `json:"taxAmount"`
}

type PurchaseElectricityRequest struct {
	RequestID     string  `json:"request_id"`
	ServiceID     string  `json:"serviceID"`
	BillersCode   string  `json:"billersCode"`
	VariationCode string  `json:"variation_code"`
	Amount        float64 `json:"amount"`
	Phone         string  `json:"phone"`
}

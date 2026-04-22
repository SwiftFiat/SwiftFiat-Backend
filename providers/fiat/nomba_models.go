package fiat

import (
	"encoding/json"
	"fmt"
)

// ── Auth ─────────────────────────────────────────────────────────────────────

type NombaTokenRequest struct {
	GrantType    string `json:"grant_type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type NombaTokenData struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
}

// UnmarshalJSON tolerates provider payload drift where `data` may be a string
// instead of an object on failed auth responses.
func (d *NombaTokenData) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*d = NombaTokenData{}
		return nil
	}

	if len(data) > 0 && data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		if raw == "" {
			*d = NombaTokenData{}
			return nil
		}
		return fmt.Errorf("unexpected token data string: %q", raw)
	}

	type alias NombaTokenData
	var parsed alias
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*d = NombaTokenData(parsed)
	return nil
}

type NombaTokenResponse = NombaResponse[NombaTokenData]

// ── Generic envelope ──────────────────────────────────────────────────────────

// NombaResponse is Nomba's standard envelope: code "00" == success.
type NombaResponse[T any] struct {
	Code        string      `json:"code"`
	Description string      `json:"description"`
	Message     string      `json:"message"`
	Status      interface{} `json:"status"`
	Data        T           `json:"data"`
}

// ── Banks ─────────────────────────────────────────────────────────────────────

type NombaBank struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type NombaBankListData struct {
	Results []NombaBank `json:"results"`
}

// ── Account lookup ────────────────────────────────────────────────────────────

// AccountLookupRequest is the POST body for /v1/transfers/bank/lookup.
// (already declared in nomba_models.go – kept here for completeness; remove
// the original if you consolidate files)
type NombaAccountLookupRequest struct {
	AccountNumber string `json:"accountNumber"`
	BankCode      string `json:"bankCode"`
}

type NombaAccountLookupData struct {
	AccountNumber string `json:"accountNumber"`
	AccountName   string `json:"accountName"`
}

// ── Transfer ──────────────────────────────────────────────────────────────────

type NombaBankTransferRequest struct {
	Amount        int64  `json:"amount"`
	AccountNumber string `json:"accountNumber"`
	AccountName   string `json:"accountName"`
	BankCode      string `json:"bankCode"`
	MerchantTxRef string `json:"merchantTxRef"`
	SenderName    string `json:"senderName"`
	Narration     string `json:"narration"`
}

type NombaTransferMeta struct {
	APIRRN              string `json:"api_rrn"`
	Narration           string `json:"narration"`
	RecipientName       string `json:"recipientName"`
	SenderName          string `json:"sender_name"`
	MerchantTxRef       string `json:"merchantTxRef"`
	APIClientID         string `json:"api_client_id"`
	Currency            string `json:"currency"`
	HooksEligible       string `json:"hooksEligible"`
	BankingEntityID     string `json:"banking_entity_id"`
	BankingEntityUserID string `json:"banking_entity_user_id"`
	BankingEntityType   string `json:"banking_entity_type"`
	SelfTransaction     string `json:"self_transaction"`
	TransactionCategory string `json:"transactionCategory"`
	AccountNumber       string `json:"accountNumber"`
	BankName            string `json:"bankName"`
	BankCode            string `json:"bankCode"`
	SessionID           string `json:"sessionId"`
	UserReferralCode    string `json:"user_referral_code"`
	AmountCharged       string `json:"amount_charged"`
	PaymentVendor       string `json:"paymentVendor"`
	WalletBalance       string `json:"wallet_balance"`
	WalletCurrency      string `json:"wallet_currency"`
	VendorReference     string `json:"paymentVendorReference"`
	AgentCommission     string `json:"agent_commission"`
	UseV2Fulfilment     string `json:"useV2Fulfilment"`
}

type NombaTransferData struct {
	Amount           interface{}       `json:"amount"` // Can be string or float64
	Meta             NombaTransferMeta `json:"meta"`
	Fee              interface{}       `json:"fee"` // Can be string or float64
	TimeCreated      string            `json:"timeCreated"`
	ID               string            `json:"id"`
	Type             string            `json:"type"`
	Status           string            `json:"status"`
	Source           string            `json:"source"`
	SourceUserID     string            `json:"sourceUserId"`
	CustomerBillerID string            `json:"customerBillerId"`
	ProductID        string            `json:"productId"`
}

// NombaTransferResponse represents the response from a successful transfer
type NombaTransferResponse struct {
	Amount        int64
	Currency      string
	Reference     string
	Reason        string
	Status        string
	TransferCode  string
	SessionID     string
	Fee           int64
	RecipientName string
	BankName      string
	BankCode      string
	AccountNumber string
	RRN           string
	SenderName    string
	RawData       *NombaTransferData
}

// NombaRecipientToken is an opaque string "accountNumber|bankCode|accountName"
// returned by CreateTransferRecipient so callers keep the same flow.
// MakeTransfer parses it back out.
type NombaRecipientToken string

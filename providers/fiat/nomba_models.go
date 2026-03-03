package fiat

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

type NombaTokenResponse = NombaResponse[NombaTokenData]

// ── Generic envelope ──────────────────────────────────────────────────────────

// NombaResponse is Nomba's standard envelope: code "00" == success.
type NombaResponse[T any] struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Data        T      `json:"data"`
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
	MerchantTxRef string `json:"merchantTxRef"`
	APIClientID   string `json:"api_client_id"`
	APIAccountID  string `json:"api_account_id"`
	RRN           string `json:"rrn"`
}

type NombaTransferData struct {
	Amount      string            `json:"amount"`
	Meta        NombaTransferMeta `json:"meta"`
	Fee         string            `json:"fee"`
	TimeCreated string            `json:"timeCreated"`
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Status      string            `json:"status"`
}

// NombaTransferResponse represents the response from a successful transfer
type NombaTransferResponse struct {
	Amount       int64
	Currency     string
	Reference    string
	Reason       string
	Status       string
	TransferCode string
}

// NombaRecipientToken is an opaque string "accountNumber|bankCode|accountName"
// returned by CreateTransferRecipient so callers keep the same flow.
// MakeTransfer parses it back out.
type NombaRecipientToken string

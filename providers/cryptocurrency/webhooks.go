package cryptocurrency

type WebhookTransferPayload struct {
	Hash            *string  `json:"hash,omitempty"`
	Transfer        *string  `json:"transfer,omitempty"`
	Coin            *string  `json:"coin,omitempty"`
	Type            *string  `json:"type,omitempty"`
	State           *string  `json:"state,omitempty"`
	Wallet          *string  `json:"wallet,omitempty"`
	WalletType      *string  `json:"walletType,omitempty"`
	TransferType    *string  `json:"transferType,omitempty"`
	BaseValue       *int64   `json:"baseValue,omitempty"`
	BaseValueString *string  `json:"baseValueString,omitempty"`
	Value           *int64   `json:"value,omitempty"`
	ValueString     *string  `json:"valueString,omitempty"`
	FeeString       *string  `json:"feeString,omitempty"`
	Initiator       []string `json:"initiator,omitempty"`
	Receiver        *string  `json:"receiver,omitempty"`
	Simulation      *bool    `json:"simulation,omitempty"`
}

// WebhookPayload represents the Cryptomus webhook structure
type WebhookPayload struct {
	Type              string  `json:"type"`
	UUID              string  `json:"uuid"`
	OrderID           string  `json:"order_id"`
	Amount            string  `json:"amount"`
	PaymentAmount     string  `json:"payment_amount"`
	PaymentAmountUSD  string  `json:"payment_amount_usd"`
	MerchantAmount    string  `json:"merchant_amount"`
	Commission        string  `json:"commission"`
	IsFinal           bool    `json:"is_final"`
	Status            string  `json:"status"`
	From              string  `json:"from"`
	WalletAddressUUID *string `json:"wallet_address_uuid"`
	Network           string  `json:"network"`
	Currency          string  `json:"currency"`
	PayerCurrency     string  `json:"payer_currency"`
	AdditionalData    *string `json:"additional_data"`
	Convert           struct {
		ToCurrency string  `json:"to_currency"`
		Commission *string `json:"commission"`
		Rate       string  `json:"rate"`
		Amount     string  `json:"amount"`
	} `json:"convert"`
	TxID string `json:"txid"`
	Sign string `json:"sign"`
}

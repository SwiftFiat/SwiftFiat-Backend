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

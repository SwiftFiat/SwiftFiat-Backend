package cryptocurrency

type WalletCreationModel struct {
	Label                           string `json:"label"`                           // The label for the wallet
	Passphrase                      string `json:"passphrase"`                      // The wallet's passphrase
	Enterprise                      string `json:"enterprise"`                      // Enterprise ID associated with the wallet
	DisableTransactionNotifications bool   `json:"disableTransactionNotifications"` // Flag to disable transaction notifications
	DisableKRSEmail                 bool   `json:"disableKRSEmail"`                 // Flag to disable KRS email notifications
}

type WalletAddress struct {
	Address      string       `json:"address"`
	AddressType  string       `json:"addressType"`
	Chain        int64        `json:"chain"`
	Coin         string       `json:"coin"`
	CoinSpecific CoinSpecific `json:"coinSpecific"`
	ID           string       `json:"id"`
	Index        int64        `json:"index"`
	Keychains    []Keychain   `json:"keychains"`
	Wallet       string       `json:"wallet"`
}

type CoinSpecific struct {
}

type Keychain struct {
	EncryptedPrv *string `json:"encryptedPrv,omitempty"`
	EthAddress   string  `json:"ethAddress"`
	ID           string  `json:"id"`
	Pub          string  `json:"pub"`
	Source       string  `json:"source"`
	Type         string  `json:"type"`
	IsBitGo      *bool   `json:"isBitGo,omitempty"`
}

type BitGoWalletResponse struct {
	Wallets []Wallet `json:"wallets"`
}

type Wallet struct {
	ApprovalsRequired               int64              `json:"approvalsRequired"`
	Coin                            string             `json:"coin"`
	CoinSpecific                    WalletCoinSpecific `json:"coinSpecific"`
	Deleted                         bool               `json:"deleted"`
	DisableTransactionNotifications bool               `json:"disableTransactionNotifications"`
	HasLargeNumberOfAddresses       bool               `json:"hasLargeNumberOfAddresses"`
	ID                              string             `json:"id"`
}
type WalletCoinSpecific struct {
	CreationFailure            []interface{} `json:"creationFailure,omitempty"`
	PendingChainInitialization *bool         `json:"pendingChainInitialization,omitempty"`
	RootAddress                *string       `json:"rootAddress,omitempty"`
	TrustedTokens              []interface{} `json:"trustedTokens,omitempty"`
}

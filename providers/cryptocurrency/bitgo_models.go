package cryptocurrency

var supportedCoinLabels = map[string]string{
	"BCH":   "Bitcoin Cash",
	"bch":   "Bitcoin Cash",
	"BNB":   "Binance Coin",
	"bnb":   "Binance Coin",
	"BSC":   "Binance Smart Chain",
	"bsc":   "Binance Smart Chain",
	"BTC":   "Bitcoin",
	"btc":   "Bitcoin",
	"DOGE":  "Dogecoin",
	"doge":  "Dogecoin",
	"DOT":   "Polkadot",
	"dot":   "Polkadot",
	"ETH":   "Ethereum",
	"eth":   "Ethereum",
	"LINK":  "Chainlink",
	"link":  "Chainlink",
	"LTC":   "Litecoin",
	"ltc":   "Litecoin",
	"MATIC": "Polygon",
	"matic": "Polygon",
	"SHIB":  "Shiba Inu",
	"shib":  "Shiba Inu",
	"SOL":   "Solana",
	"sol":   "Solana",
	"TRON":  "TRON",
	"tron":  "TRON",
	"UNI":   "Uniswap",
	"uni":   "Uniswap",
	"USDC":  "USD Coin",
	"usdc":  "USD Coin",
	"USDT":  "USD Tether",
	"usdt":  "USD Tether",
	"XLM":   "Stellar",
	"xlm":   "Stellar",
	"XRP":   "Ripple",
	"xrp":   "Ripple",

	// Testnet
	"tbtc":   "Test Bitcoin",
	"tbtc4":  "Test Bitcoin",
	"tbch":   "Test Bitcoin Cash",
	"tbnb":   "Test Binance Coin",
	"tbsc":   "Test Binance Smart Chain",
	"tdoge":  "Test Dogecoin",
	"tdot":   "Test Polkadot",
	"teth":   "Test Ethereum",
	"tlink":  "Test Chainlink",
	"tltc":   "Test Litecoin",
	"tmatic": "Test Polygon",
	"tshib":  "Test Shiba Inu",
	"tsol":   "Test Solana",
	"ttron":  "Test TRON",
	"tuni":   "Test Uniswap",
	"tusdc":  "Test USD Coin",
	"tusdt":  "Test Tether",
	"txlm":   "Test Stellar",
	"txrp":   "Test Ripple",
}

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

func GetWalletLabel(coin string) string {
	label, ok := supportedCoinLabels[coin]
	if !ok {
		return coin
	}
	return label
}

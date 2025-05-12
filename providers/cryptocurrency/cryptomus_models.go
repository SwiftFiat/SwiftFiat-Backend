package cryptocurrency

type ServicesRawResponse struct {
	Result []CryptomusService `json:"result,omitempty"`
	State  int8               `json:"state"`
}

type CryptomusService struct {
	Commission struct {
		FeeAmount string `json:"fee_amount"`
		Percent   string `json:"percent"`
	} `json:"commission"`
	Currency    string `json:"currency"`
	IsAvailable bool   `json:"is_available"`
	Limit       struct {
		MaxAmount string `json:"max_amount"`
		MinAmount string `json:"min_amount"`
	} `json:"limit"`
	Network string `json:"network"`
}

type StaticWalletRequest struct {
	Currency         string `json:"currency"`
	Network          string `json:"network"`
	OrderId          string `json:"order_id"`
	UrlCallback      string `json:"url_callback,omitempty"`
	FromReferralCode string `json:"from_referral_code,omitempty"`
}

type StaticWalletResponse struct {
	WalletUUID string `json:"wallet_uuid"`
	UUID       string `json:"uuid"`
	Address    string `json:"address"`
	Network    string `json:"network"`
	Currency   string `json:"currency"`
	Url        string `json:"url"`
}

type StaticWalletRawResponse struct {
	Result *StaticWalletResponse `json:"result"`
	State  int8                  `json:"state"`
}

type GenerateQRCodeResponse struct {
	ImageUrl string `json:"image_url"`
}

type GenerateQRCodeRawResponse struct {
	Result *GenerateQRCodeResponse `json:"result"`
	State  int8                    `json:"state"`
} 

type CoinRankingResponse struct {
	UUID              string   `json:"uuid"`
	Symbol            string   `json:"symbol"`
	Name              string   `json:"name"`
	Color             string   `json:"color"`
	IconURL           string   `json:"iconUrl"`
	MarketCap         string   `json:"marketCap"`
	Price             string   `json:"price"`
	ListedAt          int64    `json:"listedAt"`
	Change            string   `json:"change"`
	Rank              int      `json:"rank"`
	Sparkline         []string `json:"sparkline"`
	LowVolume         bool     `json:"lowVolume"`
	CoinrankingURL    string   `json:"coinrankingUrl"`
	Volume24h         string   `json:"24hVolume"`
	BTCPrice          string   `json:"btcPrice"`
	ContractAddresses []string `json:"contractAddresses"`
}

type CryptomusCoinPrice struct {
	
}
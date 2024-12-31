package cryptocurrency

type SupportedCoin string
type coin struct {
	TBTC4    SupportedCoin
	TBTCSIG  SupportedCoin
	TBTC     SupportedCoin
	TXRP     SupportedCoin
	TADA     SupportedCoin
	TLTC     SupportedCoin
	TDOGE    SupportedCoin
	TDOT     SupportedCoin
	TSOL     SupportedCoin
	TTON     SupportedCoin
	TETC     SupportedCoin
	TXLM     SupportedCoin
	TBCH     SupportedCoin
	TFIATUSD SupportedCoin
	THBAR    SupportedCoin
	TCELO    SupportedCoin
	TOPETH   SupportedCoin
	TARBETH  SupportedCoin
	TPOLYGON SupportedCoin
	TNEAR    SupportedCoin
	TSUI     SupportedCoin
	TSUSD    SupportedCoin
	TXTZ     SupportedCoin
	TCSPR    SupportedCoin
	TAVAXC   SupportedCoin
	TALGO    SupportedCoin
	TAVAXP   SupportedCoin
	TZEC     SupportedCoin
	TDASH    SupportedCoin
	TLNBTC   SupportedCoin
}

var Coin = coin{
	TBTC4:    SupportedCoin("tbtc4"),
	TBTCSIG:  SupportedCoin("tbtcsig"),
	TBTC:     SupportedCoin("tbtc"),
	TXRP:     "txrp",
	TDOGE:    "tdoge",
	TADA:     "tada",
	TLTC:     "tltc",
	TDOT:     "tdot",
	TSOL:     "tsol",
	TTON:     "tton",
	TETC:     "tetc",
	TXLM:     "txlm",
	TBCH:     "tbch",
	TFIATUSD: "tfiatusd",
	THBAR:    "thbar",
	TCELO:    "tcelo",
	TOPETH:   "topeth",
	TARBETH:  "tarbeth",
	TPOLYGON: "tpolygon",
	TNEAR:    "tnear",
	TSUI:     "tsui",
	TSUSD:    "tsusd",
	TXTZ:     "txtz",
	TCSPR:    "tcspr",
	TAVAXC:   "tavaxc",
	TALGO:    "talgo",
	TAVAXP:   "tavaxp",
	TZEC:     "tzec",
	TDASH:    "tdash",
	TLNBTC:   "tlnbtc",
}

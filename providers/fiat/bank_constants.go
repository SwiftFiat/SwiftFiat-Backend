package fiat

var bankCodeToLogoMapping = map[string]string{
	"044":    "https://nigerianbanks.xyz/logo/access-bank.png",
	"063":    "https://nigerianbanks.xyz/logo/access-bank-diamond.png",
	"035A":   "https://nigerianbanks.xyz/logo/alat-by-wema.png",
	"401":    "https://nigerianbanks.xyz/logo/asosavings.png",
	"50931":  "https://nigerianbanks.xyz/logo/default-image.png",
	"50823":  "https://nigerianbanks.xyz/logo/cemcs-microfinance-bank.png",
	"023":    "https://nigerianbanks.xyz/logo/citibank-nigeria.png",
	"050":    "https://nigerianbanks.xyz/logo/ecobank-nigeria.png",
	"562":    "https://nigerianbanks.xyz/logo/ekondo-microfinance-bank.png",
	"070":    "https://nigerianbanks.xyz/logo/fidelity-bank.png",
	"011":    "https://nigerianbanks.xyz/logo/first-bank-of-nigeria.png",
	"214":    "https://nigerianbanks.xyz/logo/first-city-monument-bank.png",
	"00103":  "https://nigerianbanks.xyz/logo/globus-bank.png",
	"058":    "https://nigerianbanks.xyz/logo/guaranty-trust-bank.png",
	"50383":  "https://nigerianbanks.xyz/logo/default-image.png",
	"030":    "https://nigerianbanks.xyz/logo/heritage-bank.png",
	"301":    "https://nigerianbanks.xyz/logo/default-image.png",
	"082":    "https://nigerianbanks.xyz/logo/keystone-bank.png",
	"50211":  "https://nigerianbanks.xyz/logo/kuda-bank.png",
	"565":    "https://nigerianbanks.xyz/logo/default-image.png",
	"327":    "https://nigerianbanks.xyz/logo/paga.png",
	"526":    "https://nigerianbanks.xyz/logo/default-image.png",
	"100004": "https://nigerianbanks.xyz/logo/default-image.png",
	"076":    "https://nigerianbanks.xyz/logo/polaris-bank.png",
	"101":    "https://nigerianbanks.xyz/logo/default-image.png",
	"125":    "https://nigerianbanks.xyz/logo/default-image.png",
	"51310":  "https://nigerianbanks.xyz/logo/sparkle-microfinance-bank.png",
	"221":    "https://nigerianbanks.xyz/logo/stanbic-ibtc-bank.png",
	"068":    "https://nigerianbanks.xyz/logo/standard-chartered-bank.png",
	"232":    "https://nigerianbanks.xyz/logo/sterling-bank.png",
	"100":    "https://nigerianbanks.xyz/logo/default-image.png",
	"302":    "https://nigerianbanks.xyz/logo/taj-bank.png",
	"51211":  "https://nigerianbanks.xyz/logo/default-image.png",
	"102":    "https://nigerianbanks.xyz/logo/default-image.png",
	"032":    "https://nigerianbanks.xyz/logo/union-bank-of-nigeria.png",
	"033":    "https://nigerianbanks.xyz/logo/united-bank-for-africa.png",
	"215":    "https://nigerianbanks.xyz/logo/default-image.png",
	"566":    "https://nigerianbanks.xyz/logo/default-image.png",
	"035":    "https://nigerianbanks.xyz/logo/wema-bank.png",
	"057":    "https://nigerianbanks.xyz/logo/zenith-bank.png",
}

func GetBankLogoByCode(bankCode string) string {
	logo, exists := bankCodeToLogoMapping[bankCode]
	if !exists {
		return "https://nigerianbanks.xyz/logo/default-image.png"
	}
	return logo
}

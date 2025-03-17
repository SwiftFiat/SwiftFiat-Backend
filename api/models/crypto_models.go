package models

import (
	"fmt"
	"strings"

	"github.com/SwiftFiat/SwiftFiat-Backend/providers/cryptocurrency"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

type CryptoWalletsResponse []CryptoWalletResponse

type CryptoWalletResponse struct {
	Label                           string      `json:"label"`
	ApprovalsRequired               int64       `json:"approvalsRequired"`
	Coin                            string      `json:"coin"`
	CoinSpecific                    interface{} `json:"coinSpecific"`
	Deleted                         bool        `json:"deleted"`
	DisableTransactionNotifications bool        `json:"disableTransactionNotifications"`
	HasLargeNumberOfAddresses       bool        `json:"hasLargeNumberOfAddresses"`
	ID                              string      `json:"id"`
	CoinAsset                       string      `json:"coinAsset"`
}

type CryptoServicesResponse struct {
	Coin      string `json:"coin"`
	CoinAsset string `json:"coinAsset"`
	Network   string `json:"network"`
}

func ToCryptoWalletResponse(wallet *cryptocurrency.Wallet) *CryptoWalletResponse {
	return &CryptoWalletResponse{
		Label:                           cryptocurrency.GetWalletLabel(wallet.Coin),
		ApprovalsRequired:               wallet.ApprovalsRequired,
		Coin:                            wallet.Coin,
		CoinSpecific:                    wallet.CoinSpecific,
		Deleted:                         wallet.Deleted,
		DisableTransactionNotifications: wallet.DisableTransactionNotifications,
		HasLargeNumberOfAddresses:       wallet.HasLargeNumberOfAddresses,
		ID:                              wallet.ID,
		CoinAsset:                       fmt.Sprintf("/crypto/assets/%s.svg", strings.ToLower(wallet.Coin)),
	}
}

func ToCryptoWalletsResponse(wallets *cryptocurrency.BitGoWalletResponse) *CryptoWalletsResponse {
	var cryptoWallets CryptoWalletsResponse
	for _, wallet := range wallets.Wallets {
		if !wallet.Deleted {
			cryptoWallets = append(cryptoWallets, *ToCryptoWalletResponse(&wallet))
		}
	}
	return &cryptoWallets
}

func ToCryptoServiceResponse(service *cryptocurrency.CryptomusService) *CryptoServicesResponse {
	return &CryptoServicesResponse{
		Coin:      service.Currency,
		Network:   service.Network,
		CoinAsset: fmt.Sprintf("/crypto/assets/%s.svg", strings.ToLower(service.Currency)),
	}
}

func ToCryptoServicesResponse(services []cryptocurrency.CryptomusService) *[]CryptoServicesResponse {
	var cryptoServices []CryptoServicesResponse
	for _, service := range services {
		if service.IsAvailable {
			cryptoServices = append(cryptoServices, *ToCryptoServiceResponse(&service))
		}
	}
	return &cryptoServices
}

func GetCryptoCallbackURL(config *utils.Config, orderID string) string {
	return fmt.Sprintf("%s/crypto/callback/%s", config.ServerBaseURL, orderID)
}

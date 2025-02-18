package models

import (
	"fmt"
	"strings"

	"github.com/SwiftFiat/SwiftFiat-Backend/providers/cryptocurrency"
)

type CryptoWalletsResponse []CryptoWalletResponse

type CryptoWalletResponse struct {
	ApprovalsRequired               int64       `json:"approvalsRequired"`
	Coin                            string      `json:"coin"`
	CoinSpecific                    interface{} `json:"coinSpecific"`
	Deleted                         bool        `json:"deleted"`
	DisableTransactionNotifications bool        `json:"disableTransactionNotifications"`
	HasLargeNumberOfAddresses       bool        `json:"hasLargeNumberOfAddresses"`
	ID                              string      `json:"id"`
	CoinAsset                       string      `json:"coinAsset"`
}

func ToCryptoWalletResponse(wallet *cryptocurrency.Wallet) *CryptoWalletResponse {
	return &CryptoWalletResponse{
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

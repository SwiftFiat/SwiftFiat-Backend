package models

import (
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/cryptocurrency"
	"github.com/google/uuid"
)

type AddressUserResponse struct {
	AddressID  uuid.UUID `json:"id"`
	CustomerID ID        `json:"customer_id"`
	Address    string    `json:"address"`
	Chain      int64     `json:"chain"`
	Coin       string    `json:"currency"`
	Balance    string    `json:"balance"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func MapWalletAddressToAddressUserResponse(walletAddress *cryptocurrency.WalletAddress, customerID ID, balance string, status string, createdAt, updatedAt time.Time) *AddressUserResponse {
	return &AddressUserResponse{
		AddressID:  uuid.MustParse(walletAddress.ID), // Assuming walletAddress.ID is a valid UUID
		CustomerID: customerID,
		Address:    walletAddress.Address,
		Chain:      walletAddress.Chain,
		Coin:       walletAddress.Coin,
		Balance:    balance,
		Status:     status,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}
}

func MapExistingWalletAddressToAddressUserResponse(walletAddress *db.CryptoAddress) *AddressUserResponse {
	return &AddressUserResponse{
		AddressID:  walletAddress.ID,
		CustomerID: ID(walletAddress.CustomerID.Int64),
		Address:    walletAddress.AddressID,
		// Chain:      walletAddress.Chain,
		Coin:      walletAddress.Coin,
		Balance:   walletAddress.Balance.String,
		Status:    walletAddress.Status,
		CreatedAt: walletAddress.CreatedAt,
		UpdatedAt: walletAddress.UpdatedAt,
	}
}

// / Cryptomus Addresses
type CryptomusAddressResponse struct {
	Address    string `json:"address"`
	Network    string `json:"network"`
	Currency   string `json:"currency"`
	PaymentUrl string `json:"payment_url"`
	Callback   string `json:"callback"`
}

func MapCryptomusStaticWalletResponseToCryptomusAddressResponse(cryptomusStaticWalletResponse *cryptocurrency.StaticWalletResponse, callbackURL string) *CryptomusAddressResponse {
	return &CryptomusAddressResponse{
		Address:    cryptomusStaticWalletResponse.Address,
		Network:    cryptomusStaticWalletResponse.Network,
		Currency:   cryptomusStaticWalletResponse.Currency,
		PaymentUrl: cryptomusStaticWalletResponse.Url,
		Callback:   callbackURL,
	}
}

func MapDBCryptomusAddressToCryptomusAddressResponse(cryptomusAddress *db.CryptomusAddress) *CryptomusAddressResponse {
	return &CryptomusAddressResponse{
		Address:    cryptomusAddress.Address,
		Network:    cryptomusAddress.Network,
		Currency:   cryptomusAddress.Currency,
		PaymentUrl: cryptomusAddress.PaymentUrl.String,
		Callback:   cryptomusAddress.CallbackUrl.String,
	}
}

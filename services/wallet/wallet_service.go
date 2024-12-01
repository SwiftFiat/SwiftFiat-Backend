package wallet

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var ValidWalletTypes = []string{"personal", "business", "savings", "checking"}

type WalletService struct {
	store  *db.Store
	logger *logging.Logger
}

func NewWalletService(store *db.Store, logger *logging.Logger) *WalletService {
	return &WalletService{
		store:  store,
		logger: logger,
	}
}

func IsWalletTypeValid(request string) bool {
	for _, c := range ValidWalletTypes {
		if request == c {
			return true
		}
	}

	return false
}

func (w *WalletService) GetWallet(ctx context.Context, dbTx *sql.Tx, walletID uuid.UUID) (*WalletModel, error) {
	w.logger.Info("Fetching wallet")
	db_wallet, err := w.store.WithTx(dbTx).GetWallet(ctx, walletID)
	if err == sql.ErrNoRows {
		return nil, ErrWalletNotFound
	} else if err != nil {
		return nil, err
	}
	return ToWalletModel(db_wallet), err
}

// / QUE: Should be in Wallet? or UserService?
func (w *WalletService) ResolveTag(ctx context.Context, dbTx *sql.Tx, tag string) (interface{}, error) {
	w.logger.Info(fmt.Sprintf("Fetching user account for tag -> %v", tag))
	// db_wallet, err := w.store.WithTx(dbTx).GetWallet(ctx, walletID)
	return nil, fmt.Errorf("not implemented")
}

func (w *WalletService) CreateWallets(ctx context.Context, dbTx *sql.Tx, userID int64, all bool) ([]*WalletModel, error) {
	var wallets []*WalletModel
	/// 'all' signifies currently supported currencies (NGN | USD | EUR)
	if dbTx == nil {
		return nil, fmt.Errorf("no transaction provided: %v", dbTx)
	}

	if all {
		for _, currency := range currency.SupportedCurrencies {
			walletType := ValidWalletTypes[0] // Personal
			param := db.CreateWalletParams{
				CustomerID: userID,
				Type:       walletType,
				Currency:   currency,
				Balance: sql.NullString{
					String: "0",
					Valid:  true,
				},
			}

			/// NOTE: Using a DBTX here causes the transaction to be terminated early
			db_wallet, err := w.store.CreateWallet(ctx, param)
			if err == nil {
				wallets = append(wallets, ToWalletModel(db_wallet))
			}
		}

		/// Check all wallets of user
		userWallets, err := w.store.WithTx(dbTx).GetWalletByCustomerID(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("user wallet retrieval issues: %v", err)
		}

		if len(userWallets) == len(currency.SupportedCurrencies) {
			_, err := w.store.WithTx(dbTx).UpdateUserWalletStatus(ctx, db.UpdateUserWalletStatusParams{
				HasWallets: true,
				UpdatedAt:  time.Now(),
				ID:         userID,
			})
			if err != nil {
				return nil, err
			}
		}
	}

	return wallets, nil
}

func (w *WalletService) UpdateWalletBalanceFromCrypto(ctx context.Context, cryptoAddress string, amount decimal.Decimal, hash string) error {
	dbTx, err := w.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	// Get Address Info from DB
	walletAddress, err := w.store.WithTx(dbTx).FetchByAddressID(ctx, cryptoAddress)
	if err != nil {
		return fmt.Errorf("failed to fetch swift address with this addess ID: %v", err)
	}

	// Verify against recorded transaction collection for address

	params := db.UpdateAddressBalanceByAddressIDParams{
		AddressID: walletAddress.AddressID,
		Balance: sql.NullString{
			String: amount.String(),
			Valid:  amount.String() != "",
		},
	}

	// Update Crypto Address Balance in DB
	_, err = w.store.WithTx(dbTx).UpdateAddressBalanceByAddressID(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to update the swift address with new changes %v", err)
	}

	// Pull users USD wallet
	walletParams := db.GetWalletByCurrencyForUpdateParams{
		CustomerID: walletAddress.CustomerID.Int64,
		Currency:   "USD", // TODO: Look into these as constants or enums
	}
	_, err = w.store.WithTx(dbTx).GetWalletByCurrencyForUpdate(ctx, walletParams)

	// Check if trail exists for this hash
	trailExists, err := w.store.WithTx(dbTx).CheckCryptoTransactionTrailByTransactionHash(ctx, hash)
	if err != nil {
		return fmt.Errorf("issue with checking for transactionHash %v", err)
	}

	if trailExists {
		return fmt.Errorf("transaction already recorded, please check transaction hash: %v", hash)
	} else {
		params := db.CreateCryptoTransactionTrailParams{
			AddressID:       cryptoAddress,
			TransactionHash: hash,
			Amount: sql.NullString{
				String: amount.String(),
				Valid:  amount.String() != "",
			},
		}
		_, err := w.store.WithTx(dbTx).CreateCryptoTransactionTrail(ctx, params)
		if err != nil {
			return fmt.Errorf("error ")
		}
	}

	// Update wallet with balance

	// walletParams := db.UpdateWalletBalanceParams{
	// 	Amount: sql.NullString{
	// 		String: amount.String(),
	// 		Valid: amount.String() != "",
	// 	},
	// 	ID:

	// }
	// _, err = w.store.WithTx(dbTx).UpdateWalletBalance(ctx, )
	return nil
}

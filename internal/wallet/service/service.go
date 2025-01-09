package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/internal/wallet/domain"
	"github.com/SwiftFiat/SwiftFiat-Backend/internal/wallet/mapper"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/fiat"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/redis"
	"github.com/google/uuid"
)

var ValidWalletTypes = []string{"personal", "business", "savings", "checking"}

type WalletService struct {
	store  *db.Store
	logger *logging.Logger
	redis  *redis.RedisService
}

func NewWalletService(store *db.Store, logger *logging.Logger) *WalletService {
	return &WalletService{
		store:  store,
		logger: logger,
	}
}

func NewWalletServiceWithCache(store *db.Store, logger *logging.Logger, redis *redis.RedisService) *WalletService {
	return &WalletService{
		store:  store,
		logger: logger,
		redis:  redis,
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

func (w *WalletService) GetWallet(ctx context.Context, dbTx *sql.Tx, walletID uuid.UUID) (*domain.WalletModel, error) {
	w.logger.Info("Fetching wallet")
	db_wallet, err := w.store.WithTx(dbTx).GetWallet(ctx, walletID)
	if err == sql.ErrNoRows {
		return nil, domain.ErrWalletNotFound
	} else if err != nil {
		return nil, err
	}
	return mapper.ToWalletModel(db_wallet), err
}

func (w *WalletService) ResolveTag(ctx context.Context, tag string, currency string) (*db.GetWalletByTagRow, error) {
	w.logger.Info(fmt.Sprintf("Fetching user account for tag -> %v", tag))

	params := db.GetWalletByTagParams{
		UserTag: sql.NullString{
			String: tag,
			Valid:  tag != "",
		},
		Currency: currency,
	}

	db_wallet, err := w.store.GetWalletByTag(ctx, params)

	if err != nil {
		w.logger.Error(fmt.Sprintf("error fetching wallet: %v", err))
		if err == sql.ErrNoRows {
			return nil, domain.NewWalletError(domain.ErrWalletNotFound, "", err)
		}

		return nil, err
	}
	return &db_wallet, nil
}

func (w *WalletService) CreateWallets(ctx context.Context, dbTx *sql.Tx, userID int64, all bool) ([]*domain.WalletModel, error) {
	var wallets []*domain.WalletModel
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
			db_wallet, err := w.store.WithTx(dbTx).CreateWallet(ctx, param)
			if err != nil {
				w.logger.Error(fmt.Sprintf("error creating wallet: %v", err))
				return nil, err
			}

			wallets = append(wallets, mapper.ToWalletModel(db_wallet))
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

func (w *WalletService) GetFiatBanks(prov *providers.ProviderService, query *string) (*models.BankResponseCollection, error) {

	/// Check existence of banks in Cache
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	var cachedBanks models.BankResponseCollection
	cachedBanks, err := w.redis.GetBankResponseCollection(ctx, "banks")
	if err != nil {
		w.logger.Error(fmt.Sprintf("failed to fetch banks from redisCache: %v", err))
		return nil, err
	}

	if len(cachedBanks) > 0 {
		w.logger.Info("banks retrieved from cache")
		if query != nil {
			queryResults := cachedBanks.FindBanks(*query)
			return queryResults, nil
		}
		return &cachedBanks, nil
	}

	w.logger.Info("retrieving banks from provider")

	provider, exists := prov.GetProvider(providers.Paystack)
	if !exists {
		w.logger.Error("FIAT Provider does not exist - Paystack")
		return nil, fmt.Errorf("FIAT Provider does not exist")
	}

	fiatProvider, ok := provider.(*fiat.PaystackProvider)
	if !ok {
		w.logger.Error("could not resolve to FIAT Provider - Paystack")
		return nil, fmt.Errorf("could not resolve FIAT Provider")
	}

	banks, err := fiatProvider.GetBanks()
	if err != nil {
		w.logger.Error(fmt.Sprintf("Error connecting to FIAT Provider - Paystack: %v", err))
		return nil, fmt.Errorf("error connecting to FIAT Provider: %v", err)
	}

	banksCollection := models.ToBankResponseCollection(*banks)

	w.logger.Info("storing banks into cache")

	err = w.redis.StoreBankResponseCollection(ctx, "banks", banksCollection)
	if err != nil {
		w.logger.Error(fmt.Sprintf("failed to store banks into redisCache: %v", err))
		// Do not break the user's flow just because you couldn't get your own (funny) convenience service to work
		// return nil, err
	}

	/// Perform search
	if query != nil {
		queryResults := banksCollection.FindBanks(*query)
		return queryResults, nil
	}

	return &banksCollection, nil
}

func (w *WalletService) ResolveAccount(prov *providers.ProviderService, accountNumber *string, bankCode *string) (*fiat.AccountInfo, error) {

	w.logger.Info("resolving account number")

	provider, exists := prov.GetProvider(providers.Paystack)
	if !exists {
		w.logger.Error("FIAT Provider does not exist - Paystack")
		return nil, fmt.Errorf("FIAT Provider does not exist")
	}

	fiatProvider, ok := provider.(*fiat.PaystackProvider)
	if !ok {
		w.logger.Error("could not resolve to FIAT Provider - Paystack")
		return nil, fmt.Errorf("could not resolve FIAT Provider")
	}

	accountInfo, err := fiatProvider.ResolveAccount(*accountNumber, *bankCode)
	if err != nil {
		w.logger.Error(fmt.Sprintf("Error connecting to FIAT Provider - Paystack: %v", err))
		return nil, fmt.Errorf("error connecting to FIAT Provider: %v", err)
	}

	return accountInfo, nil
}

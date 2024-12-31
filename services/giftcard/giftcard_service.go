package giftcard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/giftcards"
	reloadlymodels "github.com/SwiftFiat/SwiftFiat-Backend/providers/giftcards/reloadly_models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/redis"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/sqlc-dev/pqtype"
)

type GiftcardService struct {
	store  *db.Store
	logger *logging.Logger
	redis  *redis.RedisService
	/// We may need to inject the provider service here
	/// since it's getting used in all of the functions
}

func NewGiftcardServiceWithCache(store *db.Store, logger *logging.Logger, redis *redis.RedisService) *GiftcardService {
	return &GiftcardService{
		store:  store,
		logger: logger,
		redis:  redis,
	}
}

func (g *GiftcardService) SyncGiftCards(prov *providers.ProviderService) error {
	gprov, exists := prov.GetProvider(providers.Reloadly)
	if !exists {
		return fmt.Errorf("failed to get provider: 'RELOADLY'")
	}
	reloadlyProvider, ok := gprov.(*giftcards.ReloadlyProvider)
	if !ok {
		return fmt.Errorf("failed to connect to giftcard provider")
	}
	giftCards, err := reloadlyProvider.GetAllGiftCards()
	if err != nil {
		return fmt.Errorf("failed to connect to GiftCard Provider Error: %s", err)
	}

	ctx := context.Background()

	for _, card := range giftCards {
		tx, err := g.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
		if err != nil {
			g.logger.Fatalf("Failed to start transaction: %v", err)
		}

		defer tx.Rollback()

		// Upsert Brand
		brandID, err := g.store.WithTx(tx).UpsertBrand(ctx, db.UpsertBrandParams{
			BrandID: card.Brand.BrandID,
			BrandName: sql.NullString{
				String: card.Brand.BrandName,
				Valid:  card.Brand.BrandName != "",
			},
		})
		if err != nil {
			return fmt.Errorf("failed to insert BrandID: %s", err)
		}

		// Upsert Category
		categoryID, err := g.store.WithTx(tx).UpsertCategory(ctx, db.UpsertCategoryParams{
			CategoryID: card.Category.ID,
			Name:       card.Category.Name,
		})
		if err != nil {
			return fmt.Errorf("failed to insert CategoryID: %s", err)
		}

		// Upsert Country
		countryID, err := g.store.WithTx(tx).UpsertCountry(ctx, db.UpsertCountryParams{
			IsoName: sql.NullString{String: card.Country.ISOName, Valid: card.Country.ISOName != ""},
			Name:    sql.NullString{String: card.Country.Name, Valid: card.Country.Name != ""},
			FlagUrl: sql.NullString{String: card.Country.FlagURL, Valid: card.Country.FlagURL != ""},
		})
		if err != nil {
			return fmt.Errorf("failed to insert CountryID: %s", err)
		}

		// Upsert Gift Card
		// Transform Metadata to JSONB-compatible format
		metadataJSON, err := json.Marshal(card.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadataJSON: %s", err)
		}
		giftCardID, err := g.store.WithTx(tx).UpsertGiftCard(ctx, db.UpsertGiftCardParams{
			ProductID: card.ProductID,
			ProductName: sql.NullString{
				String: card.ProductName,
				Valid:  card.ProductName != "",
			},
			DenominationType: sql.NullString{
				String: card.DenominationType,
				Valid:  card.DenominationType != "",
			},
			DiscountPercentage: sql.NullFloat64{
				Float64: card.DiscountPercentage,
				Valid:   card.DiscountPercentage != 0,
			},
			MaxRecipientDenomination: sql.NullFloat64{
				Float64: card.MaxRecipientDenomination,
				Valid:   card.MaxRecipientDenomination != 0,
			},
			MinRecipientDenomination: sql.NullFloat64{
				Float64: card.MinRecipientDenomination,
				Valid:   card.MinRecipientDenomination != 0,
			},
			MaxSenderDenomination: sql.NullFloat64{
				Float64: card.MaxSenderDenomination,
				Valid:   card.MaxSenderDenomination != 0,
			},
			MinSenderDenomination: sql.NullFloat64{
				Float64: card.MinSenderDenomination,
				Valid:   card.MinSenderDenomination != 0,
			},
			Global: sql.NullBool{
				Bool:  card.Global,
				Valid: card.Global,
			},
			Metadata: pqtype.NullRawMessage{
				RawMessage: metadataJSON,
				Valid:      metadataJSON != nil,
			},
			RecipientCurrencyCode: sql.NullString{
				String: card.RecipientCurrencyCode,
				Valid:  card.RecipientCurrencyCode != "",
			},
			SenderCurrencyCode: sql.NullString{
				String: card.SenderCurrencyCode,
				Valid:  card.SenderCurrencyCode != "",
			},
			SenderFee: sql.NullFloat64{
				Float64: card.SenderFee,
				Valid:   card.SenderFee != 0,
			},
			SenderFeePercentage: sql.NullFloat64{
				Float64: card.SenderFeePercentage,
				Valid:   card.SenderFeePercentage != 0,
			},
			SupportsPreOrder: sql.NullBool{
				Bool:  card.SupportsPreOrder,
				Valid: card.SupportsPreOrder,
			},
			BrandID: sql.NullInt64{
				Int64: int64(brandID),
				Valid: brandID != 0,
			},
			CategoryID: sql.NullInt64{
				Int64: int64(categoryID),
				Valid: categoryID != 0,
			},
			CountryID: sql.NullInt64{
				Int64: int64(countryID),
				Valid: countryID != 0,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to insert GiftCardID: %s", err)
		}

		// Upsert Logo Urls
		for _, url := range card.LogoUrls {
			err = g.store.WithTx(tx).UpsertGiftCardLogoUrl(ctx, db.UpsertGiftCardLogoUrlParams{
				GiftCardID: sql.NullInt64{
					Int64: int64(giftCardID),
					Valid: giftCardID != 0,
				},
				LogoUrl: sql.NullString{
					String: url,
					Valid:  giftCardID != 0,
				},
			})
			if err != nil {
				return fmt.Errorf("failed to insert url:%s %s", url, err)
			}
		}

		// Upsert Fixed Denominations
		for _, denomination := range card.FixedRecipientDenominations {
			err = g.store.WithTx(tx).UpsertFixedDenominations(ctx, db.UpsertFixedDenominationsParams{
				GiftCardID: sql.NullInt64{
					Int64: int64(giftCardID),
					Valid: giftCardID != 0,
				},
				Type: sql.NullString{
					String: "recipient",
					Valid:  true,
				},
				Denomination: sql.NullFloat64{
					Float64: denomination,
					Valid:   denomination != 0,
				},
			})
			if err != nil {
				return fmt.Errorf("failed to insert denomination:%f %s", denomination, err)
			}
		}

		// Upsert Fixed Denominations
		for _, denomination := range card.FixedSenderDenominations {
			err = g.store.WithTx(tx).UpsertFixedDenominations(ctx, db.UpsertFixedDenominationsParams{
				GiftCardID: sql.NullInt64{
					Int64: int64(giftCardID),
					Valid: giftCardID != 0,
				},
				Type: sql.NullString{
					String: "sender",
					Valid:  true,
				},
				Denomination: sql.NullFloat64{
					Float64: denomination,
					Valid:   denomination != 0,
				},
			})
			if err != nil {
				return fmt.Errorf("failed to insert denomination:%f %s", denomination, err)
			}
		}

		// Upsert Redeem Instructions
		err = g.store.WithTx(tx).UpsertRedeemInstructions(ctx, db.UpsertRedeemInstructionsParams{
			GiftCardID: sql.NullInt64{
				Int64: int64(giftCardID),
				Valid: giftCardID != 0,
			},
			Concise: sql.NullString{
				String: card.RedeemInstruction.Concise,
				Valid:  card.RedeemInstruction.Concise != "",
			},
			DetailedInstruction: sql.NullString{
				String: card.RedeemInstruction.Verbose,
				Valid:  card.RedeemInstruction.Verbose != "",
			},
		})
		if err != nil {
			return fmt.Errorf("failed to insert redeem instructions: %s", err)
		}

		err = tx.Commit()
		if err != nil {
			log.Fatalf("Failed to commit transaction: %v", err)
			return fmt.Errorf("failed to commit transaction: %s", err)
		}
	}

	return nil
}

func (g *GiftcardService) BuyGiftCard(prov *providers.ProviderService, trans *transaction.TransactionService, userID int64, productID int64, walletID uuid.UUID, quantity int, unitPrice int) (*reloadlymodels.GiftCardPurchaseResponse, error) {
	gprov, exists := prov.GetProvider(providers.Reloadly)
	if !exists {
		return nil, fmt.Errorf("failed to get provider: 'RELOADLY'")
	}
	reloadlyProvider, ok := gprov.(*giftcards.ReloadlyProvider)
	if !ok {
		return nil, fmt.Errorf("failed to connect to giftcard provider")
	}

	ctx := context.Background()

	tx, err := g.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
	if err != nil {
		g.logger.Fatalf("Failed to start transaction: %v", err)
	}

	defer tx.Rollback()

	// Pull user information
	userInfo, err := g.store.WithTx(tx).GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to pull user information: %s", err)
	}

	// Pull product information
	productInfo, err := g.store.WithTx(tx).FetchGiftCard(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("failed to pull product information: %s", err)
	}

	// Pull wallet information and lock it for processing
	walletInfo, err := g.store.WithTx(tx).GetWalletForUpdate(ctx, walletID)
	if err != nil {
		return nil, fmt.Errorf("failed to pull and lock wallet information: %s", err)
	}

	// Check wallet balance with up to 100% markup on the platform
	if walletInfo.Currency != productInfo.SenderCurrencyCode.String {
		return nil, fmt.Errorf("cannot proceed with purchase due to conflicting currencies: %v -> %v", walletInfo.Currency, productInfo.RecipientCurrencyCode.String)
	}

	var potentialAmount decimal.Decimal

	basePrice := decimal.NewFromInt(int64(quantity * unitPrice))
	if productInfo.SenderFeePercentage.Float64 != 0 {
		potentialAmount = basePrice.Mul(decimal.NewFromFloat(productInfo.SenderFeePercentage.Float64).Mul(decimal.NewFromInt(int64(quantity)))).Add(basePrice)
	} else {
		potentialAmount = basePrice.Add(decimal.NewFromFloat(productInfo.SenderFee.Float64).Mul(decimal.NewFromInt(int64(quantity))))
	}

	val, err := decimal.NewFromString(walletInfo.Balance.String)
	if err != nil {
		return nil, fmt.Errorf("failed to parse user's wallet balance: %v", err)
	}

	if val.LessThan(potentialAmount) {
		return nil, fmt.Errorf("insufficient balance for purchase: %v", val)
	}

	// Perform transaction
	request := reloadlymodels.GiftCardPurchaseRequest{
		ProductID:        productInfo.ProductID,
		CountryCode:      "US",
		Quantity:         float64(quantity),
		UnitPrice:        float64(unitPrice),
		CustomIdentifier: fmt.Sprintf("%v:%v", userInfo.Email, uuid.NewString()),
		SenderName:       userInfo.FirstName.String,
		RecipientEmail:   userInfo.Email,
		RecipientPhoneDetails: reloadlymodels.RecipientPhoneDetails{
			CountryCode: "NG",
			PhoneNumber: "08022491679",
		},
	}

	giftCardPurchaseResponse, err := reloadlyProvider.BuyGiftCard(&request)
	if err != nil {
		return nil, fmt.Errorf("failed to perform transaction: %s", err)
	}

	// Create GiftCardTransaction
	_, err = trans.CreateGiftCardOutflowTransactionWithTx(ctx, tx, transaction.GiftCardTransaction{
		SourceWalletID:   walletInfo.ID,
		Amount:           decimal.NewFromFloat(giftCardPurchaseResponse.Amount),
		WalletCurrency:   walletInfo.Currency,
		WalletBalance:    walletInfo.Balance.String,
		GiftCardCurrency: productInfo.SenderCurrencyCode.String,
		Description:      "giftcard-purchase",
		Type:             transaction.GiftCard,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to debit customer: %s", err)
	}

	// Commit
	err = tx.Commit()
	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %s", err)
	}

	g.logger.Info("transaction (gitftcard purchase) completed successfully", tx)

	return giftCardPurchaseResponse, nil
}

func (g *GiftcardService) GetCardInfo(prov *providers.ProviderService, transactionID string) (interface{}, error) {

	gprov, exists := prov.GetProvider(providers.Reloadly)
	if !exists {
		return nil, fmt.Errorf("failed to get provider: 'RELOADLY'")
	}
	reloadlyProvider, ok := gprov.(*giftcards.ReloadlyProvider)
	if !ok {
		return nil, fmt.Errorf("failed to connect to giftcard provider")
	}

	giftCardInfo, err := reloadlyProvider.GetCardInfo(transactionID)
	if err != nil {
		return nil, fmt.Errorf("failed to perform transaction: %s", err)
	}

	return giftCardInfo, nil
}

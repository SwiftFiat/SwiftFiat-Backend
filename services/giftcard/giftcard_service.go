package giftcard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/giftcards"
	reloadlymodels "github.com/SwiftFiat/SwiftFiat-Backend/providers/giftcards/reloadly_models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/redis"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/sqlc-dev/pqtype"
)

type GiftcardService struct {
	store  *db.Store
	logger *logging.Logger
	redis  *redis.RedisService
	config *utils.Config
	/// We may need to inject the provider service here
	/// since it's getting used in all of the functions
}

func NewGiftcardServiceWithCache(store *db.Store, logger *logging.Logger, redis *redis.RedisService, config *utils.Config) *GiftcardService {
	return &GiftcardService{
		store:  store,
		logger: logger,
		redis:  redis,
		config: config,
	}
}

func (g *GiftcardService) SyncGiftCards(prov *providers.ProviderService) error {
	g.logger.Info("Starting gift card synchronization")

	// Get provider
	gprov, exists := prov.GetProvider(providers.Reloadly)
	if !exists {
		g.logger.Error("Failed to get RELOADLY provider")
		return fmt.Errorf("failed to get provider: 'RELOADLY'")
	}

	reloadlyProvider, ok := gprov.(*giftcards.ReloadlyProvider)
	if !ok {
		g.logger.Error("Failed to cast provider to ReloadlyProvider")
		return fmt.Errorf("failed to connect to giftcard provider")
	}

	// Fetch all gift cards
	g.logger.Info("Fetching gift cards from Reloadly provider")
	giftCards, err := reloadlyProvider.GetAllGiftCards()
	if err != nil {
		g.logger.Error("Failed to get gift cards from provider", "error", err)
		return fmt.Errorf("failed to connect to GiftCard Provider Error: %s", err)
	}

	g.logger.Info("Retrieved gift cards successfully", "count", len(giftCards))
	ctx := context.Background()

	// Track progress for logging
	totalCards := len(giftCards)
	successCount := 0
	failCount := 0

	// Process each gift card in its own transaction
	for i, card := range giftCards {
		cardLogFields := map[string]interface{}{
			"productID":   card.ProductID,
			"productName": card.ProductName,
			"brandName":   card.Brand.BrandName,
			"progress":    fmt.Sprintf("%d/%d", i+1, totalCards),
		}

		g.logger.Info("Processing gift card", cardLogFields)

		// Begin transaction for this gift card
		tx, err := g.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault})
		if err != nil {
			g.logger.Error("Failed to start transaction", "error", err)
			failCount++
			return fmt.Errorf("failed to start transaction: %v", err)
		}

		// Use a function to handle the transaction scope and defer
		err = func(tx *sql.Tx) error {
			defer func() {
				if p := recover(); p != nil {
					tx.Rollback()
					g.logger.Error("Panic occurred during processing", "panic", p, "card", cardLogFields)
				}
			}()

			// Upsert Brand
			g.logger.Debug("Upserting brand", "brandID", card.Brand.BrandID, "brandName", card.Brand.BrandName)
			brandID, err := g.store.WithTx(tx).UpsertBrand(ctx, db.UpsertBrandParams{
				BrandID: card.Brand.BrandID,
				BrandName: sql.NullString{
					String: card.Brand.BrandName,
					Valid:  card.Brand.BrandName != "",
				},
			})
			if err != nil {
				g.logger.Error("Failed to upsert brand", "error", err)
				return fmt.Errorf("failed to insert BrandID: %s", err)
			}

			// Upsert Category
			g.logger.Debug("Upserting category", "categoryID", card.Category.ID, "categoryName", card.Category.Name)
			categoryID, err := g.store.WithTx(tx).UpsertCategory(ctx, db.UpsertCategoryParams{
				CategoryID: card.Category.ID,
				Name:       card.Category.Name,
			})
			if err != nil {
				g.logger.Error("Failed to upsert category", "error", err)
				return fmt.Errorf("failed to insert CategoryID: %s", err)
			}

			// Upsert Country
			g.logger.Debug("Upserting country", "countryName", card.Country.Name)
			countryID, err := g.store.WithTx(tx).UpsertCountry(ctx, db.UpsertCountryParams{
				IsoName: sql.NullString{String: card.Country.ISOName, Valid: card.Country.ISOName != ""},
				Name:    sql.NullString{String: card.Country.Name, Valid: card.Country.Name != ""},
				FlagUrl: sql.NullString{String: card.Country.FlagURL, Valid: card.Country.FlagURL != ""},
			})
			if err != nil {
				g.logger.Error("Failed to upsert country", "error", err)
				return fmt.Errorf("failed to insert CountryID: %s", err)
			}

			// Transform Metadata to JSONB-compatible format
			metadataJSON, err := json.Marshal(card.Metadata)
			if err != nil {
				g.logger.Error("Failed to marshal metadata", "error", err)
				return fmt.Errorf("failed to marshal metadataJSON: %s", err)
			}

			// Upsert Gift Card
			g.logger.Debug("Upserting gift card", "productID", card.ProductID)
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
				g.logger.Error("Failed to upsert gift card", "error", err)
				return fmt.Errorf("failed to insert GiftCardID: %s", err)
			}

			// Upsert Logo Urls
			if len(card.LogoUrls) > 0 {
				g.logger.Debug("Upserting logo URLs", "count", len(card.LogoUrls))
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
						g.logger.Error("Failed to upsert logo URL", "url", url, "error", err)
						return fmt.Errorf("failed to insert url:%s %s", url, err)
					}
				}
			}

			// Upsert Fixed Recipient Denominations
			if len(card.FixedRecipientDenominations) > 0 {
				g.logger.Debug("Upserting recipient denominations", "count", len(card.FixedRecipientDenominations))
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
						g.logger.Error("Failed to upsert recipient denomination", "value", denomination, "error", err)
						return fmt.Errorf("failed to insert denomination:%f %s", denomination, err)
					}
				}
			}

			// Upsert Fixed Sender Denominations
			if len(card.FixedSenderDenominations) > 0 {
				g.logger.Debug("Upserting sender denominations", "count", len(card.FixedSenderDenominations))
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
						g.logger.Error("Failed to upsert sender denomination", "value", denomination, "error", err)
						return fmt.Errorf("failed to insert denomination:%f %s", denomination, err)
					}
				}
			}

			// Upsert Redeem Instructions
			g.logger.Debug("Upserting redeem instructions")
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
				g.logger.Error("Failed to upsert redeem instructions", "error", err)
				return fmt.Errorf("failed to insert redeem instructions: %s", err)
			}

			return nil
		}(tx)

		// Handle the transaction result
		if err != nil {
			tx.Rollback()
			failCount++
			g.logger.Error("Transaction failed for gift card", "error", err, "card", cardLogFields)
			return fmt.Errorf("transaction failed: %s", err)
		} else {
			err = tx.Commit()
			if err != nil {
				failCount++
				g.logger.Error("Failed to commit transaction", "error", err, "card", cardLogFields)
				return fmt.Errorf("failed to commit transaction: %s", err)
			}
			successCount++
			g.logger.Info("Successfully processed gift card", cardLogFields)
		}
	}

	g.logger.Info(fmt.Sprintf("Gift card synchronization completed - Total: %d, Successful: %d, Failed: %d",
		totalCards, successCount, failCount))

	return nil
}

func (g *GiftcardService) BuyGiftCard(prov *providers.ProviderService, trans *transaction.TransactionService, userID int64, productID int64, walletID uuid.UUID, quantity int, unitPrice int) (*transaction.TransactionResponse[transaction.GiftcardMetadataResponse], error) {
	gprov, exists := prov.GetProvider(providers.Reloadly)
	if !exists {
		return nil, fmt.Errorf("failed to get provider: 'RELOADLY'")
	}
	reloadlyProvider, ok := gprov.(*giftcards.ReloadlyProvider)
	if !ok {
		return nil, fmt.Errorf("failed to connect to giftcard provider")
	}

	ctx := context.Background()

	// Pull user information
	userInfo, err := g.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to pull user information: %s", err)
	}

	// Pull product information
	productInfo, err := g.store.FetchGiftCard(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("failed to pull product information: %s", err)
	}

	// Calculate the potential amount including service fees
	var potentialAmount decimal.Decimal
	basePrice := decimal.NewFromInt(int64(quantity * unitPrice))

	g.logger.Info("base price", "basePrice", basePrice)

	// Calculate percentage-based fee if applicable
	var percentageFee decimal.Decimal
	if productInfo.SenderFeePercentage.Float64 != 0 {
		percentageValue := decimal.NewFromFloat(productInfo.SenderFeePercentage.Float64).Div(decimal.NewFromInt(100))
		percentageFee = basePrice.Mul(percentageValue)
	}

	g.logger.Info("percentage fee", "percentageFee", percentageFee)

	// Calculate flat fee
	flatFee := decimal.NewFromFloat(productInfo.SenderFee.Float64)

	g.logger.Info("flat fee", "flatFee", flatFee)

	// Sum up all components
	potentialAmount = basePrice.Add(percentageFee).Add(flatFee)

	g.logger.Info("potential amount", "potentialAmount", potentialAmount)

	if potentialAmount.LessThan(decimal.NewFromInt(0)) {
		return nil, fmt.Errorf("potential amount is less than 0")
	}

	g.logger.Info("starting giftcard outflow transaction")

	// Start transaction
	dbTx, err := g.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer dbTx.Rollback()

	// Create GiftCardTransaction
	tInfo, err := trans.CreateGiftCardOutflowTransactionWithTx(ctx, dbTx, &userInfo, transaction.GiftCardTransaction{
		/// SentAmount is still in it's potential stage, Fees etc. should be added before debit
		SourceWalletID:   walletID,
		SentAmount:       potentialAmount,
		GiftCardCurrency: productInfo.SenderCurrencyCode.String,
		Description:      "giftcard-purchase",
		Type:             transaction.GiftCard,
	})
	if err != nil {
		return nil, err
	}

	// Perform transaction
	request := reloadlymodels.GiftCardPurchaseRequest{
		ProductID:        productInfo.ProductID,
		CountryCode:      "US",
		Quantity:         float64(quantity),
		UnitPrice:        float64(unitPrice),
		CustomIdentifier: fmt.Sprintf("%v:%v", userInfo.Email, uuid.NewString()),
		SenderName:       userInfo.FirstName.String,
		RecipientEmail:   g.config.Email,
		RecipientPhoneDetails: reloadlymodels.RecipientPhoneDetails{
			CountryCode: g.config.CountryCode,
			PhoneNumber: g.config.Phone,
		},
	}

	giftCardPurchaseResponse, err := reloadlyProvider.BuyGiftCard(&request)
	if err != nil {
		return nil, fmt.Errorf("failed to perform transaction: %s", err)
	}

	updatedTransaction, err := g.store.WithTx(dbTx).UpdateGiftCardServiceTransactionID(ctx, db.UpdateGiftCardServiceTransactionIDParams{
		ServiceTransactionID: sql.NullString{
			String: fmt.Sprintf("%d", giftCardPurchaseResponse.TransactionID),
			Valid:  true,
		},
		TransactionID: tInfo.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update giftcard service transaction ID: %w", err)
	}

	tInfo.Metadata.ServiceTransactionID = updatedTransaction.ServiceTransactionID.String

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	g.logger.Info("transaction (gitftcard purchase) completed successfully", tInfo)

	return tInfo, nil
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

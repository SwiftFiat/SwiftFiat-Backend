package rapidramp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/cryptocurrency"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/fiat"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	ratemanager "github.com/SwiftFiat/SwiftFiat-Backend/services/rate_manager"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/sqlc-dev/pqtype"
)

type QRCodeService struct {
	store              *db.Store
	logger             *logging.Logger
	cryptomusProvider  *cryptocurrency.CryptomusProvider
	providerService    *providers.ProviderService
	config             *utils.Config
	rateManagerService *ratemanager.Service
}

func NewQRCodeService(
	store *db.Store,
	logger *logging.Logger,
	cryptomusProvider *cryptocurrency.CryptomusProvider,
	providerService *providers.ProviderService,
	config *utils.Config,
	rateManagerService *ratemanager.Service,
) *QRCodeService {
	return &QRCodeService{
		store:              store,
		logger:             logger,
		cryptomusProvider:  cryptomusProvider,
		providerService:    providerService,
		config:             config,
		rateManagerService: rateManagerService,
	}
}

type QrcodeTraansactionStatus string

const (
	QRTransactionStatusReceived        QrcodeTraansactionStatus = "received"
	QRTransactionStatusConverting      QrcodeTraansactionStatus = "converting"
	QRTransactionStatusConversionFail  QrcodeTraansactionStatus = "conversion_failed"
	QRTransactionStatusReadyForPayout  QrcodeTraansactionStatus = "ready_for_payout"
	QRTransactionStatusSendingToBank   QrcodeTraansactionStatus = "sending_to_bank"
	QRTransactionStatusPayoutFailed    QrcodeTraansactionStatus = "failed"
	QRTransactionStatusPayoutCompleted QrcodeTraansactionStatus = "completed"
)

// ============================================================
// QR CODE MANAGEMENT
// ============================================================

// CreateQRCode generates a new QR code for receiving crypto payments
func (s *QRCodeService) CreateQRCode(ctx context.Context, userID int64, req *CreateQRCodeRequest) (*QRCodeResponse, error) {
	s.logger.Info(fmt.Sprintf("Creating QR code for user %d", userID))

	// Get or create Cryptomus static wallet address
	cryptomusAddress, err := s.getOrCreateCryptomusAddress(ctx, userID, req.Network, req.CryptoCurrency)
	if err != nil {
		return nil, fmt.Errorf("failed to get cryptomus address: %w", err)
	}

	// Generate QR code image via Cryptomus
	qrImage, err := s.cryptomusProvider.GenerateQRCode(uuid.MustParse(cryptomusAddress.Uuid))
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to generate QR image: %v", err))
		// Continue without image - we still have the address
	}

	// Create QR code token
	token := uuid.New()
	qrCodeData := fmt.Sprintf("swiftpay://qr/%s", token.String())

	// Prepare image URL
	var imageURL *string
	if qrImage != nil {
		imageURL = &qrImage.ImageUrl
	}

	// Create QR code in database
	params := db.CreateQRCodeParams{
		UserID:              userID,
		QrType:              "payment",
		CurrencyPreference:  "NGN",
		ConversionMode:      "auto",
		Network:             req.Network,
		CryptoCurrency:      req.CryptoCurrency,
		CryptomusAddressID:  uuid.NullUUID{UUID: uuid.MustParse(cryptomusAddress.ID.String()), Valid: true},
		LinkedBankAccountID: s.uuidPtrToNullUUID(req.BankAccountID),
		QrCodeData:          qrCodeData,
		Amount:              req.Amount,
		QrCodeImageUrl:      s.stringPtrToNullString(imageURL),
		Label:               s.stringPtrToNullString(req.Label),
		Description:         s.stringPtrToNullString(req.Description),
		UsageLimit:          s.intPtrToNullInt32(req.UsageLimit),
		ExpiresAt:           s.timePtrToNullTime(req.ExpiresAt),
	}

	s.logger.Info(params)
	qrCode, err := s.store.CreateQRCode(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create QR code: %w", err)
	}

	// Get bank account info if in auto mode
	var bankInfo *BankAccountInfo
	if req.BankAccountID != nil {
		bankAccount, err := s.store.GetBankAccount(ctx, *req.BankAccountID)
		if err == nil {
			bankInfo = &BankAccountInfo{
				AccountNumber: bankAccount.AccountNumber,
				AccountName:   bankAccount.AccountName,
				BankName:      bankAccount.BankName,
			}
		}
	}

	return &QRCodeResponse{
		ID:                 qrCode.ID,
		UserID:             userID,
		Token:              qrCode.Token,
		QRCodeData:         qrCode.QrCodeData,
		QRCodeImageURL:     s.nullStringToStringPtr(qrCode.QrCodeImageUrl),
		CryptoAddress:      cryptomusAddress.Address,
		Network:            qrCode.Network,
		CryptoCurrency:     qrCode.CryptoCurrency,
		CurrencyPreference: qrCode.CurrencyPreference,
		ConversionMode:     qrCode.ConversionMode,
		Status:             qrCode.Status,
		UsageCount:         int(qrCode.UsageCount),
		UsageLimit:         s.nullInt32ToIntPtr(qrCode.UsageLimit),
		Label:              s.nullStringToStringPtr(qrCode.Label),
		BankAccount:        bankInfo,
		ExpiresAt:          s.nullTimeToTimePtr(qrCode.ExpiresAt),
		CreatedAt:          qrCode.CreatedAt,
	}, nil
}

// GetQRCodes retrieves all QR codes for a user
func (s *QRCodeService) GetQRCodes(ctx context.Context, userID int64) ([]*QRCodeResponse, error) {
	qrCodes, err := s.store.GetQRCodesByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch QR codes: %w", err)
	}

	var responses []*QRCodeResponse
	for _, qr := range qrCodes {
		// Get crypto address
		var cryptoAddress string
		if qr.CryptomusAddressID.Valid {
			addr, err := s.store.GetCryptomusAddressByID(ctx, qr.CryptomusAddressID.UUID)
			if err == nil {
				cryptoAddress = addr.Address
			}
		}

		// Get bank account info if exists
		var bankInfo *BankAccountInfo
		if qr.LinkedBankAccountID.Valid {
			bankAccount, err := s.store.GetBankAccount(ctx, qr.LinkedBankAccountID.UUID)
			if err == nil {
				bankInfo = &BankAccountInfo{
					AccountNumber: bankAccount.AccountNumber,
					AccountName:   bankAccount.AccountName,
					BankName:      bankAccount.BankName,
				}
			}
		}

		responses = append(responses, &QRCodeResponse{
			ID:                 qr.ID,
			UserID:             userID,
			Token:              qr.Token,
			QRCodeData:         qr.QrCodeData,
			QRCodeImageURL:     s.nullStringToStringPtr(qr.QrCodeImageUrl),
			CryptoAddress:      cryptoAddress,
			Network:            qr.Network,
			CryptoCurrency:     qr.CryptoCurrency,
			CurrencyPreference: qr.CurrencyPreference,
			ConversionMode:     qr.ConversionMode,
			Status:             qr.Status,
			UsageCount:         int(qr.UsageCount),
			UsageLimit:         s.nullInt32ToIntPtr(qr.UsageLimit),
			Label:              s.nullStringToStringPtr(qr.Label),
			BankAccount:        bankInfo,
			ExpiresAt:          s.nullTimeToTimePtr(qr.ExpiresAt),
			LastUsedAt:         s.nullTimeToTimePtr(qr.LastUsedAt),
			CreatedAt:          qr.CreatedAt,
		})
	}
	return responses, nil
}

// DeleteQRCode soft deletes a QR code
func (s *QRCodeService) DeleteQRCode(ctx context.Context, qrCodeID uuid.UUID, userID int64) error {
	_, err := s.store.DeleteQRCode(ctx, db.DeleteQRCodeParams{
		ID:     qrCodeID,
		UserID: userID,
	})

	if err == sql.ErrNoRows {
		return utils.ErrQRCodeNotFound
	}

	return err
}

// ============================================================
// WEBHOOK PROCESSING
// ============================================================

// ProcessCryptomusWebhook processes incoming Cryptomus webhook for QR payments
func (s *QRCodeService) ProcessCryptomusWebhook(ctx context.Context, payload *cryptocurrency.WebhookPayload) error {
	s.logger.Info(fmt.Sprintf("Processing Cryptomus webhook for order: %s", payload.OrderID))

	// Check if transaction already exists (idempotency)
	existingTx, err := s.store.GetQRTransactionByOrderID(ctx, sql.NullString{
		String: payload.OrderID,
		Valid:  true,
	})
	if err == nil && (existingTx.OrderID.String != "" || existingTx.OrderID.Valid) {
		s.logger.Info(fmt.Sprintf("Transaction already exists: %s", existingTx.ID))
		return nil // Already processed
	}

	// Find QR code by wallet address UUID
	if payload.OrderID == "" {
		return fmt.Errorf("order_id is missing in webhook")
	}

	// Get cryptomus address
	cryptomusAddress, err := s.store.GetCryptomusAddressByOrderID(ctx, payload.OrderID)
	if err != nil {
		return fmt.Errorf("cryptomus address not found: %w", err)
	}

	// Find active QR code for this address
	qrCode, err := s.findActiveQRCodeByAddress(ctx, cryptomusAddress.ID)
	if err != nil {
		return fmt.Errorf("no active QR code found for address: %w", err)
	}

	// Validate QR code status
	if qrCode.Status != "active" {
		return utils.ErrQRCodeInactive
	}

	if qrCode.ExpiresAt.Valid && qrCode.ExpiresAt.Time.Before(time.Now()) {
		return ErrQRCodeExpired
	}

	// Parse crypto amount
	cryptoAmount, err := decimal.NewFromString(payload.PaymentAmount)
	if err != nil {
		return fmt.Errorf("invalid payment amount: %w", err)
	}

	// Parse USD amount
	var cryptoAmountUSD *decimal.Decimal
	if payload.PaymentAmountUSD != "" {
		usdAmount, err := decimal.NewFromString(payload.PaymentAmountUSD)
		if err == nil {
			cryptoAmountUSD = &usdAmount
		}
	}

	// Marshal webhook data
	webhookJSON, _ := json.Marshal(payload)

	// Start transaction
	tx, err := s.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.store.WithTx(tx)

	// Create transaction record
	txRecord, err := qtx.CreateTransaction(ctx, db.CreateTransactionParams{
		Type:            string(transaction.QrCode),
		Description:     qrCode.Description,
		TransactionFlow: string(transaction.Inflow),
		Status:          string(QRTransactionStatusReceived),
	})
	if err != nil {
		return fmt.Errorf("failed to create qr-transaction record: %w", err)
	}

	txParams := db.CreateQRTransactionParams{
		QrCodeID:               qrCode.ID,
		UserID:                 qrCode.UserID,
		CryptomusTransactionID: sql.NullString{String: payload.UUID, Valid: true},
		CryptomusOrderID:       sql.NullString{String: payload.OrderID, Valid: true},
		CryptomusUuid:          sql.NullString{String: payload.UUID, Valid: true},
		CryptomusAddressID:     uuid.NullUUID{UUID: cryptomusAddress.ID, Valid: true},
		WebhookData:            pqtype.NullRawMessage{RawMessage: webhookJSON, Valid: true},
		CryptoCurrency:         payload.Currency,
		CryptoNetwork:          payload.Network,
		CryptoAmount:           cryptoAmount.String(),
		CryptoAmountUsd:        s.decimalPtrToNullString(cryptoAmountUSD),
		TransactionHash:        sql.NullString{String: payload.TxID, Valid: true},
		BankAccountID:          qrCode.LinkedBankAccountID,
		Status:                 string(QRTransactionStatusReceived),
		TransactionID:          uuid.NullUUID{UUID: txRecord.ID, Valid: true},
	}
	s.logger.Infof("creating qr tx with status as %s", txParams.Status)
	qrTx, err := qtx.CreateQRTransaction(ctx, txParams)
	if err != nil {
		return fmt.Errorf("failed to create QR transaction: %w", err)
	}

	// Update QR code usage
	_, err = qtx.UpdateQRCodeUsage(ctx, qrCode.ID)
	if err != nil {
		s.logger.Error(fmt.Sprintf("Failed to update QR code usage: %v", err))
	}

	_, err = qtx.CreateCryptoMetadata(ctx, db.CreateCryptoMetadataParams{
		TransactionID:        txRecord.ID,
		Coin:                 payload.Currency,
		SourceHash:           sql.NullString{String: payload.Sign, Valid: true},
		ReceivedAmount:       sql.NullString{String: payload.PaymentAmount, Valid: true},
		Fees:                 sql.NullString{String: "0", Valid: true},
		SentAmount:           sql.NullString{String: payload.PaymentAmount, Valid: true},
		ServiceProvider:      "cryptomus",
		ServiceTransactionID: sql.NullString{String: payload.UUID, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to create qrcode crypto metadata: %w", err)
	}

	s.logger.Info(fmt.Sprintf("QR transaction created: %s, status: received", qrTx.ID))

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logger.Infof("creating qr tx with status as %v", txParams)

	// If payment is already final (confirmed by Cryptomus), move to confirmed
	// if payload.IsFinal {
	// 	err = s.confirmQRTransaction(ctx, qrTx.ID, requiredConfirmations)
	// 	if err != nil {
	// 		s.logger.Error(fmt.Sprintf("Failed to confirm transaction: %v", err))
	// 	}
	// }

	return nil
}

// ============================================================
// TRANSACTION PROCESSING PIPELINE
// ============================================================

// ProcessReadyForConversion processes transactions ready for conversion
func (s *QRCodeService) ProcessReadyForConversion(ctx context.Context, limit int32) error {
	s.logger.Info("Processing transactions ready for conversion")

	transactions, err := s.store.GetTransactionsReadyForConversion(ctx, limit)
	if err != nil {
		return fmt.Errorf("failed to fetch transactions: %w", err)
	}

	s.logger.Infof("pending qr-txs ready for conversion: %d", len(transactions))

	for _, tx := range transactions {
		err := s.convertQRTransaction(ctx, &tx)
		if err != nil {
			s.logger.Error(fmt.Sprintf("Failed to convert transaction %s: %v", tx.ID, err))
			// Todo: use db transaction
			// Update failure
			s.store.UpdateQRTransactionFailure(ctx, db.UpdateQRTransactionFailureParams{
				ID:            tx.ID,
				FailureReason: sql.NullString{String: err.Error(), Valid: true},
				FailureStage:  sql.NullString{String: "conversion", Valid: true},
			})

			s.store.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
				ID:     tx.TransactionID.UUID,
				Status: string(QRTransactionStatusReadyForPayout),
			})
		}

		s.store.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     tx.TransactionID.UUID,
			Status: string(QRTransactionStatusReadyForPayout),
		})
	}

	return nil
}

// ProcessReadyForPayout processes transactions ready for bank payout
func (s *QRCodeService) ProcessReadyForPayout(ctx context.Context, limit int32) error {
	s.logger.Info("Processing transactions ready for payout")

	transactions, err := s.store.GetTransactionsReadyForPayout(ctx, limit)
	if err != nil {
		return fmt.Errorf("failed to fetch transactions: %w", err)
	}

	s.logger.Infof("pending qr txs ready for payout: %d", len(transactions))

	for _, tx := range transactions {
		err := s.payoutQRTransaction(ctx, &tx)
		if err != nil {
			s.logger.Error(fmt.Sprintf("Failed to payout transaction %s: %v", tx.ID, err))

			// Update failure
			s.store.UpdateQRTransactionFailure(ctx, db.UpdateQRTransactionFailureParams{
				ID:            tx.ID,
				FailureReason: sql.NullString{String: err.Error(), Valid: true},
				FailureStage:  sql.NullString{String: "payout", Valid: true},
			})

			s.store.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
				ID:     tx.TransactionID.UUID,
				Status: string(QRTransactionStatusPayoutFailed),
			})
		}
	}

	return nil
}

// RetryFailedTransactions retries failed transactions
func (s *QRCodeService) RetryFailedTransactions(ctx context.Context, limit int32) error {
	s.logger.Info("Retrying failed transactions")

	transactions, err := s.store.GetFailedQRTransactions(ctx, limit)
	if err != nil {
		return fmt.Errorf("failed to fetch failed transactions: %w", err)
	}

	for _, tx := range transactions {
		// Determine what stage to retry
		switch tx.FailureStage.String {
		case "conversion":
			// Reset to confirmed and retry conversion
			_, err := s.store.UpdateQRTransactionStatus(ctx, db.UpdateQRTransactionStatusParams{
				ID:     tx.ID,
				Status: "confirmed",
			})
			if err == nil {
				s.convertQRTransaction(ctx, &tx)
			}
		case "payout":
			// Reset to sending_to_bank and retry payout
			_, err := s.store.UpdateQRTransactionStatus(ctx, db.UpdateQRTransactionStatusParams{
				ID:     tx.ID,
				Status: "sending_to_bank",
			})
			if err == nil {
				s.payoutQRTransaction(ctx, &tx)
			}
		}
	}

	return nil
}

// payoutQRTransaction sends fiat to user's bank account
func (s *QRCodeService) payoutQRTransaction(ctx context.Context, tx *db.QrTransaction) error {
	s.logger.Info(fmt.Sprintf("Processing payout for transaction %s", tx.ID))

	// Get bank account
	if !tx.BankAccountID.Valid {
		return fmt.Errorf("no bank account linked")
	}

	bankAccount, err := s.store.GetBankAccount(ctx, tx.BankAccountID.UUID)
	if err != nil {
		return fmt.Errorf("failed to get bank account: %w", err)
	}

	// Verify bank account
	if !bankAccount.IsVerified {
		return utils.ErrBankAccountNotVerified
	}

	// Get Paystack provider
	provider, exists := s.providerService.GetProvider(providers.Paystack)
	if !exists {
		return fmt.Errorf("paystack provider not available")
	}

	paystackProvider, ok := provider.(*fiat.NombaProvider)
	if !ok {
		return fmt.Errorf("invalid paystack provider")
	}

	// Create transfer recipient if not exists
	// TODO: In production, store the recipient code in bank_accounts table
	recipient, err := paystackProvider.CreateTransferRecipient(
		bankAccount.AccountNumber,
		bankAccount.BankCode,
		bankAccount.AccountName,
	)
	if err != nil {
		return fmt.Errorf("failed to create recipient: %w", err)
	}

	// Convert net amount to kobo (NGN smallest unit)
	netAmount, _ := decimal.NewFromString(tx.NetAmount.String)
	amountInKobo := netAmount.Mul(decimal.NewFromInt(100)).IntPart()

	// Initiate transfer
	transfer, err := paystackProvider.MakeTransfer(
		recipient.RecipientCode,
		uuid.NewString(),
		"sent via Swiift",
		amountInKobo,
		bankAccount.AccountName,
	)
	if err != nil {
		return fmt.Errorf("failed to initiate transfer: %w", err)
	}

	// Marshal Paystack response
	transferJSON, _ := json.Marshal(transfer)

	// Update transaction with payout details
	_, err = s.store.UpdateQRTransactionPayoutInitiated(ctx, db.UpdateQRTransactionPayoutInitiatedParams{
		ID:                     tx.ID,
		PayoutReference:        sql.NullString{String: transfer.Reference, Valid: true},
		PayoutProvider:         sql.NullString{String: "Paystack", Valid: true},
		PayoutProviderResponse: pqtype.NullRawMessage{RawMessage: transferJSON, Valid: true},
	})

	if err != nil {
		return fmt.Errorf("failed to update payout initiated: %w", err)
	}

	// If transfer is immediately successful, mark as completed
	if transfer.Status == "success" || transfer.Status == "pending" {
		_, err = s.store.UpdateQRTransactionPayoutCompleted(ctx, tx.ID)
		if err != nil {
			s.logger.Error(fmt.Sprintf("Failed to mark payout as completed: %v", err))
		} else {
			s.logger.Info(fmt.Sprintf("Payout completed for transaction %s", tx.ID))

			// Increment user's conversion volume for VIP tracking
			if netAmount, err := decimal.NewFromString(tx.NetAmount.String); err == nil {
				if err := s.rateManagerService.IncrementUserConversionVolume(ctx, tx.UserID, netAmount); err != nil {
					s.logger.Error(fmt.Sprintf("Failed to increment conversion volume for user %d: %v", tx.UserID, err))
				}
			}
		}

		s.store.UpdateTransactionStatus(ctx, db.UpdateTransactionStatusParams{
			ID:     tx.TransactionID.UUID,
			Status: string(QRTransactionStatusPayoutCompleted),
		})
	}

	return nil
}

// ============================================================
// HELPER FUNCTIONS
// ============================================================

// convertQRTransaction converts crypto to fiat
func (s *QRCodeService) convertQRTransaction(ctx context.Context, tx *db.QrTransaction) error {
	s.logger.Info(fmt.Sprintf("Converting transaction %s", tx.ID))

	// Get QR code to determine target currency
	qrCode, err := s.store.GetQRCode(ctx, tx.QrCodeID)
	if err != nil {
		return fmt.Errorf("failed to get QR code: %w", err)
	}

	// Get conversion rate
	rate, err := s.getConversionRate(tx.CryptoCurrency, qrCode.CurrencyPreference)
	if err != nil {
		return fmt.Errorf("failed to get conversion rate: %w", err)
	}

	// Parse crypto amount
	cryptoAmount, _ := decimal.NewFromString(tx.CryptoAmount)

	// Calculate fiat amount
	fiatAmount := cryptoAmount.Mul(rate)

	// Calculate fees
	// TODO: change fees to vip rates
	conversionFee := fiatAmount.Mul(decimal.NewFromFloat(0.00)) // 1% conversion fee
	platformFee := fiatAmount.Mul(decimal.NewFromFloat(0.000))  // 0.5% platform fee
	networkFee := decimal.NewFromFloat(0)                       // Fixed NGN 100 network fee
	totalFees := conversionFee.Add(platformFee).Add(networkFee)
	netAmount := fiatAmount.Sub(totalFees)

	// Update transaction with conversion details
	_, err = s.store.UpdateQRTransactionToConverting(ctx, db.UpdateQRTransactionToConvertingParams{
		ID:             tx.ID,
		ConversionRate: sql.NullString{String: rate.String(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("failed to update to converting: %w", err)
	}

	// Mark conversion as complete
	_, err = s.store.UpdateQRTransactionConversionComplete(ctx, db.UpdateQRTransactionConversionCompleteParams{
		ID:             tx.ID,
		FiatCurrency:   sql.NullString{String: qrCode.CurrencyPreference, Valid: true},
		FiatAmount:     sql.NullString{String: fiatAmount.String(), Valid: true},
		ConversionFees: conversionFee.String(),
		PlatformFees:   platformFee.String(),
		NetworkFees:    networkFee.String(),
		TotalFees:      totalFees.String(),
		NetAmount:      sql.NullString{String: netAmount.String(), Valid: true},
	})

	if err != nil {
		return fmt.Errorf("failed to complete conversion: %w", err)
	}

	s.logger.Info(fmt.Sprintf("Transaction %s converted: %s %s -> %s %s",
		tx.ID, cryptoAmount, tx.CryptoCurrency, netAmount, qrCode.CurrencyPreference))

	return nil
}

// getConversionRate gets crypto to fiat conversion rate
func (s *QRCodeService) getConversionRate(cryptoCurrency, fiatCurrency string) (decimal.Decimal, error) {
	// First get crypto to USD rate via Cryptomus
	usdRateStr, err := s.cryptomusProvider.GetUSDRate(cryptoCurrency)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to get USD rate: %w", err)
	}

	cryptoToUSD, _ := decimal.NewFromString(usdRateStr)

	// If target is USD, return directly
	if fiatCurrency == "USD" {
		return cryptoToUSD, nil
	}

	// If target is NGN, get USD to NGN rate
	// TODO: Using a simplified fixed rate - in production, use live API
	if fiatCurrency == "NGN" {
		usdToNGN := decimal.NewFromFloat(1550.0) // Example rate
		return cryptoToUSD.Mul(usdToNGN), nil
	}

	return decimal.Zero, fmt.Errorf("unsupported fiat currency: %s", fiatCurrency)
}

// confirmQRTransaction confirms a QR transaction after blockchain confirmations
// func (s *QRCodeService) confirmQRTransaction(ctx context.Context, txID uuid.UUID, confirmations int) error {
// 	_, err := s.store.UpdateQRTransactionConfirmation(ctx, db.UpdateQRTransactionConfirmationParams{
// 		ID:                 txID,
// 		ConfirmationBlocks: sql.NullInt32{Int32: int32(confirmations), Valid: true},
// 	})

// 	if err != nil {
// 		return fmt.Errorf("failed to update confirmation: %w", err)
// 	}

// 	s.logger.Info(fmt.Sprintf("Transaction %s confirmed", txID))
// 	return nil
// }

// getRequiredConfirmations returns required confirmations based on network
// func (s *QRCodeService) getRequiredConfirmations(network string) int {
// 	confirmations := map[string]int{
// 		"tron":     1,
// 		"ethereum": 1,
// 		"bsc":      1,
// 		"bitcoin":  1,
// 		// add others
// 	}

// 	if conf, exists := confirmations[network]; exists {
// 		return conf
// 	}

// 	return 1
// }

// findActiveQRCodeByAddress finds an active QR code for a given address
func (s *QRCodeService) findActiveQRCodeByAddress(ctx context.Context, addressID uuid.UUID) (*db.QrCode, error) {
	// This would need a custom query - for now, simplified
	qrCodes, err := s.store.GetQRCodesByCryptomusAddress(ctx, uuid.NullUUID{UUID: addressID, Valid: true})
	if err != nil || len(qrCodes) == 0 {
		return nil, fmt.Errorf("no QR code found")
	}

	// Return first active one
	for _, qr := range qrCodes {
		if qr.Status == "active" {
			return &qr, nil
		}
	}

	return &qrCodes[0], nil
}

// getOrCreateCryptomusAddress gets existing or creates new Cryptomus address
func (s *QRCodeService) getOrCreateCryptomusAddress(ctx context.Context, userID int64, network, currency string) (*db.CryptomusAddress, error) {
	// Try to find existing address
	existingAddresses, err := s.store.ListCryptomusAddressesByCurrency(ctx, currency)

	if err == nil && len(existingAddresses) > 0 {
		return &existingAddresses[0], nil
	}

	// Create new address via Cryptomus
	orderID := fmt.Sprintf("qr_%d_%s_%s_%d", userID, network, currency, time.Now().Unix())
	callbackURL := fmt.Sprintf("%s/%s", s.config.SwiftBaseUrl, "crypto/cryptomus/webhook")
	staticWallet, err := s.cryptomusProvider.CreateStaticWallet(&cryptocurrency.StaticWalletRequest{
		Currency:    currency,
		Network:     network,
		OrderId:     orderID,
		UrlCallback: callbackURL,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create cryptomus wallet: %w", err)
	}

	address, err := s.store.UpsertCryptomusAddress(ctx, db.UpsertCryptomusAddressParams{
		CustomerID:  sql.NullInt64{Int64: userID, Valid: true},
		WalletUuid:  staticWallet.WalletUUID,
		Uuid:        staticWallet.UUID,
		Address:     staticWallet.Address,
		Network:     staticWallet.Network,
		Currency:    staticWallet.Currency,
		OrderID:     orderID,
		PaymentUrl:  sql.NullString{String: staticWallet.Url, Valid: true},
		CallbackUrl: sql.NullString{String: callbackURL, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to store cryptomus address: %w", err)
	}

	return &address, nil
}

// Conversion helper functions
func (s *QRCodeService) uuidPtrToNullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{Valid: false}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

func (s *QRCodeService) stringPtrToNullString(str *string) sql.NullString {
	if str == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: *str, Valid: true}
}

func (s *QRCodeService) decimalPtrToNullString(d *decimal.Decimal) sql.NullString {
	if d == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: d.String(), Valid: true}
}

func (s *QRCodeService) intPtrToNullInt32(i *int) sql.NullInt32 {
	if i == nil {
		return sql.NullInt32{Valid: false}
	}
	return sql.NullInt32{Int32: int32(*i), Valid: true}
}

func (s *QRCodeService) timePtrToNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{Valid: false}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func (s *QRCodeService) nullStringToStringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}

func (s *QRCodeService) nullInt32ToIntPtr(ni sql.NullInt32) *int {
	if !ni.Valid {
		return nil
	}
	i := int(ni.Int32)
	return &i
}

func (s *QRCodeService) nullTimeToTimePtr(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	return &nt.Time
}

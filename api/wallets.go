package api

/// Transaction Types

// transfer
// deposit
// swap
// giftcard
// airtime

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/fiat"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Wallet struct {
	server             *Server
	walletService      *wallet.WalletService
	currencyService    *currency.CurrencyService
	transactionService *transaction.TransactionService
}

func (w Wallet) router(server *Server) {
	w.server = server
	w.walletService = wallet.NewWalletServiceWithCache(w.server.queries, w.server.logger, w.server.redis)
	w.currencyService = currency.NewCurrencyService(w.server.queries, w.server.logger)
	w.transactionService = transaction.NewTransactionService(
		w.server.queries,
		w.currencyService,
		w.walletService,
		w.server.logger,
	)

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := server.router.Group("/api/v1/wallets")
	serverGroupV1.GET("", w.server.authMiddleware.AuthenticatedMiddleware(), w.getUserWallets)
	serverGroupV1.GET("transactions", w.server.authMiddleware.AuthenticatedMiddleware(), w.getTransactions)
	// serverGroupV1.GET("transactions/:id", AuthenticatedMiddleware(), w.getSingleTransaction)
	serverGroupV1.POST("transfer", w.server.authMiddleware.AuthenticatedMiddleware(), w.walletTransfer)
	serverGroupV1.POST("swap", w.server.authMiddleware.AuthenticatedMiddleware(), w.swap)
	serverGroupV1.GET("banks", w.server.authMiddleware.AuthenticatedMiddleware(), w.banks)
	serverGroupV1.GET("resolve-bank-account", w.server.authMiddleware.AuthenticatedMiddleware(), w.resolveBankAccount)
	serverGroupV1.GET("resolve-user-tag", w.server.authMiddleware.AuthenticatedMiddleware(), w.resolveUserTag)
	serverGroupV1.GET("beneficiaries", w.server.authMiddleware.AuthenticatedMiddleware(), w.getBeneficiaries)
	serverGroupV1.POST("withdraw", w.server.authMiddleware.AuthenticatedMiddleware(), w.fiatTransfer)
	serverGroupV1.GET("transaction-fee", w.server.authMiddleware.AuthenticatedMiddleware(), w.getTransactionFee)

	serverGroupV1Admin := server.router.Group("/api/admin/v1/wallets")
	serverGroupV1Admin.POST("transaction-fee", w.server.authMiddleware.AuthenticatedMiddleware(), w.createTransactionFee)
	serverGroupV1Admin.GET("transaction-fee", w.server.authMiddleware.AuthenticatedMiddleware(), w.getTransactionFee)

}

func (w *Wallet) getUserWallets(ctx *gin.Context) {
	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	accounts, err := w.server.queries.GetWalletByCustomerID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNoWallet))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("User Wallets Fetched Successfully", models.ToWalletCollectionResponse(&accounts)))
}

// func (w *Wallet) getSingleTransaction(ctx *gin.Context) {
// 	transactionId := ctx.Param("id")

// 	// Fetch user details
// 	activeUser, err := utils.GetActiveUser(ctx)
// 	if err != nil {
// 		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
// 		return
// 	}

// 	transactionUUID, err := uuid.Parse(transactionId)
// 	if err != nil {
// 		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidTransactionID))
// 		return
// 	}

// 	params := db.GetWalletTransactionParams{
// 		ID:         transactionUUID,
// 		CustomerID: activeUser.UserID,
// 	}

// 	transaction, err := w.server.queries.GetWalletTransaction(ctx, params)
// 	if err == sql.ErrNoRows {
// 		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidTransactionID))
// 		return
// 	} else if err != nil {
// 		w.server.logger.Error(err)
// 		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
// 		return
// 	}

// 	ctx.JSON(http.StatusOK, basemodels.NewSuccess("User Wallet Transaction Fetched Successfully", transaction))
// }

func (w *Wallet) getTransactions(ctx *gin.Context) {
	/// Pagination
	cursor := ctx.Query("cursor")

	var timestampStr string
	var uuidStr string
	var transactionTime time.Time
	var uuidValue uuid.UUID

	// Fetch user details
	// activeUser, err := utils.GetActiveUser(ctx)
	// if err != nil {
	// 	ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
	// 	return
	// }

	if cursor != "" {
		unescaped, err := url.QueryUnescape(cursor)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid cursor format"))
			return
		}

		// Split cursor into timestamp and UUID
		parts := strings.Split(unescaped, "_")
		if len(parts) != 2 {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid cursor format"))
			return
		}
		timestampStr = parts[0]
		uuidStr = parts[1]

		// Preprocess the timestamp to fix the timezone part if necessary
		if strings.HasSuffix(timestampStr, " 00") {
			timestampStr = strings.Replace(timestampStr, " 00", "+00:00", 1)
		}

		// Parse the timestamp
		postgresLayout := "2006-01-02 15:04:05.999999-07:00"
		transactionTime, err = time.Parse(postgresLayout, timestampStr)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(fmt.Sprintf("Error parsing timestamp: %v", err)))
			return
		}

		// Extract the UUID
		uuidValue, err = uuid.Parse(uuidStr)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(fmt.Sprintf("Error parsing UUID: %v", err)))
			return
		}
	}

	w.server.logger.Debug(transactionTime)
	w.server.logger.Debug(uuidValue)

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	params := db.GetTransactionsByUserIDParams{
		UserID: sql.NullInt64{
			Int64: activeUser.UserID,
			Valid: true,
		},
	}

	// params := db.ListWalletTransactionsByUserIDParams{
	// 	CustomerID: activeUser.UserID,
	// 	PageLimit:  5,
	// 	TransactionCreated: sql.NullTime{
	// 		Time:  transactionTime,
	// 		Valid: cursor != "",
	// 	},
	// 	TransactionID: uuid.NullUUID{
	// 		UUID:  uuidValue,
	// 		Valid: cursor != "",
	// 	},
	// }

	transactions, err := w.server.queries.GetTransactionsByUserID(ctx, params)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNoWallet))
		return
	} else if err != nil {
		w.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("User Wallet Transactions Fetched Successfully", transactions))

}

func (w *Wallet) walletTransfer(ctx *gin.Context) {

	/// Active USER must OWN source wallet
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// Transfer Type -> Wallet -> Withdrawal -> Swap
	// Call the appropriate walletService handler based on type

	// Observe request
	request := struct {
		FromAccountID      string  `json:"source_account"`
		ToAccountID        string  `json:"destination_account"`
		Amount             float64 `json:"amount"`
		DestinationUserTag string  `json:"target_user_tag"`
		Description        string  `json:"description"`
		Pin                string  `json:"pin"`
	}{}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidTransactionInput))
		return
	}

	dbUserValue, err := w.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err != nil {
		if err == sql.ErrNoRows {
			ctx.JSON(http.StatusUnauthorized, basemodels.NewError("user does not exist"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	if err = utils.VerifyHashValue(request.Pin, dbUserValue.HashedPin.String); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidTransactionPIN))
		return
	}

	sourceAccount, err := uuid.Parse(request.FromAccountID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("source account seems to be wrong"))
		return
	}

	destinationAccount, err := uuid.Parse(request.ToAccountID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("destination account seems to be wrong"))
		return
	}

	if sourceAccount == destinationAccount {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("source and destination cannot be same"))
		return
	}

	amount := decimal.NewFromFloat(request.Amount)

	tparams := transaction.IntraTransaction{
		FromAccountID: sourceAccount,
		ToAccountID:   destinationAccount,
		SentAmount:    amount,
		// ReceivedAmount: to be set after rate is decided
		UserTag:     request.Description,
		Description: request.Description,
		Type:        transaction.Transfer,
	}

	tObj, err := w.transactionService.CreateWalletTransaction(ctx, tparams, &activeUser)
	if err != nil {
		w.server.logger.Error(err)
		if wallError, ok := err.(*wallet.WalletError); ok {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(wallError.ErrorOut()))
			return
		}
		if currError, ok := err.(*currency.CurrencyError); ok {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(currError.ErrorOut()))
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Transaction Created Successfully", tObj))
}

func (w *Wallet) swap(ctx *gin.Context) {

	/// Active USER must OWN source wallet
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// Observe request
	request := struct {
		FromAccountID string  `json:"source_account"`
		ToAccountID   string  `json:"destination_account"`
		Amount        float64 `json:"amount"`
		Description   string  `json:"description"`
		Pin           string  `json:"pin"`
	}{}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidTransactionInput))
		return
	}

	dbUserValue, err := w.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err != nil {
		if err == sql.ErrNoRows {
			ctx.JSON(http.StatusUnauthorized, basemodels.NewError("user does not exist"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	if err = utils.VerifyHashValue(request.Pin, dbUserValue.HashedPin.String); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidTransactionPIN))
		return
	}

	sourceAccount, err := uuid.Parse(request.FromAccountID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("source account seems to be wrong"))
		return
	}

	destinationAccount, err := uuid.Parse(request.ToAccountID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("destination account seems to be wrong"))
		return
	}

	if sourceAccount == destinationAccount {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("source and destination cannot be same"))
		return
	}

	amount := decimal.NewFromFloat(request.Amount)

	tparams := transaction.IntraTransaction{
		FromAccountID: sourceAccount,
		ToAccountID:   destinationAccount,
		SentAmount:    amount,
		// ReceivedAmount: to be set after rate is decided
		Description: request.Description,
		Type:        transaction.Swap,
	}

	tObj, err := w.transactionService.CreateWalletTransaction(ctx, tparams, &activeUser)
	if err != nil {
		w.server.logger.Error(fmt.Errorf("failed to create wallet transaction: %s", err))
		if wallError, ok := err.(*wallet.WalletError); ok {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(wallError.ErrorOut()))
			return
		}
		if currError, ok := err.(*currency.CurrencyError); ok {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(currError.ErrorOut()))
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Swapped Successfully", tObj))
}

func (w *Wallet) banks(ctx *gin.Context) {
	query := ctx.Query("query")

	banks, err := w.walletService.GetFiatBanks(w.server.provider, &query)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("fetched banks successfully", banks))
}

func (w *Wallet) resolveBankAccount(ctx *gin.Context) {
	accountNumber := ctx.Query("accountNumber")
	bankCode := ctx.Query("bankCode")

	if bankCode == "" || accountNumber == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter valid bankCode and accountNumber"))
		return
	}

	userInfo, err := w.walletService.ResolveAccount(w.server.provider, &accountNumber, &bankCode)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("account resolved successfully", models.ToAccountInfoResponse(userInfo)))
}

func (w *Wallet) resolveUserTag(ctx *gin.Context) {
	userTag := ctx.Query("userTag")
	curr := ctx.Query("currency")

	if userTag == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter valid userTag"))
		return
	}

	if curr == "" || currency.IsCurrencyInvalid(curr) {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter valid currency (USD | NGN | EUR)"))
		return
	}

	tagInfo, err := w.walletService.ResolveTag(ctx, userTag, curr)
	if err != nil {
		if wallError, ok := err.(*wallet.WalletError); ok {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(wallError.ErrorOut()))
			return
		}

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("tag resolved successfully", models.ToTagResolveResponse(*tagInfo)))
}

func (w *Wallet) getBeneficiaries(ctx *gin.Context) {
	/// Active USER must OWN source wallet
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	beneficiaries, err := w.server.queries.GetBeneficiariesByUserID(ctx, sql.NullInt64{
		Int64: activeUser.UserID,
		Valid: true,
	})
	if err != nil {
		w.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("beneficiaries retrieved successfully", models.ToBeneficiaryResponseCollection(beneficiaries)))
}

func (w *Wallet) fiatTransfer(ctx *gin.Context) {

	request := struct {
		Name            string `json:"name" binding:"required"`
		AccountNumber   string `json:"account_number" binding:"required"`
		BankCode        string `json:"bank_code" binding:"required"`
		WalletID        string `json:"wallet_id" binding:"required"`
		Amount          int64  `json:"amount" binding:"required"`
		Pin             string `json:"pin" binding:"required"`
		SaveBeneficiary bool   `json:"save_beneficiary,omitempty"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		w.server.logger.Error(err)
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please check your entered details: 'name', 'account_numner', 'bank_code', 'wallet_id', 'amount' and 'pin'"))
		return
	}

	/// Active USER must OWN source wallet
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	provider, exists := w.server.provider.GetProvider(providers.Paystack)
	if !exists {
		w.server.logger.Error("FIAT Provider does not exist - Paystack")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	fiatProvider, ok := provider.(*fiat.PaystackProvider)
	if !ok {
		w.server.logger.Error("could not resolve to FIAT Provider - Paystack")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	walletUUID, err := uuid.Parse(request.WalletID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("failed to parse wallet ID, please use correct wallet"))
		return
	}

	dbUserValue, err := w.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err != nil {
		if err == sql.ErrNoRows {
			ctx.JSON(http.StatusUnauthorized, basemodels.NewError("user does not exist"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	if err = utils.VerifyHashValue(request.Pin, dbUserValue.HashedPin.String); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidTransactionPIN))
		return
	}

	w.server.logger.Info("starting fiat withrawal transaction")

	// Start transaction
	dbTx, err := w.server.queries.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		w.server.logger.Error(fmt.Errorf("failed to begin transaction: %s", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}
	defer dbTx.Rollback()

	// Create Withdrawal Transaction
	transactionInfo, err := w.transactionService.CreateFiatOutflowTransactionWithTx(ctx, dbTx, &dbUserValue, transaction.FiatTransaction{
		SourceWalletID:             walletUUID,
		DestinationAccountName:     request.Name,
		DestinationAccountNumber:   request.AccountNumber,
		DestinationAccountBankCode: request.BankCode,
		DestinationAccountCurrency: "NGN",
		Description:                "withdrawal-from-swift",
		Type:                       transaction.Withdrawal,
		SentAmount:                 decimal.NewFromInt(request.Amount),
	})
	if err != nil {
		w.server.logger.Error(fmt.Errorf("failed to debit customer: %s", err))
		if wallError, ok := err.(*wallet.WalletError); ok {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(wallError.ErrorOut()))
			return
		}
		if currError, ok := err.(*currency.CurrencyError); ok {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(currError.ErrorOut()))
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	recipientInfo, err := fiatProvider.CreateTransferRecipient(request.AccountNumber, request.BankCode, request.Name)
	if err != nil {
		w.server.logger.Error(fmt.Errorf("failed to perform transaction: %s", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	parsedAmount, err := decimal.NewFromString(transactionInfo.Metadata.SentAmount)
	if err != nil {
		w.server.logger.Error(fmt.Errorf("failed to parse amount from transaction: %s", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Convert transaction amount back to smallest denom of source wallet as required by FiatProvier - Paystack
	paystackAmount := parsedAmount.Mul(decimal.NewFromInt(100)).BigInt().Int64()
	if paystackAmount == 0 {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("cannot withdraw 0 amount"))
		return
	}

	_, err = fiatProvider.MakeTransfer(recipientInfo.RecipientCode, paystackAmount, request.Name)
	if err != nil {
		w.server.logger.Error(fmt.Errorf("failed to perform transaction: %s", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Try to save beneficiary
	_, err = w.server.queries.CreateBeneficiary(ctx, db.CreateBeneficiaryParams{
		UserID: sql.NullInt64{
			Int64: activeUser.UserID,
			Valid: true,
		},
		AccountNumber:   request.AccountNumber,
		BeneficiaryName: request.Name,
		BankCode:        request.BankCode,
	})

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		w.server.logger.Error(fmt.Errorf("failed to perform transaction: %s", err))
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	w.server.logger.Info("fiat withdrawal transaction completed successfully", transactionInfo)

	w.server.logger.Info("transaction (withdraw) completed successfully")
	var savedBeneficiary bool = true

	if err != nil {
		w.server.logger.Error(err)
		savedBeneficiary = false
	}

	response := struct {
		TransactionInfo  transaction.TransactionResponse[transaction.FiatWithdrawalMetadataResponse] `json:"transaction"`
		SavedBeneficiary bool                                                                        `json:"saved_beneficiary"`
	}{
		TransactionInfo:  *transactionInfo,
		SavedBeneficiary: savedBeneficiary,
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("transfer successful", response))
}

func (w *Wallet) createTransactionFee(ctx *gin.Context) {
	var request transaction.CreateTransactionFeeRequest

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		w.server.logger.Error(err)
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter valid transaction type, fee percentage and max fee"))
		return
	}

	if !transaction.IsTransactionTypeValid(transaction.TransactionType(request.TransactionType)) {
		w.server.logger.Error("invalid transaction type")
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please enter valid transaction type (Transfer | Withdrawal | Deposit | Swap | GiftCard | Airtime)"))
		return
	}

	feeInfo, err := w.transactionService.CreateTransactionFee(ctx, request)
	if err != nil {
		if wallError, ok := err.(*wallet.WalletError); ok {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(wallError.ErrorOut()))
			return
		}

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Fee Created Successfully", feeInfo))
}

func (w *Wallet) getTransactionFee(ctx *gin.Context) {
	transactionType := ctx.Query("type")

	if !transaction.IsTransactionTypeValid(transaction.TransactionType(transactionType)) {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("please pass a valid transaction type (Transfer | Withdrawal | Deposit | Swap | GiftCard | Airtime)"))
		return
	}

	feeInfo, err := w.transactionService.GetTransactionFee(ctx, transaction.TransactionType(transactionType))
	if err != nil {
		if wallError, ok := err.(*wallet.WalletError); ok {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(wallError.ErrorOut()))
			return
		}

		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Fee Fetched Successfully", feeInfo))
}

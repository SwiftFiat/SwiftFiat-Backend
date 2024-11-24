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
	"strings"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
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
	w.walletService = wallet.NewWalletService(w.server.queries, w.server.logger)
	w.currencyService = currency.NewCurrencyService(w.server.queries, w.server.logger)
	w.transactionService = transaction.NewTransactionService(
		w.server.queries,
		w.currencyService,
		w.walletService,
		w.server.logger,
	)

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := server.router.Group("/api/v1/wallets")
	serverGroupV1.GET("", AuthenticatedMiddleware(), w.getUserWallets)
	serverGroupV1.GET("transactions", AuthenticatedMiddleware(), w.getTransactions)
	serverGroupV1.GET("transactions/:id", AuthenticatedMiddleware(), w.getSingleTransaction)
	serverGroupV1.POST("transfer", AuthenticatedMiddleware(), w.walletTransfer)
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

func (w *Wallet) getSingleTransaction(ctx *gin.Context) {
	transactionId := ctx.Param("id")

	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	transactionUUID, err := uuid.Parse(transactionId)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidTransactionID))
		return
	}

	params := db.GetWalletTransactionParams{
		ID:         transactionUUID,
		CustomerID: activeUser.UserID,
	}

	transaction, err := w.server.queries.GetWalletTransaction(ctx, params)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidTransactionID))
		return
	} else if err != nil {
		w.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("User Wallet Transaction Fetched Successfully", transaction))
}

func (w *Wallet) getTransactions(ctx *gin.Context) {
	/// Pagination
	cursor := ctx.Query("cursor")

	var timestampStr string
	var uuidStr string
	var transactionTime time.Time
	var uuidValue uuid.UUID

	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if cursor != "" {
		// Split cursor into timestamp and UUID
		parts := strings.Split(cursor, "_")
		if len(parts) != 2 {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid cursor format"))
			return
		}
		timestampStr = parts[0]
		uuidStr = parts[1]

		// Parse the timestamp
		postgresLayout := "2006-01-02 15:04:05.999999-07"
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

	params := db.ListWalletTransactionsByUserIDParams{
		CustomerID: activeUser.UserID,
		PageLimit:  5,
		TransactionCreated: sql.NullTime{
			Time:  transactionTime,
			Valid: cursor != "",
		},
		TransactionID: uuid.NullUUID{
			UUID:  uuidValue,
			Valid: cursor != "",
		},
	}

	transactions, err := w.server.queries.ListWalletTransactionsByUserID(ctx, params)
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
		FromAccountID      string `json:"source_account"`
		ToAccountID        string `json:"destination_account"`
		Amount             int32  `json:"amount"`
		DestinationUserTag string `json:"target_user_tag"`
		Type               string `json:"type"`
		Description        string `json:"description"`
	}{}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidTransactionInput))
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

	amount := decimal.NewFromInt32(request.Amount)

	tparams := transaction.Transaction{
		FromAccountID: sourceAccount,
		ToAccountID:   destinationAccount,
		Amount:        amount,
		UserTag:       request.DestinationUserTag,
		Description:   request.Description,
		Type:          request.Type,
	}

	tObj, err := w.transactionService.CreateTransaction(ctx, tparams, &activeUser)
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

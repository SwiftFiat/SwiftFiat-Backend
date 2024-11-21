package api

import (
	"database/sql"
	"net/http"

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
	"github.com/lib/pq"
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
	serverGroupV1.POST("create", AuthenticatedMiddleware(), w.createWallet)
	serverGroupV1.GET("", AuthenticatedMiddleware(), w.getUserWallets)
	serverGroupV1.GET("transactions", AuthenticatedMiddleware(), w.getTransactions)
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

func (w *Wallet) createWallet(ctx *gin.Context) {
	// Observe request
	request := struct {
		Currency string `json:"currency" binding:"required"`
		Type     string `json:"type"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidWalletInput))
		return
	}

	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	// Validate currency is supported
	supportedCurrencies := []string{"NGN", "USD", "EUR"}
	currencyValid := false
	for _, c := range supportedCurrencies {
		if request.Currency == c {
			currencyValid = true
			break
		}
	}
	if !currencyValid {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.CurrencyNotSupported))
		return
	}

	// Set wallet type, defaulting to "personal" if not specified
	walletType := "personal"
	if request.Type != "" {
		validTypes := []string{"personal", "business", "savings", "checking"}
		typeValid := false
		for _, t := range validTypes {
			if request.Type == t {
				typeValid = true
				break
			}
		}
		if !typeValid {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidWalletInput))
			return
		}
		walletType = request.Type
	}

	currency := request.Currency

	param := db.CreateWalletParams{
		CustomerID: activeUser.UserID,
		Type:       walletType,
		Currency:   currency,
		Balance: sql.NullString{
			String: "0",
			Valid:  true,
		},
	}

	account, err := w.server.queries.CreateWallet(ctx, param)
	if err != nil {
		if err, ok := err.(*pq.Error); ok && err.Code == db.DuplicateEntry {
			ctx.JSON(http.StatusConflict, basemodels.NewError(apistrings.DuplicateWallet))
			return
		} else if err == sql.ErrNoRows {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNoWallet))
			return
		} else {
			w.server.logger.Error("DB Error", err)
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
			return
		}
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("User Wallet Created Successfully", models.ToWalletResponse(&account)))
}

func (w *Wallet) getTransactions(ctx *gin.Context) {
	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	params := db.ListWalletTransactionsByUserIDParams{
		CustomerID: activeUser.UserID,
		Limit:      10,
	}

	transactions, err := w.server.queries.ListWalletTransactionsByUserID(ctx, params)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNoWallet))
		return
	} else if err != nil {
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

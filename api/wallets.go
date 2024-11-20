package api

import (
	"database/sql"
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type Wallet struct {
	server             *Server
	walletService      *wallet.WalletService
	transactionService *transaction.TransactionService
}

func (w Wallet) router(server *Server) {
	w.server = server
	w.walletService = wallet.NewWalletService(w.server.queries, w.server.logger)
	w.transactionService = transaction.NewTransactionService(
		w.server.queries,
		nil,
		w.walletService,
		w.server.logger,
	)

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := server.router.Group("/api/v1/wallets")
	serverGroupV1.POST("create", AuthenticatedMiddleware(), w.createWallet)
	serverGroupV1.GET("", AuthenticatedMiddleware(), w.getUserWallets)
	serverGroupV1.GET("transactions", AuthenticatedMiddleware(), w.getTransactions)
	serverGroupV1.POST("transactions", AuthenticatedMiddleware(), w.insertTransactions)
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

func (w *Wallet) insertTransactions(ctx *gin.Context) {

	params := db.CreateWalletTransactionParams{
		Type:          "credit",
		Amount:        "200",
		Currency:      "NGN",
		FromAccountID: uuid.NullUUID{},
		ToAccountID:   uuid.NullUUID{},
		Description:   sql.NullString{},
	}

	transactions, err := w.server.queries.CreateWalletTransaction(ctx, params)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNoWallet))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Transaction Created Successfully", transactions))
}

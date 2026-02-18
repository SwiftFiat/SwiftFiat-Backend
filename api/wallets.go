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
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Wallet struct {
	server             *Server
	walletService      *wallet.WalletService
	currencyService    *currency.CurrencyService
	transactionService *transaction.TransactionService
	notifr             *service.Notification
	audit              *audit.Service
	pushService        *service.PushNotificationService
}

func (w Wallet) router(server *Server) {
	w.server = server
	w.walletService = server.walletService
	w.currencyService = server.currencyService
	w.notifr = server.inAppnotificationService
	w.transactionService = server.transactionService
	w.audit = server.auditService
	w.pushService = server.pushNotification

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := server.router.Group("/api/v1/wallets")
	serverGroupV1.GET("", w.server.authMiddleware.AuthenticatedMiddleware(), w.getUserWallets)
	serverGroupV1.GET("transactions", w.server.authMiddleware.AuthenticatedMiddleware(), w.getTransactions)
	serverGroupV1.GET("transactions-cursor", w.server.authMiddleware.AuthenticatedMiddleware(), w.getTransactionsCursor)
	serverGroupV1.GET("transactions/:id", w.server.authMiddleware.AuthenticatedMiddleware(), w.getTransaction)
	serverGroupV1.POST("transfer", w.server.authMiddleware.AuthenticatedMiddleware(), w.walletTransfer)
	serverGroupV1.GET("banks", w.server.authMiddleware.AuthenticatedMiddleware(), w.banks)
	serverGroupV1.GET("resolve-bank-account", w.server.authMiddleware.AuthenticatedMiddleware(), w.resolveBankAccount)
	serverGroupV1.GET("resolve-user-tag", w.server.authMiddleware.AuthenticatedMiddleware(), w.resolveUserTag)
	serverGroupV1.GET("beneficiaries", w.server.authMiddleware.AuthenticatedMiddleware(), w.getBeneficiaries)
	serverGroupV1.POST("withdraw", w.server.authMiddleware.AuthenticatedMiddleware(), w.fiatTransfer)
	serverGroupV1.GET("transaction-fee", w.server.authMiddleware.AuthenticatedMiddleware(), w.getTransactionFee)
	serverGroupV1.POST("transaction-fee", w.server.authMiddleware.AuthenticatedMiddleware(), w.createTransactionFee)
	serverGroupV1.PUT("add-to-wallet-balance", w.server.authMiddleware.AuthenticatedMiddleware(), w.updateWalletBalance)

}

type UpdateWalletBalanceRequest struct {
	WalletID string  `json:"wallet_id" binding:"required"`
	Amount   float64 `json:"amount" binding:"required"`
	Currency string  `json:"currency" binding:"required"`
	UserID   int64   `json:"user_id" binding:"required"`
}

// updateWalletBalance godoc
// @Summary      Update Wallet Balance (Admin Only)
// @Description  Adds a specified amount to a user's wallet balance. This endpoint is intended for administrative use only.
// @Tags         Wallets
// @Accept       json
// @Produce      json
// @Param        request        body      UpdateWalletBalanceRequest  true  "Request Body"
// @Security 	 BearerAuth
// @Success      200            {object}  models.WalletResponse
// @Failure      400            {object}  basemodels.ErrorResponse
// @Failure      401            {object}  basemodels.ErrorResponse
// @Failure      500            {object}  basemodels.ErrorResponse
// @Router       /api/v1/wallets/add-to-wallet-balance [put]
func (w *Wallet) updateWalletBalance(ctx *gin.Context) {

	var request UpdateWalletBalanceRequest
	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(401, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	user, err := w.server.queries.GetUserByID(ctx, request.UserID)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	wallet, err := w.server.queries.GetWalletByCurrencyForUpdate(ctx, db.GetWalletByCurrencyForUpdateParams{
		CustomerID: user.ID,
		Currency:   request.Currency,
	})
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNoWallet))
		return
	}

	response, err := w.server.queries.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		ID: wallet.ID,
		Amount: sql.NullString{
			String: fmt.Sprintf("%f", request.Amount),
			Valid:  true,
		},
	})

	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Wallet Balance Updated Successfully", models.ToWalletResponse(&response)))

}

// getUserWallets godoc
// @Summary      Get User Wallets
// @Description  Retrieves the wallets associated with the authenticated user.
// @Tags         Wallets
// @Accept       json
// @Produce      json
// @Security 	 BearerAuth
// @Success      200            {object}  models.WalletCollectionResponse
// @Failure      400            {object}  basemodels.ErrorResponse
// @Failure      401            {object}  basemodels.ErrorResponse
// @Failure      500            {object}  basemodels.ErrorResponse
// @Router       /api/v1/wallets [get]
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

// getTransactions godoc
// @Summary      Get User Wallet Transactions
// @Description  Retrieves the transactions associated with the authenticated user's wallets.
// @Tags         Wallets
// @Accept       json
// @Produce      json
// @Security 	 BearerAuth
// @Param        page_limit     query     int     false "Number of transactions to retrieve per page"
// @Param        page_offset    query     int     false "Offset for pagination"
// @Success      200            {object}  basemodels.SuccessResponse{data=[]models.TransactionResponse}
// @Failure      400            {object}  basemodels.ErrorResponse
// @Failure      401            {object}  basemodels.ErrorResponse
// @Failure      500            {object}  basemodels.ErrorResponse
// @Router       /api/v1/wallets/transactions [get]
func (w *Wallet) getTransactions(ctx *gin.Context) {
	/// Pagination
	// cursor := ctx.Query("cursor")
	pageLimit := ctx.Query("page_limit")
	pageLimitInt, err := strconv.Atoi(pageLimit)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid page limit"))
		return
	}
	pageOffset := ctx.Query("page_offset")
	pageOffsetInt, err := strconv.Atoi(pageOffset)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid page offset"))
		return
	}

	if pageLimitInt == 0 {
		pageLimitInt = 10
	}

	if pageOffsetInt == 0 {
		pageOffsetInt = 0
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	wallet, err := w.server.queries.GetWalletByCustomerID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNoWallet))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// transactions, err := w.server.queries.GetTransactionsForWallet(ctx, db.GetTransactionsForWalletParams{
	// 	UsdWalletID: uuid.NullUUID{
	// 		UUID:  wallet[0].ID,
	// 		Valid: true,
	// 	},
	// 	NgnWalletID: uuid.NullUUID{
	// 		UUID:  wallet[1].ID,
	// 		Valid: true,
	// 	},
	// 	Limit:  int32(pageLimitInt),
	// 	Offset: int32(pageOffsetInt),
	// })

	// Find wallets by currency
	var usdWalletID, ngnWalletID, usdcWalletID, usdtWalletID uuid.NullUUID

	for _, w := range wallet {
		switch w.Currency {
		case "USD":
			usdWalletID = uuid.NullUUID{UUID: w.ID, Valid: true}
		case "NGN":
			ngnWalletID = uuid.NullUUID{UUID: w.ID, Valid: true}
		case "USDC":
			usdcWalletID = uuid.NullUUID{UUID: w.ID, Valid: true}
		case "USDT":
			usdtWalletID = uuid.NullUUID{UUID: w.ID, Valid: true}
		}
	}

	transactions, err := w.server.queries.GetTransactionsForWallet(ctx, db.GetTransactionsForWalletParams{
		UsdWalletID:  usdWalletID,
		NgnWalletID:  ngnWalletID,
		UsdcWalletID: usdcWalletID,
		UsdtWalletID: usdtWalletID,
		Limit:        int32(pageLimitInt),
		Offset:       int32(pageOffsetInt),
	})

	// transactions, err := w.server.queries.GetTransactionsByUserID(ctx, params)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNoWallet))
		return
	} else if err != nil {
		w.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("User Wallet Transactions Fetched Successfully", models.ToTransactionResponseObject(transactions)))

}

// getTransaction godoc
// @Summary      Get Single Wallet Transaction
// @Description  Retrieves a specific transaction associated with the authenticated user's wallet.
// @Tags         Wallets
// @Accept       json
// @Produce      json
// @Security 	 BearerAuth
// @Param        id             path      string  true  "Transaction ID"
// @Success      200            {object}  basemodels.SuccessResponse{data=models.TransactionResponse}
// @Failure      400            {object}  basemodels.ErrorResponse
// @Failure      401            {object}  basemodels.ErrorResponse
// @Failure      500            {object}  basemodels.ErrorResponse
// @Router       /api/v1/wallets/transactions/{id} [get]
func (w *Wallet) getTransaction(ctx *gin.Context) {
	transactionID := ctx.Param("id")

	if transactionID == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("transaction id is required"))
		return
	}

	transaction, err := w.server.queries.GetTransactionWithMetadata(ctx, uuid.MustParse(transactionID))

	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNoWallet))
		return
	} else if err != nil {
		w.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Transaction Fetched Successfully", models.ToTransactionResponse(transaction)))

}

// getTransactionsCursor godoc
// @Summary      Get User Wallet Transactions with Cursor Pagination
// @Description  Retrieves the transactions associated with the authenticated user's wallets using cursor-based pagination.
// @Tags         Wallets
// @Accept       json
// @Produce      json
// @Security 	 BearerAuth
// @Param        cursor_date        query     string  false "Cursor date for pagination (RFC3339 format)"
// @Param        cursor_transaction_id  query     string  false "Cursor transaction ID for pagination"
// @Success      200                {object}  basemodels.SuccessResponse{data=[]models.TransactionResponse}
// @Failure      400                {object}  basemodels.ErrorResponse
// @Failure	  	 401                {object}  basemodels.ErrorResponse
// @Failure      500                {object}  basemodels.ErrorResponse
// @Router       /api/v1/wallets/transactions-cursor [get]
func (w *Wallet) getTransactionsCursor(ctx *gin.Context) {
	/// Pagination
	var cursorDate time.Time
	cursorDateQuery := ctx.Query("cursor_date")
	if cursorDateQuery != "" {
		var err error
		cursorDate, err = time.Parse(time.RFC3339, cursorDateQuery)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid cursor date"))
			return
		}
	}

	var cursorTransactionIDUUID uuid.UUID
	cursorTransactionIDQuery := ctx.Query("cursor_transaction_id")
	if cursorTransactionIDQuery != "" {
		var err error
		cursorTransactionIDUUID, err = uuid.Parse(cursorTransactionIDQuery)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid cursor transaction id"))
			return
		}
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	wallet, err := w.server.queries.GetWalletByCustomerID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNoWallet))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	transactions, err := w.server.queries.GetTransactionsForWalletCursor(ctx, db.GetTransactionsForWalletCursorParams{
		UsdWalletID: uuid.NullUUID{
			UUID:  wallet[0].ID,
			Valid: true,
		},
		NgnWalletID: uuid.NullUUID{
			UUID:  wallet[1].ID,
			Valid: true,
		},
		CreatedAt: sql.NullTime{
			Time:  cursorDate,
			Valid: true,
		},
		TransactionID: uuid.NullUUID{
			UUID:  cursorTransactionIDUUID,
			Valid: true,
		},
	})

	// transactions, err := w.server.queries.GetTransactionsByUserID(ctx, params)
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
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	var request transaction.WalletTransferRequest

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	user, err := w.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err != nil {
		if err == sql.ErrNoRows {
			ctx.JSON(http.StatusUnauthorized, basemodels.NewError("user does not exist"))
			return
		}
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("an error occurred retrieving the user %v", err.Error())))
		return
	}

	if err = utils.VerifyHashValue(request.Pin, user.HashedPin.String); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidTransactionPIN))
		return
	}

	response, err := w.transactionService.HandleWalletTransfer(ctx, &user, request)
	if err != nil {
		ctx.JSON(500, basemodels.NewError(err.Error()))
		return
	}

	switch response.Status {
	case "successful":
		ctx.JSON(200, basemodels.NewSuccess("Transfer Successful", response))

	}
}

// banks godoc
// @Summary      Get Bank List
// @Description  Retrieves a list of supported banks for fiat transactions. Can be filtered using a query parameter.
// @Tags         Wallets
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        query    query     string  false  "Search query to filter banks"
// @Success      200     {object}  models.BankResponseCollection
// @Failure      500     {object}  basemodels.ErrorResponse
// @Router       /api/v1/wallets/banks [get]
func (w *Wallet) banks(ctx *gin.Context) {
	query := ctx.Query("query")

	banks, err := w.walletService.GetFiatBanks(w.server.provider, &query)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("fetched banks successfully", banks))
}

// resolveBankAccount godoc
// @Summary      Resolve Bank Account
// @Description  Validates and retrieves bank account details using account number and bank code.
// @Tags         Wallets
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        accountNumber  query     string  true   "Bank account number"
// @Param        bankCode      query     string  true   "Bank code"
// @Success      200          {object}  basemodels.SuccessResponse{data=models.AccountInfoResponse}
// @Failure      400          {object}  basemodels.ErrorResponse
// @Failure      500          {object}  basemodels.ErrorResponse
// @Router       /api/v1/wallets/resolve-bank-account [get]
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

// resolveUserTag godoc
// @Summary      Resolve User Tag
// @Description  Resolves a user tag to retrieve associated wallet information for a specific currency.
// @Tags         Wallets
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        userTag   query     string  true   "User tag to resolve"
// @Param        currency  query     string  true   "Currency code (USD|NGN|EUR)"
// @Success      200      {object}  basemodels.SuccessResponse{data=models.TagResolveResponse}
// @Failure      400      {object}  basemodels.ErrorResponse
// @Failure      500      {object}  basemodels.ErrorResponse
// @Router       /api/v1/wallets/resolve-user-tag [get]
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

// getBeneficiaries godoc
// @Summary      Get User's Beneficiaries
// @Description  Retrieves the list of saved beneficiaries for the authenticated user.
// @Tags         Wallets
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  basemodels.SuccessResponse{data=[]models.BeneficiaryResponse}
// @Failure      401  {object}  basemodels.ErrorResponse
// @Failure      500  {object}  basemodels.ErrorResponse
// @Router       /api/v1/wallets/beneficiaries [get]
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

type FiatTransferResponse struct {
	TransactionInfo  transaction.TransactionResponse[transaction.FiatWithdrawalMetadataResponse] `json:"transaction"`
	SavedBeneficiary bool                                                                        `json:"saved_beneficiary"`
}

// fiatTransfer godoc
// @Summary      Perform Fiat Transfer
// @Description  Executes a fiat currency transfer to a bank account.
// @Tags         Wallets
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      object  true  "Fiat Transfer Request"  SchemaExample({"name":"John Doe","account_number":"0123456789","bank_code":"057","wallet_id":"uuid","amount":1000,"pin":"1234","save_beneficiary":true})
// @Success      200     {object}  FiatTransferResponse
// @Failure      400     {object}  basemodels.ErrorResponse
// @Failure      401     {object}  basemodels.ErrorResponse
// @Failure      500     {object}  basemodels.ErrorResponse
// @Router       /api/v1/wallets/withdraw [post]
func (w *Wallet) fiatTransfer(ctx *gin.Context) {
	var request transaction.BankTransferRequest
	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		w.server.logger.Error(err)
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(err.Error()))
		return
	}

	/// Active USER must OWN source wallet
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
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

	response, err := w.transactionService.HandleBankTransfer(ctx, &dbUserValue, &request)
	if err != nil {
		ctx.JSON(500, basemodels.NewError(err.Error()))
		return
	}
	if response.Status == "failed" {
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("transfer failed", response))
		return
	}
	if response.Status == "pending" {
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("transfer pending", response))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("transfer successful", response))
}

// createTransactionFee godoc
// @Summary      Create Transaction Fee
// @Description  Creates or updates the fee structure for a specific transaction type.
// @Tags         Wallets
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request  body      transaction.CreateTransactionFeeRequest  true  "Transaction Fee Details"
// @Success      200     {object}  models.TransactionFeeResponse
// @Failure      400     {object}  basemodels.ErrorResponse
// @Failure      500     {object}  basemodels.ErrorResponse
// @Router       /api/v1/wallets/transaction-fee [post]
func (w *Wallet) createTransactionFee(ctx *gin.Context) {
	var request transaction.CreateTransactionFeeRequest

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		w.server.logger.Error(err.Error())
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		w.server.logger.Error("unauthorized access to create transaction fee")
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	err = ctx.ShouldBindJSON(&request)
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

	feeinfoId := strconv.Itoa(int(feeInfo.ID))

	auditEntry := audit.LogEntry{
		EventType:   audit.EventTransactionFeeCreated,
		ActorType:   activeUser.Role,
		ActorID:     &activeUser.UserID,
		Severity:    audit.SeverityInfo,
		EntityType:  "transaction_fees",
		EntityID:    feeinfoId,
		IPAddress:   net.IP(ctx.ClientIP()),
		UserAgent:   ctx.Request.UserAgent(),
		Description: "created transaction fee for " + string(request.TransactionType),
		Action:      audit.ActionCreate,
		Success:     true,
		Metadata: map[string]any{
			"transaction_type": request.TransactionType,
			"fee_percentage":   request.FeePercentage,
			"maximum_fee":      request.MaxFee,
		},
	}
	w.audit.Log(&auditEntry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Fee Created Successfully", models.ToTransactioFeeResponse(&feeInfo)))
}

// getTransactionFee godoc
// @Summary      Get Transaction Fee
// @Description  Retrieves the fee structure for a specific transaction type.
// @Tags         Wallets
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        type     query     string  true   "Transaction type (Transfer|Withdrawal|Deposit|Swap|GiftCard|Airtime)"
// @Success      200     {object}  models.TransactionFeeResponse
// @Failure      400     {object}  basemodels.ErrorResponse
// @Failure      500     {object}  basemodels.ErrorResponse
// @Router       /api/v1/wallets/transaction-fee [get]
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

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Fee Fetched Successfully", models.ToTransactioFeeResponse(&feeInfo)))
}

package wallet

import (
	"database/sql"
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/internal/common/middleware"
	"github.com/SwiftFiat/SwiftFiat-Backend/internal/common/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
)

type WalletDependencies struct {
	Router *gin.Engine
	Logger *logging.Logger
	Store  *db.Store
	// Do not be tempted to use the entire server, only add what you need
	// WalletService     service.WalletService
	// CurrencyService   service.CurrencyService
	// TransactionService service.TransactionService
	// Redis             redis.RedisClient
	// Config            config.Config
}

type WalletHandler struct {
	router *gin.Engine
	logger *logging.Logger
	store  *db.Store
	// walletService      *wallet.WalletService
	// currencyService    *currency.CurrencyService
	// transactionService *transaction.TransactionService
}

func NewWalletHandler(d *WalletDependencies) *WalletHandler {

	return &WalletHandler{
		router: d.Router,
		logger: d.Logger,
		store:  d.Store,
	}
}

func (w *WalletHandler) RegisterRoutes() {
	// w.walletService = wallet.NewWalletServiceWithCache(w.server.queries, w.server.logger, w.server.redis)
	// w.currencyService = currency.NewCurrencyService(w.server.queries, w.server.logger)
	// w.transactionService = transaction.NewTransactionService(
	// 	w.server.queries,
	// 	w.currencyService,
	// 	w.walletService,
	// 	w.server.logger,
	// )

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := w.router.Group("/api/v1/wallets")
	serverGroupV1.GET("", middleware.AuthenticatedMiddleware(), w.getUserWallets)
	// serverGroupV1.GET("transactions", middleware.AuthenticatedMiddleware(), w.getTransactions)
	// serverGroupV1.GET("transactions/:id", middleware.AuthenticatedMiddleware(), w.getSingleTransaction)
	// serverGroupV1.POST("transfer", middleware.AuthenticatedMiddleware(), w.walletTransfer)
	// serverGroupV1.POST("swap", middleware.AuthenticatedMiddleware(), w.swap)
	// serverGroupV1.GET("banks", middleware.AuthenticatedMiddleware(), w.banks)
	// serverGroupV1.GET("resolve-bank-account", middleware.AuthenticatedMiddleware(), w.resolveBankAccount)
	// serverGroupV1.GET("resolve-user-tag", middleware.AuthenticatedMiddleware(), w.resolveUserTag)
	// serverGroupV1.GET("beneficiaries", middleware.AuthenticatedMiddleware(), w.getBeneficiaries)
	// serverGroupV1.POST("withdraw", middleware.AuthenticatedMiddleware(), w.fiatTransfer)
}

func (w *WalletHandler) getUserWallets(ctx *gin.Context) {
	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, models.NewError(apistrings.UserNotFound))
		return
	}

	// TODO: Change this to talk to the service that will be instantiated by the necessary
	// values too
	accounts, err := w.store.GetWalletByCustomerID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, models.NewError(apistrings.UserNoWallet))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, models.NewError(apistrings.ServerError))
		return
	}

	// ctx.JSON(http.StatusOK, models.NewSuccess("User Wallets Fetched Successfully", models.ToWalletCollectionResponse(&accounts)))
	ctx.JSON(http.StatusOK, models.NewSuccess("User Wallets Fetched Successfully", accounts))
}

package api

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	_ "github.com/SwiftFiat/SwiftFiat-Backend/docs" // This will be generated
	"github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/bills"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/cryptocurrency"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/fiat"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/giftcards"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/kyc"
	activitylogs "github.com/SwiftFiat/SwiftFiat-Backend/services/activity_logs"
	bankaccounts "github.com/SwiftFiat/SwiftFiat-Backend/services/bank_accounts"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/tasks"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/redis"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/rewards"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/security"
	user_service "github.com/SwiftFiat/SwiftFiat-Backend/services/user"
	vaultsavings "github.com/SwiftFiat/SwiftFiat-Backend/services/vault_savings"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/wallet"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// / If there's a better place to access this
// / TODO, I feel the config should be the one accessible like this
var TokenController *utils.JWTToken

type Server struct {
	router                   *gin.Engine
	queries                  *db.Store
	config                   *utils.Config
	logger                   *logging.Logger
	taskScheduler            *tasks.TaskScheduler
	vaultScheduler           *vaultsavings.VaultScheduler
	yieldScheduler           *vaultsavings.YieldScheduler
	vaultService             *vaultsavings.VaultService
	yieldService             *vaultsavings.YieldService
	provider                 *providers.ProviderService
	redis                    *redis.RedisService
	pushNotification         *service.PushNotificationService
	authMiddleware           *AuthMiddleware
	emailService             *service.Plunk
	walletService            *wallet.WalletService
	auditLog                 *activitylogs.ActivityLog
	inAppnotificationService *service.Notification
	rewardService            *rewards.RewardService
	bankAccountService       *bankaccounts.BankAccountService
	userService              *user_service.UserService
}

func NewServer(envPath string) *Server {
	c, err := utils.LoadConfig("")
	if err != nil {
		panic(fmt.Sprintf("Could not load config: %v", err))
	}

	dbConn, err := sql.Open(c.DBDriver, utils.GetDBSource(c, c.DBName))
	if err != nil {
		panic(fmt.Sprintf("Could not load DB: %v", err))
	}

	m, err := migrate.New(
		"file://db/migrations",
		utils.GetDBSource(c, c.DBName),
	)
	if err != nil {
		log.Fatalf("Unable to instantiate the database schema migrator - %v", err)
	}

	if err := m.Up(); err != nil {
		if err != migrate.ErrNoChange {
			log.Fatalf("Unable to migrate up to the latest database schema - %v", err)
		}
	}

	q := db.NewStore(dbConn)
	gin.SetMode(c.Env)
	g := gin.Default()
	l := logging.NewLogger()
	p := providers.NewProviderService()
	pn := service.NewPushNotificationService(l)
	email := service.NewPlunkService(c)

	// Set up KYC service
	kp := kyc.NewKYCProvider()
	p.AddProvider(kp)

	// Set up GiftCard service
	gp := giftcards.NewGiftCardProvider()
	p.AddProvider(gp)

	// Set up Crypto service
	cp := cryptocurrency.NewCryptoProvider()
	p.AddProvider(cp)

	// Set up Crypto (Rates) service
	rp := cryptocurrency.NewRatesProvider()
	p.AddProvider(rp)

	// Setup Coin data service
	cd := cryptocurrency.NewCoinRankingProvider()
	p.AddProvider(cd)

	cryptomus := cryptocurrency.NewCryptomusProvider()
	p.AddProvider(cryptomus)

	// Set up Paystack Fiat Provider
	fp := fiat.NewFiatProvider()
	p.AddProvider(fp)

	// Set up Bills Provider
	bp := bills.NewBillProvider()
	p.AddProvider(bp)

	/// Add Middleware
	g.Use(CORSMiddleware())
	g.Use(l.LoggingMiddleWare())

	TokenController = utils.NewJWTToken(c)
	t := tasks.NewTaskScheduler(l)

	// wallet
	ws := wallet.NewWalletService(q, l)

	// auditlogs
	al := activitylogs.NewActivityLog(q)

	// in app notification service
	ns := service.NewNotificationService(q)

	// vault service
	vs := vaultsavings.NewVaultService(q, l, ws, email, pn, al, ns)

	// vault yield service
	ys := vaultsavings.NewYieldService(q, l, email, pn)

	yieldScheduler := vaultsavings.NewYieldScheduler(t, ys, q, l, 0)

	// vault scheduler
	vaultScheduler := vaultsavings.NewVaultScheduler(t, vs, q, l, 5*time.Minute)

	// reward service
	rs := rewards.NewRewardService(q, l, pn, security.NewCache())

	// bank account service
	bas := bankaccounts.NewBankAccountService(q, l, p)

	// user service
	us := user_service.NewUserService(q, l, ws)

	// Log Redis connection details (remove in production)
	log.Printf("Connecting to Redis at %s:%s", c.RedisHost, c.RedisPort)

	// Initialize Redis
	redisConfig := &redis.RedisConfig{
		Host:     c.RedisHost,
		Port:     c.RedisPort,
		Password: c.RedisPassword,
		DB:       0,
	}

	r, err := redis.NewRedisService(redisConfig)
	if err != nil {
		panic(fmt.Sprintf("Could not initialize Redis: %v", err))
	}

	am := NewAuthMiddleware(r)

	g.Static("/docs", "./docs/site") // serves docs at /docs
	// Register an application services manager
	// accessible via e.g ```server.services.WalletService```

	return &Server{
		router:                   g,
		queries:                  q,
		config:                   c,
		logger:                   l,
		taskScheduler:            t,
		provider:                 p,
		redis:                    r,
		pushNotification:         pn,
		authMiddleware:           am,
		emailService:             email,
		vaultScheduler:           vaultScheduler,
		walletService:            ws,
		auditLog:                 al,
		vaultService:             vs,
		yieldService:             ys,
		yieldScheduler:           yieldScheduler,
		inAppnotificationService: ns,
		rewardService:            rs,
		bankAccountService:       bas,
		userService:              us,
	}
}

func (s *Server) Start() error {

	dr := models.SuccessResponse{
		Status:  "success",
		Message: "Welcome to SwiftFiat!",
		Version: utils.REVISION,
	}

	s.router.GET("/", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, dr)
	})

	// Swagger documentation routes
	s.router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	/// Register Object Routers Below
	Auth{}.router(s)
	KYC{}.router(s)
	GiftCard{}.router(s)
	Wallet{}.router(s)
	Currency{}.router(s)
	CryptoAPI{}.router(s)
	User{}.router(s)
	Bills{}.router(s)
	Referral{}.router(s)
	ActivityLog{}.router(s)
	Vault{}.router(s)
	Rewards{}.router(s)

	/// TODO: Register all server dependent services to be accessible from SERVER
	// e.g. s.RegisterService({services.wallet, WalletService})

	// Start vault scheduler
	if s.vaultScheduler != nil {
		if err := s.vaultScheduler.Start(); err != nil {
			s.logger.Error("Failed to start vault scheduler", "error", err)
		}
	}

	if s.yieldScheduler != nil {
		if err := s.yieldScheduler.Start(); err != nil {
			s.logger.Error("Failed to start vault savings yield scheduler", "error", err)
		}
	}

	err := s.router.Run(fmt.Sprintf(":%v", s.config.ServerPort))
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	// Create a channel to track cleanup completion
	done := make(chan struct{})
	var shutdownErr error

	go func() {
		// Stop vault scheduler first
		if s.vaultScheduler != nil {
			if err := s.vaultScheduler.Stop(); err != nil {
				s.logger.Warn("Error stopping vault scheduler", "error", err)
			}
		}

		if s.yieldScheduler != nil {
			if err := s.yieldScheduler.Stop(); err != nil {
				s.logger.Warn("Error stopping vault savings yield scheduler", "error", err)
			}
		}

		// Close Redis connection with context awareness
		if err := s.redis.Close(); err != nil {
			s.logger.Error("Error closing Redis connection", "error", err)
			shutdownErr = err
		}

		close(done)
	}()

	// Wait for either context cancellation or cleanup completion
	select {
	case <-ctx.Done():
		return fmt.Errorf("shutdown timed out: %v", ctx.Err())
	case <-done:
		return shutdownErr
	}
}

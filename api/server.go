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
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/bridgecards"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/cryptocurrency"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/fiat"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/giftcards"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/kyc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	bankaccounts "github.com/SwiftFiat/SwiftFiat-Backend/services/bank_accounts"
	chatsupport "github.com/SwiftFiat/SwiftFiat-Backend/services/chat_support"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/currency"
	exchangerate "github.com/SwiftFiat/SwiftFiat-Backend/services/exchange_rate"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/tasks"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	rapidramp "github.com/SwiftFiat/SwiftFiat-Backend/services/rapid_ramp"
	ratemanager "github.com/SwiftFiat/SwiftFiat-Backend/services/rate_manager"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/redis"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/rewards"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/security"
	smartconversion "github.com/SwiftFiat/SwiftFiat-Backend/services/smart_conversion"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/subscriptions"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/transaction"
	user_service "github.com/SwiftFiat/SwiftFiat-Backend/services/user"
	vaultsavings "github.com/SwiftFiat/SwiftFiat-Backend/services/vault_savings"
	virtualcard "github.com/SwiftFiat/SwiftFiat-Backend/services/virtual_card"
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
	inAppnotificationService *service.Notification
	rewardService            *rewards.RewardService
	bankAccountService       *bankaccounts.BankAccountService
	userService              *user_service.UserService
	qrcodeService            *rapidramp.QRCodeService
	qrcodeScheduler          *rapidramp.RapidRampScheduler
	transactionService       *transaction.TransactionService
	currencyService          *currency.CurrencyService
	scExchangeRateservice    *exchangerate.ExchangeRateService
	smartConvertService      *smartconversion.ConversionService
	smartConversionScheduler *smartconversion.Scheduler
	auditService             *audit.Service
	rateManager              *ratemanager.Service
	virtualcard              *virtualcard.Service
	bridgecard               *bridgecards.BridgeCardProvider
	subscriptions            *subscriptions.Service
	subscriptionScheduler    *subscriptions.Scheduler
	chatService              *chatsupport.ChatService
	ticketService            *chatsupport.TicketService
	aiService                *chatsupport.AIService
	supportService           *chatsupport.SupportAdminService
	wsHub                    *Hub
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

	// audit service
	ads := audit.NewService(q, 0)

	// wallet
	ws := wallet.NewWalletService(q, l)

	// in app notification service
	ns := service.NewNotificationService(q)

	// vault service
	vs := vaultsavings.NewVaultService(q, l, ws, email, pn, ns)

	// vault yield service
	ys := vaultsavings.NewYieldService(q, l, email, pn)

	yieldScheduler := vaultsavings.NewYieldScheduler(t, ys, q, l, 1*time.Hour)

	// vault scheduler
	vaultScheduler := vaultsavings.NewVaultScheduler(t, vs, q, l, 1*time.Hour)

	// reward service
	rs := rewards.NewRewardService(q, l, pn, security.NewCache())

	// bank account service
	bas := bankaccounts.NewBankAccountService(q, l, p)

	// user service
	us := user_service.NewUserService(q, l, ws)

	// give PN the user service so it can resolve tokens
	pn.SetUserService(us)

	// qrcode service
	qr := rapidramp.NewQRCodeService(q, l, cryptomus, p, c)

	// subscriptons service
	ss := subscriptions.NewService(q, l)

	qrScheduler := rapidramp.NewRapidRampScheduler(
		t,
		qr,
		q,
		l,
		1*time.Minute,
	)

	// currency service
	cs := currency.NewCurrencyService(q, l)

	// transaction service
	txs := transaction.NewTransactionService(q, cs, ws, l, c, ns)

	// smart conversion exchange rate service
	scex := exchangerate.NewExchangeRateService(cryptomus, l)

	// Rates manager
	rm := ratemanager.NewService(q, scex, ads, l)

	// smart conversion service
	scs := smartconversion.NewConversionService(q, l, rm, scex, txs)

	// smart conversion scheduler
	scsScheduler := smartconversion.NewScheduler(t, q, l, scs, 0)

	// bridgecard service
	bridgecard := bridgecards.NewBridgeCardProvider(c, true, l)

	// virtual card service
	vcs := virtualcard.NewService(q, l, bridgecard, ws)

	// subscription scheduler
	ssScheduler := subscriptions.NewScheduler(t, ss, q, l, 1*time.Hour)

	// chat AI service
	ai := chatsupport.NewAIService(q, l, c)

	// chat service
	chat := chatsupport.NewChatService(q, l)

	// tickets
	ticket := chatsupport.NewTicketService(q, l, ns, email)

	// admin support
	support := chatsupport.NewSupportAdminService(q, l)

	// Initialize WebSocket Hub
	wsHub := NewHub(l)
	go wsHub.Run()

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
		vaultService:             vs,
		yieldService:             ys,
		yieldScheduler:           yieldScheduler,
		inAppnotificationService: ns,
		rewardService:            rs,
		bankAccountService:       bas,
		userService:              us,
		qrcodeService:            qr,
		qrcodeScheduler:          qrScheduler,
		transactionService:       txs,
		currencyService:          cs,
		scExchangeRateservice:    scex,
		smartConvertService:      scs,
		smartConversionScheduler: scsScheduler,
		auditService:             ads,
		rateManager:              rm,
		virtualcard:              vcs,
		bridgecard:               bridgecard,
		subscriptions:            ss,
		subscriptionScheduler:    ssScheduler,
		chatService:              chat,
		aiService:                ai,
		ticketService:            ticket,
		supportService:           support,
		wsHub:                    wsHub,
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
	QRCodeHandler{}.router(s)
	SmartConvertHandler{}.router(s)
	Streaks{}.router(s)
	AuditHandler{}.router(s)
	RateManagerHandler{}.router(s)
	Virtualcard{}.router(s)
	Subscriptions{}.router(s)
	ChatSupport{}.router(s)
	SupportAdmin{}.router(s)
	WebSocketHandler{}.router(s)

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

	// Start rapid ramp scheduler
	if s.qrcodeScheduler != nil {
		if err := s.qrcodeScheduler.Start(); err != nil {
			s.logger.Error("Failed to start rapid ramp scheduler", "error", err)
		}
	}

	// Start smart conversion scheduler
	if s.smartConversionScheduler != nil {
		if err := s.smartConversionScheduler.Start(); err != nil {
			s.logger.Error("Failed to start smart conversion scheduler", "error", err)
		}
	}

	// Start subscription scheduler
	if s.subscriptionScheduler != nil {
		if err := s.subscriptionScheduler.Start(); err != nil {
			s.logger.Error("Failed to start subscription scheduler", "error", err)
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

		// Stop rapid ramp scheduler
		if s.qrcodeScheduler != nil {
			if err := s.qrcodeScheduler.Stop(); err != nil {
				s.logger.Warn("Error stopping rapid ramp scheduler", "error", err)
			}
		}

		// Stop smart conversion scheduler
		if s.smartConversionScheduler != nil {
			if err := s.smartConversionScheduler.Stop(); err != nil {
				s.logger.Warn("Error stopping smart conversion scheduler", "error", err)
			}
		}

		// Stop subscription scheduler
		if s.subscriptionScheduler != nil {
			if err := s.subscriptionScheduler.Stop(); err != nil {
				s.logger.Warn("Error stopping subscription scheduler", "error", err)
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

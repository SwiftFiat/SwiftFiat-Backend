package api

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/tasks"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/provider"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/provider/giftcards"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/provider/kyc"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// / If there's a better place to access this
// / TODO, I feel the config should be the one accessible like this
var TokenController *utils.JWTToken

type Server struct {
	router        *gin.Engine
	queries       *db.Store
	config        *utils.Config
	logger        *logging.Logger
	taskScheduler *tasks.TaskScheduler
	provider      *provider.ProviderService
	redis         *services.RedisService
}

func NewServer(envPath string) *Server {
	c, err := utils.LoadConfig(envPath)
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
	g := gin.Default()
	l := logging.NewLogger()
	p := provider.NewProviderService()

	// Set up KYC service
	kp := kyc.NewKYCProvider()
	p.AddProvider(kp)

	// Set up GiftCard service
	gp := giftcards.NewGiftCardProvider()
	p.AddProvider(gp)

	/// Add Middleware
	g.Use(CORSMiddleware())
	g.Use(l.LoggingMiddleWare())

	TokenController = utils.NewJWTToken(c)
	t := tasks.NewTaskScheduler(l)

	// Log Redis connection details (remove in production)
	log.Printf("Connecting to Redis at %s:%s", c.RedisHost, c.RedisPort)

	// Initialize Redis
	redisConfig := &services.RedisConfig{
		Host:     c.RedisHost,
		Port:     c.RedisPort,
		Password: c.RedisPassword,
		DB:       0,
	}

	r, err := services.NewRedisService(redisConfig)
	if err != nil {
		panic(fmt.Sprintf("Could not initialize Redis: %v", err))
	}

	return &Server{
		router:        g,
		queries:       q,
		config:        c,
		logger:        l,
		taskScheduler: t,
		provider:      p,
		redis:         r,
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

	/// Register Object Routers Below
	Auth{}.router(s)
	KYC{}.router(s)
	GiftCard{}.router(s)
	Wallet{}.router(s)

	err := s.router.Run(fmt.Sprintf(":%v", s.config.ServerPort))
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	// Create a channel to track cleanup completion
	done := make(chan struct{})
	var shutdownErr error

	go func() {
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

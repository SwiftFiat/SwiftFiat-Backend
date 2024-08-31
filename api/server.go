package api

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/monitoring/logging"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/provider"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/provider/kyc"
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
	router   *gin.Engine
	queries  *db.Queries
	config   *utils.Config
	logger   *logging.Logger
	provider *provider.ProviderService
}

func NewServer(envPath string) *Server {
	c, err := utils.LoadConfig(envPath)
	if err != nil {
		panic(fmt.Sprintf("Could not load config: %v", err))
	}

	conn, err := sql.Open(c.DBDriver, utils.GetDBSource(c, c.DBName))
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

	q := db.New(conn)
	g := gin.Default()
	l := logging.NewLogger()
	p := provider.NewProviderService()

	// Set up KYC service
	kp := kyc.NewKYCProvider()
	p.AddProvider(kp)

	g.Use(CORSMiddleware())
	g.Use(l.LoggingMiddleWare())

	TokenController = utils.NewJWTToken(c)

	return &Server{
		router:   g,
		queries:  q,
		config:   c,
		logger:   l,
		provider: p,
	}
}

func (s *Server) Start() {

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

	s.router.Run(fmt.Sprintf(":%v", s.config.ServerPort))
}

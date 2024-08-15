package api

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"

	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	"github.com/SwiftFiat/SwiftFiat-Backend/models"
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
	router  *gin.Engine
	queries *db.Queries
	config  *utils.Config
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
	g.Use(CORSMiddleware())
	g.Use(gin.Logger())

	TokenController = utils.NewJWTToken(c)

	return &Server{
		router:  g,
		queries: q,
		config:  c,
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

	s.router.Run(fmt.Sprintf(":%v", s.config.ServerPort))
}

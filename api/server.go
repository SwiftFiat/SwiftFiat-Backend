package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Server struct {
	router *gin.Engine
}

func NewServer(envPath string) *Server {
	g := gin.Default()

	return &Server{
		router: g,
	}
}

func (s *Server) Start(port int) {

	s.router.GET("/", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"message": "Welcome to SwiftFiat!"})
	})

	/// Register Object Routers Below

	s.router.Run(fmt.Sprintf(":%v", port))
}

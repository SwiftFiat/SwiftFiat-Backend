package api

type Auth struct {
	server *Server
}

func (a Auth) router(server *Server) {
	a.server = server

	serverGroup := server.router.Group("/auth")
	serverGroup.POST("login", a.login)
	serverGroup.POST("register", a.register)
	serverGroup.POST("register-admin", a.registerAdmin)
}

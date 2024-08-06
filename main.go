package main

import (
	"github.com/SwiftFiat/SwiftFiat-Backend/api"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/security"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

func main() {

	cache := security.NewCache()
	cache.Start()

	server := api.NewServer(utils.EnvPath)
	server.Start()
}

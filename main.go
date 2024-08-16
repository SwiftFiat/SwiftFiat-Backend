package main

import (
	"github.com/SwiftFiat/SwiftFiat-Backend/api"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/security"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

func main() {

	// fmt.Println(string(27) + "[35mColored.")

	// Set up token cache
	cache := security.NewCache()
	cache.Start()

	// Set up server
	server := api.NewServer(utils.EnvPath)
	server.Start()
}

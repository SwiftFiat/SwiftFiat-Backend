package main

import (
	"fmt"

	"github.com/SwiftFiat/SwiftFiat-Backend/api"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/security"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

var envPath string = "."

func main() {

	config, err := utils.LoadConfig(envPath)
	if err != nil {
		panic(fmt.Sprintf("Could not load config: %v", err))
	}

	cache := security.NewCache()
	cache.Start()

	server := api.NewServer(".")
	server.Start(config.ServerPort)
}

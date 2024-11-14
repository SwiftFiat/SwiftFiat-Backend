package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/security"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
)

func main() {
	// Create a context that we can cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create WaitGroup to track all running services
	var wg sync.WaitGroup

	// Initialize logger
	logger := log.New(os.Stdout, "[SwiftFiat] ", log.LstdFlags)

	// Channel to receive shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Channel for startup errors
	startupErrors := make(chan error, 1)

	// Start cache service
	cache := security.NewCache()
	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Println("Starting token cache service...")
		if err := cache.Start(); err != nil {
			startupErrors <- fmt.Errorf("failed to start cache service: %v", err)
			cancel()
			return
		}

		// Monitor context for shutdown
		<-ctx.Done()
		logger.Println("Stopping token cache service...")
		if err := cache.Stop(); err != nil {
			logger.Printf("Error stopping cache service: %v", err)
		}
	}()

	// Initialize and start server
	var server *api.Server
	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Println("Initializing server...")
		server = api.NewServer(utils.EnvPath)
		if server == nil {
			startupErrors <- fmt.Errorf("failed to initialize server")
			cancel()
			return
		}

		// Start server in a separate goroutine
		go func() {
			logger.Println("Starting server...")
			if err := server.Start(); err != nil {
				startupErrors <- fmt.Errorf("server error: %v", err)
				cancel()
			}
		}()

		// Monitor context for shutdown
		<-ctx.Done()
		logger.Println("Stopping server...")

		// Create shutdown context with timeout
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Printf("Error during server shutdown: %v", err)
		}
	}()

	// Monitor for shutdown signals or startup errors
	select {
	case err := <-startupErrors:
		logger.Printf("Startup error: %v", err)
		cancel() // Trigger shutdown of all services
	case sig := <-shutdown:
		logger.Printf("Received shutdown signal: %v", sig)
		cancel() // Trigger shutdown of all services
	}

	// Wait for all services to shutdown with timeout
	waitChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitChan)
	}()

	select {
	case <-waitChan:
		logger.Println("All services stopped successfully")
	case <-time.After(45 * time.Second):
		logger.Println("Shutdown timed out")
	}
}

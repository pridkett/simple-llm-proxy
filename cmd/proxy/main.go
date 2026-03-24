package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pwagstro/simple_llm_proxy/internal/api"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/openapi"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/storage/sqlite"

	// Register providers
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/anthropic"
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/openai"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	startTime := time.Now()

	// Load configuration
	reloader, err := config.NewReloader(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	cfg := reloader.Config()

	// Initialize router
	r, err := router.New(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize router: %v", err)
	}

	// Initialize storage
	var store storage.Storage
	if cfg.GeneralSettings.DatabaseURL != "" {
		sqliteStore, err := sqlite.New(cfg.GeneralSettings.DatabaseURL)
		if err != nil {
			log.Fatalf("Failed to initialize storage: %v", err)
		}
		if err := sqliteStore.Initialize(context.Background()); err != nil {
			log.Fatalf("Failed to run migrations: %v", err)
		}
		store = sqliteStore
		defer store.Close()
	}

	// Build OpenAPI spec
	spec := openapi.New()
	if err := spec.Build(); err != nil {
		log.Fatalf("Failed to build OpenAPI spec: %v", err)
	}

	// Initialize cost map manager (non-fatal: proxy starts even if CDN is unreachable)
	cm := costmap.New()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := cm.Load(ctx); err != nil {
			log.Printf("Warning: failed to load initial cost map: %v", err)
		}
	}()

	// Create HTTP router
	httpRouter := api.NewRouter(r, store, reloader, cm, startTime, spec)

	// Create server
	addr := fmt.Sprintf(":%d", cfg.GeneralSettings.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      httpRouter,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second, // Long timeout for streaming
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Starting server on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}

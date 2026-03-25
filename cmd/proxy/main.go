package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/pwagstro/simple_llm_proxy/internal/api"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/logger"
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

	// Load configuration — use fmt+os.Exit here because the logger isn't initialized yet.
	reloader, err := config.NewReloader(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	cfg := reloader.Config()

	// Initialize structured logger before any other operations.
	logger.Init(cfg.LogSettings)

	// Initialize router
	r, err := router.New(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize router")
	}

	// Initialize storage
	var store storage.Storage
	if cfg.GeneralSettings.DatabaseURL != "" {
		sqliteStore, err := sqlite.New(cfg.GeneralSettings.DatabaseURL)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to initialize storage")
		}
		if err := sqliteStore.Initialize(context.Background()); err != nil {
			log.Fatal().Err(err).Msg("failed to run migrations")
		}
		store = sqliteStore
		defer store.Close()
	}

	// Build OpenAPI spec
	spec := openapi.New()
	if err := spec.Build(); err != nil {
		log.Fatal().Err(err).Msg("failed to build OpenAPI spec")
	}

	// Initialize cost map manager (non-fatal: proxy starts even if CDN is unreachable)
	cm := costmap.New()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := cm.Load(ctx); err != nil {
			log.Warn().Err(err).Msg("failed to load initial cost map")
		}
	}()

	// Seed cost overrides from SQLite into the costmap Manager.
	// This restores user-defined mappings across server restarts.
	// Done before the HTTP server starts so all routes see consistent state.
	if store != nil {
		seedCostOverrides(context.Background(), store, cm)
	}

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
		log.Info().Str("addr", addr).Msg("starting server")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down server")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("server forced to shutdown")
	}

	log.Info().Msg("server exited")
}

// seedCostOverrides reads persisted cost overrides from storage and loads them into the
// costmap Manager's in-memory state. Called once at startup before serving requests.
func seedCostOverrides(ctx context.Context, store storage.Storage, cm *costmap.Manager) {
	overrides, err := store.ListCostOverrides(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("failed to load cost overrides from storage")
		return
	}
	for _, ov := range overrides {
		if ov.CostMapKey != nil {
			cm.SetOverrideKey(ov.ModelName, *ov.CostMapKey)
		} else if ov.CustomSpec != nil {
			var spec costmap.ModelSpec
			if err := json.Unmarshal([]byte(*ov.CustomSpec), &spec); err != nil {
				log.Warn().Err(err).Str("model", ov.ModelName).Msg("failed to decode custom cost spec")
				continue
			}
			cm.SetCustomSpec(ov.ModelName, spec)
		}
	}
	if len(overrides) > 0 {
		log.Info().Int("count", len(overrides)).Msg("loaded cost overrides from storage")
	}
}

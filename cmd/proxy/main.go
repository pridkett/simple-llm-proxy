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

	"github.com/alexedwards/scs/v2"
	"github.com/rs/zerolog/log"

	"github.com/pwagstro/simple_llm_proxy/internal/api"
	"github.com/pwagstro/simple_llm_proxy/internal/auth"
	"github.com/pwagstro/simple_llm_proxy/internal/config"
	"github.com/pwagstro/simple_llm_proxy/internal/costmap"
	"github.com/pwagstro/simple_llm_proxy/internal/keystore"
	"github.com/pwagstro/simple_llm_proxy/internal/logger"
	"github.com/pwagstro/simple_llm_proxy/internal/openapi"
	"github.com/pwagstro/simple_llm_proxy/internal/router"
	"github.com/pwagstro/simple_llm_proxy/internal/storage"
	"github.com/pwagstro/simple_llm_proxy/internal/storage/sqlite"

	// Register providers — blank imports trigger init() to self-register with the provider registry.
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/anthropic"
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/gemini"
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/minimax"
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/ollama"
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/openai"
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/openrouter"
	_ "github.com/pwagstro/simple_llm_proxy/internal/provider/vllm"
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
	var sqliteStore *sqlite.Storage
	if cfg.GeneralSettings.DatabaseURL != "" {
		var err error
		sqliteStore, err = sqlite.New(cfg.GeneralSettings.DatabaseURL)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to initialize storage")
		}
		if err := sqliteStore.Initialize(context.Background()); err != nil {
			log.Fatal().Err(err).Msg("failed to run migrations")
		}
		store = sqliteStore
		defer store.Close()
	}

	// Create SCS session manager backed by the custom SQLite session store.
	// Cookie attributes are set explicitly per ADR 003 §4.
	sm := scs.New()
	if sqliteStore != nil {
		sm.Store = &sqlite.SessionStore{DB: sqliteStore.DB()}
	}
	sm.Lifetime = 24 * time.Hour
	sm.IdleTimeout = 2 * time.Hour
	sm.Cookie.Name = "proxy_session"
	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = !cfg.OIDCSettings.DevMode // true in production, false in local HTTP dev
	sm.Cookie.SameSite = http.SameSiteLaxMode    // SameSite=Lax: CSRF protection for admin mutations
	sm.Cookie.Path = "/"

	// Initialize OIDC provider (returns nil without error when IssuerURL is empty).
	oidcProvider, err := auth.NewOIDCProvider(
		context.Background(),
		cfg.OIDCSettings.IssuerURL,
		cfg.OIDCSettings.ClientID,
		cfg.OIDCSettings.ClientSecret,
		cfg.OIDCSettings.RedirectURL,
		cfg.OIDCSettings.AdminGroup,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize OIDC provider")
	}
	if oidcProvider != nil {
		log.Info().Str("issuer", cfg.OIDCSettings.IssuerURL).Msg("OIDC provider initialized")
	} else {
		log.Info().Msg("OIDC not configured — /auth/* endpoints will return 503")
	}

	// Start background goroutine for expired session cleanup (runs hourly per ADR 003 §11).
	if store != nil {
		go func() {
			ticker := time.NewTicker(1 * time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if err := store.CleanExpiredSessions(context.Background()); err != nil {
						log.Warn().Err(err).Msg("failed to clean expired sessions")
					}
				}
			}
		}()
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

	// Initialize keystore (in-memory enforcement engine for per-app API keys)
	cache := keystore.New(60 * time.Second) // 60s TTL per D-07
	rl := keystore.NewRateLimiter()
	sa := keystore.NewSpendAccumulator()

	// Initialize spend accumulator from historical usage_logs (D-09).
	// Non-fatal: accumulator starts at 0 if DB query fails.
	if store != nil {
		initCtx, initCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := sa.InitFromStorage(initCtx, store); err != nil {
			log.Warn().Err(err).Msg("spend accumulator init failed: starting at 0")
		}
		initCancel()
	}

	// Create HTTP router
	httpRouter := api.NewRouter(r, store, reloader, cm, startTime, spec, sm, oidcProvider, cache, rl, sa)

	// Create server
	addr := fmt.Sprintf(":%d", cfg.GeneralSettings.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      httpRouter,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second, // Long timeout for streaming
		IdleTimeout:  120 * time.Second,
	}

	// Spend flush loop — persists in-memory spend totals to usage_logs every 30s (D-09).
	// On shutdown, a final flush is performed before process exit.
	flushDone := make(chan struct{})
	shutdownFlush := make(chan struct{})
	if store != nil {
		go func() {
			defer close(flushDone)
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					flushCtx, flushCancel := context.WithTimeout(context.Background(), 10*time.Second)
					if err := sa.FlushToStorage(flushCtx, store); err != nil {
						log.Warn().Err(err).Msg("spend flush failed")
					}
					flushCancel()
				case <-shutdownFlush:
					flushCtx, flushCancel := context.WithTimeout(context.Background(), 10*time.Second)
					if err := sa.FlushToStorage(flushCtx, store); err != nil {
						log.Warn().Err(err).Msg("spend final flush on shutdown failed")
					}
					flushCancel()
					return
				}
			}
		}()
	} else {
		close(flushDone)
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

	// Signal spend flush loop to perform final flush and stop
	close(shutdownFlush)
	<-flushDone

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

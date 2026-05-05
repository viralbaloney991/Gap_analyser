package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"coralogix-alert-analyzer/internal/api"
	"coralogix-alert-analyzer/internal/cache"
	"coralogix-alert-analyzer/internal/config"
	"coralogix-alert-analyzer/internal/monday"
	"coralogix-alert-analyzer/internal/pipeline"
	alertprewarm "coralogix-alert-analyzer/internal/prewarm"
	alertstore "coralogix-alert-analyzer/internal/store"
	alertsync "coralogix-alert-analyzer/internal/sync"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	corsOrigin := os.Getenv("CORS_ORIGIN")
	if corsOrigin == "" {
		corsOrigin = "http://localhost:5173"
	}

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "clients.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	log.Printf("loaded config: %d clients", len(cfg.Clients))

	// Pipeline semaphore — global cap on concurrent LLM calls across all stages.
	sem := pipeline.NewSemaphore(cfg.LLM.PipelineGlobalCap)
	log.Printf("INFO [pipeline] semaphore cap=%d batch=%d", cfg.LLM.PipelineGlobalCap, cfg.LLM.PipelineBatchSize)

	// Auto-resolve Monday group IDs for clients that don't have one configured.
	{
		resolveCtx, resolveCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer resolveCancel()
		mondayResolver := monday.NewClient(cfg.MondayAPIToken, cfg.MondayBoardID)
		if groups, err := mondayResolver.FetchGroups(resolveCtx); err != nil {
			log.Printf("WARN [monday] could not fetch groups for auto-resolution: %v", err)
		} else {
			resolveGroupIDs(cfg, groups)
		}
	}

	// Context cancelled on shutdown — used by sync worker.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Redis (L1 cache).
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	redisStore, err := cache.NewStore(redisAddr)
	if err != nil {
		log.Printf("WARN: Redis unavailable (%v) — running without cache", err)
	}
	if redisStore != nil {
		defer redisStore.Close()
	}

	// NeonDB (L2 persistent store).
	var neonStore *alertstore.Store
	if cfg.NeonDSN != "" {
		initCtx, initCancel := context.WithTimeout(ctx, 10*time.Second)
		neonStore, err = alertstore.New(initCtx, cfg.NeonDSN)
		initCancel()
		if err != nil {
			log.Printf("WARN: NeonDB unavailable (%v) — running without alert persistence", err)
		} else {
			defer neonStore.Close()

			// Start background sync worker.
			worker := alertsync.New(neonStore, redisStore, cfg.Clients)
			go worker.Start(ctx)
		}
	} else {
		log.Printf("INFO: neon_dsn not configured — alert persistence disabled")
	}

	// Suggestion pre-warm worker — nil-safe if NeonDB is unavailable.
	var prewarmWorker *alertprewarm.Worker
	if neonStore != nil {
		prewarmWorker = alertprewarm.New(cfg, neonStore, monday.NewClient(cfg.MondayAPIToken, cfg.MondayBoardID), sem)
	}

	handler := api.NewHandler(cfg, redisStore, neonStore, prewarmWorker, sem)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", handler.HandleHealth)
	mux.HandleFunc("/api/clients", handler.HandleClients)
	mux.HandleFunc("/api/analyze", handler.HandleAnalyze)
	mux.HandleFunc("/api/insights", handler.HandleInsights)
	mux.HandleFunc("/api/noise", handler.HandleNoise)
	mux.HandleFunc("/api/suggestions", handler.HandleSuggestions)
	mux.HandleFunc("/api/correlations", handler.HandleCorrelations)
	mux.HandleFunc("/api/export/narrative", handler.HandleExportNarrative)

	// Serve static frontend files in production.
	frontendDist := os.Getenv("FRONTEND_DIST")
	if frontendDist == "" {
		frontendDist = filepath.Join(".", "..", "..", "frontend", "dist")
	}
	if info, err := os.Stat(frontendDist); err == nil && info.IsDir() {
		fs := http.FileServer(http.Dir(frontendDist))
		mux.Handle("/", fs)
		log.Printf("serving static files from %s", frontendDist)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("coralogix-alert-analyzer API server"))
		})
	}

	wrapped := corsMiddleware(corsOrigin)(mux)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      wrapped,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 180 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("server starting on :%s (CORS origin: %s)", port, corsOrigin)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("received signal %s, shutting down gracefully...", sig)

	cancel() // stop sync worker

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}

	log.Println("server stopped")
}

// corsMiddleware returns middleware that handles CORS headers and preflight requests.
func corsMiddleware(allowOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			allowed := false
			for _, o := range strings.Split(allowOrigin, ",") {
				if strings.TrimSpace(o) == origin {
					allowed = true
					break
				}
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

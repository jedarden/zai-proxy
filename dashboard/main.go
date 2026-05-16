// zai-proxy-dashboard is a real-time web dashboard for zai-proxy metrics.
package main

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/ardenone/ardenone-cluster/containers/zai-proxy-dashboard/api"
	"github.com/ardenone/ardenone-cluster/containers/zai-proxy-dashboard/collector"
	"github.com/ardenone/ardenone-cluster/containers/zai-proxy-dashboard/logger"
	"github.com/ardenone/ardenone-cluster/containers/zai-proxy-dashboard/storage"
)

//go:embed frontend/dist/*
var frontendFS embed.FS

func main() {
	// Initialize structured logging
	logger.Init(logger.DefaultConfig())
	log := logger.Component("main")

	// Load configuration
	collectorConfig := collector.DefaultConfig()
	storageConfig := storage.DefaultConfig()
	apiConfig := api.DefaultConfig()

	log.Info("initializing zai-proxy-dashboard",
		"scrape_targets", collectorConfig.Targets,
		"scrape_interval", collectorConfig.Interval.String(),
		"listen_addr", apiConfig.ListenAddr,
	)

	// Initialize storage
	store, err := storage.NewStorage(storageConfig)
	if err != nil {
		log.Error("failed to initialize storage", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	log.Info("storage initialized", "db_path", storageConfig.DBPath)

	// Initialize collector
	coll := collector.NewCollector(collectorConfig)
	log.Info("collector initialized")

	// Initialize SSE hub
	hub := api.NewSSEHub(apiConfig)
	go hub.Run()
	log.Info("sse hub started")

	// Set up API router
	router := api.NewRouter(hub, store, apiConfig)

	// Start collector
	ctx, cancel := context.WithCancel(context.Background())
	go coll.Start(ctx)

	// Connect collector to storage and hub
	go func() {
		snapLog := logger.Component("snapshot-processor")
		for snapshot := range coll.Snapshots() {
			// Write to storage
			store.Write(snapshot)

			// Broadcast to SSE clients
			hub.Broadcast(snapshot)
			snapLog.Debug("processed snapshot",
				"variant", snapshot.Variant,
				"timestamp", snapshot.Timestamp,
				"req_rate", snapshot.ReqRate,
			)
		}
	}()

	// Set up HTTP server
	mux := http.NewServeMux()
	router.SetupRoutes(mux)

	// Serve embedded frontend
	frontendSub, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		log.Error("failed to create frontend sub FS", "error", err)
		os.Exit(1)
	}
	frontendHandler := http.FileServer(http.FS(frontendSub))
	mux.Handle("/assets/", frontendHandler)

	// Serve index.html for root path - directly read to avoid redirect loop
	// (http.FileServer with modified URL.Path can cause 301 redirects)
	indexHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, err := frontendFS.ReadFile("frontend/dist/index.html")
		if err != nil {
			http.Error(w, "index.html not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(content)
	})

	// Serve index.html for all other routes (SPA)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			indexHandler.ServeHTTP(w, r)
			return
		}
		// Try static files first, then fallback to index.html for SPA routing
		if _, err := frontendFS.Open("frontend/dist" + r.URL.Path); err == nil {
			frontendHandler.ServeHTTP(w, r)
			return
		}
		// SPA fallback - serve index.html for client-side routes
		indexHandler.ServeHTTP(w, r)
	})

	// Apply middleware
	handler := api.Chain(mux,
		api.RecoveryMiddleware,
		api.LoggingMiddleware,
		api.CORSMiddleware,
	)

	server := &http.Server{
		Addr:    apiConfig.ListenAddr,
		Handler: handler,
	}

	// Handle shutdown gracefully
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigChan

		log.Info("shutdown signal received", "signal", sig.String())
		cancel()
		server.Shutdown(context.Background())
	}()

	log.Info("server starting", "address", apiConfig.ListenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Error("server error", "error", err)
		os.Exit(1)
	}

	log.Info("server shutdown complete")
}

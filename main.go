package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dify2api/config"
	"dify2api/db"
	"dify2api/handler"
)

// Version is the Dify2API release version, printed by the -version flag.
const Version = "1.0.0"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Parse command-line flags
	adminPath := flag.String("admin", "", "Path to the startup configuration file (required; see admin.env.example)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.BoolVar(showVersion, "v", false, "Print version and exit (shorthand)")
	debugMode := flag.Bool("debug", false, "Debug mode: intercept requests, dump them to disk, and return an error (nothing is sent to Dify)")
	debugDir := flag.String("debug-dir", "debug_dumps", "Directory for debug dump folders (used with -debug)")
	forceHTTPS := flag.Bool("force-https", false, "Redirect plain-HTTP requests to HTTPS (use behind a TLS-terminating proxy)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("Dify2API v%s\n", Version)
		return
	}

	// Load all configuration from the single startup file (OS env overrides apply)
	cfg, err := config.LoadStartup(*adminPath)
	if err != nil {
		log.Fatalf("Configuration error: %v\n\nSee admin.env.example for the full startup file format.", err)
	}
	cfg.Debug = *debugMode
	cfg.DebugDir = *debugDir
	cfg.ForceHTTPS = *forceHTTPS

	// Open the database (schema + encryption master key)
	store, err := db.Open(cfg.DBPath, cfg.MasterKeyPath)
	if err != nil {
		log.Fatalf("Database error: %v", err)
	}
	defer store.Close()

	log.Printf("Dify2API v%s starting", Version)
	log.Printf("  Startup file: %s", *adminPath)
	log.Printf("  Database:    %s", cfg.DBPath)
	if cfg.Debug {
		log.Printf("  ⚠️  DEBUG MODE: requests will be intercepted and dumped to %q; nothing is sent to Dify", cfg.DebugDir)
	}
	log.Printf("  Listen Addr:   %s", cfg.ListenAddr)
	log.Printf("  Site Base URL: %s (admin site: %s)", cfg.Admin.SiteBaseURL, cfg.Admin.AdminHost)
	log.Printf("  Limits: chat_in_flight=%d body=%dMB sse_buffer=%dMB login_lock=%dmin",
		cfg.MaxChatInFlight, cfg.MaxRequestBodyMB, cfg.SSEBufferMB, cfg.LoginLockMin)
	if cfg.ForceHTTPS {
		log.Printf("  HTTPS enforcement: ON (plain HTTP will be redirected to HTTPS)")
	} else {
		log.Printf("  ⚠️  HTTP access ALLOWED (default). For public deployment, terminate TLS (see nginx/) and start with -force-https")
	}
	log.Printf("  Endpoints:")
	log.Printf("    POST /v1/chat/completions  (OpenAI-compatible)")
	log.Printf("    GET  /v1/models            (Model list)")
	log.Printf("    GET  /health               (Health check)")

	// Create gateway and register routes
	gw := handler.NewGateway(cfg, store)
	mux := http.NewServeMux()
	gw.RegisterRoutes(mux)

	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: gw.Wrap(mux),
		// Slowloris hardening: cap how slowly clients may send. WriteTimeout is
		// intentionally unset — SSE streams legitimately last minutes.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("Received signal %v, shutting down...", sig)
		server.Close()
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server stopped")
}

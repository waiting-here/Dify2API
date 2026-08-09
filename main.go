package main

import (
	"context"
	"errors"
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
// 发版时与 git tag 同步（tag 格式 v<major.minor.patch>）。
const Version = "1.3.0"

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
	// Resolve durable charity reservations left by an interrupted process
	// before accepting traffic: never-dispatched work is refunded; dispatched
	// work is conservatively settled as consumed.
	recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 30*time.Second)
	releasedReservations, committedReservations, recoveryErr := store.RecoverCharityReservations(recoveryCtx)
	recoveryCancel()
	if recoveryErr != nil {
		log.Fatalf("Charity reservation recovery error: %v", recoveryErr)
	}
	if releasedReservations+committedReservations > 0 {
		log.Printf("[RECOVERY] charity reservations: released=%d committed=%d", releasedReservations, committedReservations)
	}

	log.Printf("Dify2API v%s starting", Version)
	log.Printf("  Startup file: %s", *adminPath)
	log.Printf("  Database:    %s", cfg.DBPath)
	if cfg.Debug {
		log.Printf("  ⚠️  DEBUG MODE: requests will be intercepted and dumped to %q; nothing is sent to Dify", cfg.DebugDir)
	}
	log.Printf("  Listen Addr:   %s", cfg.ListenAddr)
	log.Printf("  Site Base URL: %s (admin site: %s)", cfg.Admin.SiteBaseURL, cfg.Admin.AdminHost)
	if cfg.CreditsLogoPath != "" {
		if _, err := os.Stat(cfg.CreditsLogoPath); os.IsNotExist(err) {
			log.Printf("  ⚠️  CREDITS_LOGO_PATH points to a non-existent file: %s (logo will not display)", cfg.CreditsLogoPath)
		}
	}
	log.Printf("  Limits: chat_in_flight=%d chat_body=%dMiB web_body=%dKiB dify_response=%dMiB sse_buffer=%dMiB probes=%d login_lock=%dmin",
		cfg.MaxChatInFlight, cfg.MaxRequestBodyMB, cfg.MaxWebRequestBodyKB, cfg.DifyMaxResponseMB,
		cfg.SSEBufferMB, cfg.DifyProbeInFlight, cfg.LoginLockMin)
	log.Printf("  Trusted proxy CIDRs: %v", cfg.TrustedProxyCIDRs)
	log.Printf("  Private Dify egress allowlist: %v", cfg.DifyEgressAllowlist)
	log.Printf("  Remote content origin allowlist: %v", cfg.RemoteContentOriginAllowlist)
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

	cleanupCtx, cancelCleanup := context.WithCancel(context.Background())
	cleanupDone := startCleanupWorker(cleanupCtx, store, time.Duration(cfg.RequestLogCleanupIntervalSec)*time.Second)

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()

	var listenErr error
	serveResultReceived := false
	select {
	case <-signalCtx.Done():
		log.Printf("Received shutdown signal, draining for up to %d seconds...", cfg.ShutdownTimeoutSec)
	case listenErr = <-serveErr:
		serveResultReceived = true
		if listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			log.Printf("Server stopped unexpectedly: %v", listenErr)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Duration(cfg.ShutdownTimeoutSec)*time.Second)
	shutdownErr := shutdownApplication(shutdownCtx, server, gw, cancelCleanup, cleanupDone)
	shutdownCancel()
	if !serveResultReceived {
		listenErr = <-serveErr
	}
	if closeErr := store.Close(); closeErr != nil {
		shutdownErr = errors.Join(shutdownErr, fmt.Errorf("close database: %w", closeErr))
	}
	if shutdownErr != nil {
		log.Printf("Shutdown completed with warning: %v", shutdownErr)
	}
	if listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
		log.Fatalf("Server error: %v", listenErr)
	}
	log.Println("Server stopped")
}

type shutdownServer interface {
	Shutdown(context.Context) error
	Close() error
}

type shutdownGateway interface {
	Shutdown(context.Context) error
}

// shutdownApplication orders process teardown so no component uses the Store
// after it is closed by main: stop periodic work, drain HTTP handlers, stop
// gateway-owned timers/senders, and wait for cleanup up to the shared deadline.
func shutdownApplication(ctx context.Context, server shutdownServer, gateway shutdownGateway, cancelCleanup context.CancelFunc, cleanupDone <-chan struct{}) error {
	cancelCleanup()
	var errs []error
	if err := server.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("drain HTTP server: %w", err))
		if closeErr := server.Close(); closeErr != nil {
			errs = append(errs, fmt.Errorf("force close HTTP server: %w", closeErr))
		}
	}
	if err := gateway.Shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("stop gateway background work: %w", err))
	}
	select {
	case <-cleanupDone:
	case <-ctx.Done():
		errs = append(errs, fmt.Errorf("wait for cleanup worker: %w", ctx.Err()))
	}
	return errors.Join(errs...)
}

// startCleanupWorker enforces retention and expiry until ctx is cancelled.
// Closing the returned channel means all Store access by this worker ended.
func startCleanupWorker(ctx context.Context, store *db.Store, requestLogInterval time.Duration) <-chan struct{} {
	if requestLogInterval <= 0 {
		requestLogInterval = time.Duration(config.DefaultRequestLogCleanupIntervalSec) * time.Second
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		purgeRequestLogs := func() {
			logsDeleted, alertsDeleted, err := store.PurgeExpiredRequestLogs(time.Now().Unix())
			if err != nil {
				log.Printf("[CLEANUP] request logs: %v", err)
			} else if logsDeleted+alertsDeleted > 0 {
				log.Printf("[CLEANUP] purged %d request logs and %d bound alerts", logsDeleted, alertsDeleted)
			}
		}
		runCleanup := func() {
			now := time.Now().Unix()
			if err := store.MaintainActivity(time.Unix(now, 0)); err != nil {
				log.Printf("[CLEANUP] activity: %v", err)
			}
			donationsExpired, donationErr := store.ExpireOverdueDonations(now)
			if donationErr != nil {
				log.Printf("[CLEANUP] donations: %v", donationErr)
			}
			logsDeleted, alertsDeleted, logErr := store.PurgeExpiredRequestLogs(now)
			if logErr != nil {
				log.Printf("[CLEANUP] request logs: %v", logErr)
			}
			sessionsDeleted, sessionErr := store.PurgeExpiredSessions()
			if sessionErr != nil {
				log.Printf("[CLEANUP] sessions: %v", sessionErr)
			}
			reservationsDeleted, reservationErr := store.PurgeSettledCharityReservations(now - int64((30 * 24 * time.Hour).Seconds()))
			if reservationErr != nil {
				log.Printf("[CLEANUP] charity reservations: %v", reservationErr)
			}
			log.Printf("[CLEANUP] expired %d donations; purged %d request logs, %d bound alerts, %d sessions, %d charity reservations",
				donationsExpired, logsDeleted, alertsDeleted, sessionsDeleted, reservationsDeleted)
		}
		select {
		case <-ctx.Done():
			return
		default:
			runCleanup()
		}
		cleanupTicker := time.NewTicker(24 * time.Hour)
		expiryTicker := time.NewTicker(time.Minute)
		requestLogTicker := time.NewTicker(requestLogInterval)
		defer cleanupTicker.Stop()
		defer expiryTicker.Stop()
		defer requestLogTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-cleanupTicker.C:
				runCleanup()
			case <-requestLogTicker.C:
				purgeRequestLogs()
			case <-expiryTicker.C:
				now := time.Now()
				if err := store.MaintainActivity(now); err != nil {
					log.Printf("[CLEANUP] activity: %v", err)
				}
				expired, err := store.ExpireOverdueDonations(now.Unix())
				if err != nil {
					log.Printf("[CLEANUP] donations: %v", err)
				} else if expired > 0 {
					log.Printf("[CLEANUP] expired %d overdue donations", expired)
				}
			}
		}
	}()
	return done
}

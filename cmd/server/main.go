package main

import (
	"context"
	"errors"
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jvanrhyn/disapyr-link/internal/config"
	"github.com/jvanrhyn/disapyr-link/internal/db"
	"github.com/jvanrhyn/disapyr-link/internal/handler"
	applogger "github.com/jvanrhyn/disapyr-link/internal/logger"
	"github.com/jvanrhyn/disapyr-link/internal/repository"
	"github.com/jvanrhyn/disapyr-link/internal/service"
	"github.com/jvanrhyn/disapyr-link/web"
)

func main() {
	printConfig := flag.Bool("print-config", false, "print resolved configuration and exit")
	flag.Parse()

	cfg := config.Load()

	if *printConfig {
		cfg.Print(os.Stdout)
		os.Exit(0)
	}

	// Structured JSON logging.
	level := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		level = slog.LevelDebug
	}
	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	log := slog.New(jsonHandler)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Database.
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Upgrade logger: fan out to both stdout JSON and the app_logs DB table.
	dbLogLevel := parseLogLevel(cfg.LogDBMinLevel)
	dbHandler := applogger.NewDBHandler(pool, dbLogLevel)
	defer dbHandler.Stop() // drain buffer on graceful shutdown
	log = slog.New(applogger.NewMultiHandler(jsonHandler, dbHandler))
	slog.SetDefault(log)

	log.Info("database connected")

	// Layers.
	repo := repository.New(pool)
	svc := service.New(repo, cfg.MaxSecretBytes)

	// web.FS embeds "templates" and "static" at its root (relative to web/web.go).
	h, err := handler.New(svc, log, cfg.MaxSecretBytes, web.FS)
	if err != nil {
		log.Error("init handlers", "err", err)
		os.Exit(1)
	}

	admin, err := handler.NewAdminHandler(pool, log, cfg.AdminUser, cfg.AdminPassword, web.FS)
	if err != nil {
		log.Error("init admin handler", "err", err)
		os.Exit(1)
	}

	// Background worker: delete expired secrets every 15 minutes.
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := repo.CleanupExpired(ctx); err != nil {
					log.Warn("expired secret cleanup failed", "err", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Router (Go 1.22+ pattern matching).
	createLimiter := handler.NewRateLimiter(cfg.RateLimitCreatePerMin, 5, cfg.TrustProxy, log)
	revealLimiter := handler.NewRateLimiter(cfg.RateLimitRevealPerMin, 10, cfg.TrustProxy, log)
	healthLimiter := handler.NewRateLimiter(cfg.RateLimitHealthPerMin, 3, cfg.TrustProxy, log)

	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS(web.FS)))))
	mux.HandleFunc("GET /", h.ServeIndex)
	mux.Handle("POST /", createLimiter.Limit(http.HandlerFunc(h.CreateSecret)))
	mux.HandleFunc("GET /s/{token}", h.ServePage)
	mux.Handle("POST /s/{token}/reveal", revealLimiter.Limit(http.HandlerFunc(h.RevealSecret)))
	mux.Handle("GET /health", healthLimiter.Limit(admin.BasicAuth(admin.ServeHealth)))
	mux.Handle("GET /health/logs", healthLimiter.Limit(admin.BasicAuth(admin.ServeHealthLogs)))
	mux.Handle("POST /health/logs/clear", healthLimiter.Limit(admin.BasicAuth(admin.ClearLogs)))

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler.Middleware(log)(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server.
	go func() {
		log.Info("server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown error", "err", err)
	}
	log.Info("server stopped")
}

// staticFS returns a sub-FS scoped to the "static" directory.
func staticFS(webFS fs.FS) fs.FS {
	sub, _ := fs.Sub(webFS, "static")
	return sub
}

// parseLogLevel converts a level string to slog.Level. Defaults to Warn.
func parseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "error":
		return slog.LevelError
	default:
		return slog.LevelWarn
	}
}

package main

import (
	"context"
	"errors"
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
	"github.com/jvanrhyn/disapyr-link/internal/repository"
	"github.com/jvanrhyn/disapyr-link/internal/service"
	"github.com/jvanrhyn/disapyr-link/web"
)

func main() {
	cfg := config.Load()

	// Structured JSON logging.
	level := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
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

	// Background worker: delete expired secrets every 15 minutes.
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := pool.Exec(ctx, `DELETE FROM secrets WHERE expires_at IS NOT NULL AND expires_at < NOW()`); err != nil {
					log.Warn("expired secret cleanup", "err", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Router (Go 1.22+ pattern matching).
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS(web.FS)))))
	mux.HandleFunc("GET /", h.ServeIndex)
	mux.HandleFunc("POST /", h.CreateSecret)
	mux.HandleFunc("GET /s/{token}", h.ServePage)
	mux.HandleFunc("POST /s/{token}/reveal", h.RevealSecret)

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

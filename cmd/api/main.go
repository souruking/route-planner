package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/souruking/route-planner/internal/httpapi"
	"github.com/souruking/route-planner/internal/store"
)

type config struct {
	databaseURL   string
	httpAddr      string
	runMigrations bool
}

func requiredEnv(key string) (string, error) {
	value, exists := os.LookupEnv(key)
	if !exists || value == "" {
		return "", fmt.Errorf("required environment variable %s is not set", key)
	}
	return value, nil
}

func loadConfig() (config, error) {
	var cfg config
	var err error

	cfg.databaseURL, err = requiredEnv("DB_URL")
	if err != nil {
		return cfg, err
	}

	cfg.httpAddr, err = requiredEnv("HTTP_ADDR")
	if err != nil {
		return cfg, err
	}

	if value := os.Getenv("RUN_MIGRATIONS"); value != "" {
		cfg.runMigrations, err = strconv.ParseBool(value)
		if err != nil {
			return cfg, fmt.Errorf("invalid RUN_MIGRATIONS value %q: %w", value, err)
		}
	}

	return cfg, nil
}

func run(logger *slog.Logger) error {
	// Optional .env file; variables already set in the environment take precedence.
	if err := godotenv.Load(); err != nil {
		logger.Info("no .env file found; using system environment variables")
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// Cancelled on SIGINT/SIGTERM to trigger graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Connect to PostgreSQL with a startup timeout.
	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	pool, err := store.Connect(connectCtx, cfg.databaseURL)
	cancel()
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	logger.Info("database connected")

	if cfg.runMigrations {
		migrateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := store.Migrate(migrateCtx, pool)
		cancel()
		if err != nil {
			return fmt.Errorf("run migrations: %w", err)
		}
		logger.Info("database migrations completed")
	}

	api := httpapi.New(pool, store.New(pool), logger)

	server := &http.Server{
		Addr:              cfg.httpAddr,
		Handler:           api.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	serverErr := make(chan error, 1)

	go func() {
		logger.Info("HTTP server listening", "addr", cfg.httpAddr)

		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	// Wait for a shutdown signal or a server failure.
	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("HTTP server failed: %w", err)
		}
		return nil

	case <-ctx.Done():
		// Restore default signal handling so a second Ctrl+C kills the process
		// immediately if shutdown hangs.
		stop()
		logger.Info("shutting down HTTP server")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down HTTP server: %w", err)
		}

		logger.Info("HTTP server stopped")
		return nil
	}
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

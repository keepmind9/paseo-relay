package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const shutdownTimeout = 10 * time.Second

func main() {
	listenAddr := flag.String("listen", "", "Listen address (default: 0.0.0.0:8080)")
	tlsCert := flag.String("tls-cert", "", "TLS certificate file path")
	tlsKey := flag.String("tls-key", "", "TLS private key file path")
	logLevel := flag.String("log-level", "", "Log level: debug, info, warn, error")
	configPath := flag.String("config", "", "Config file path")
	flag.Parse()

	cfg := ResolveConfig(*listenAddr, *tlsCert, *tlsKey, *logLevel, *configPath)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))

	hub := NewSessionHub(logger)
	relayHandler := NewRelayServer(hub, logger)

	httpServer := &http.Server{
		Addr:    cfg.Listen,
		Handler: relayHandler,
	}

	var reloader *TLSReloader

	if cfg.TLS.Enabled {
		var err error
		reloader, err = NewTLSReloader(cfg.TLS.Cert, cfg.TLS.Key, logger)
		if err != nil {
			logger.Error("failed to load TLS certs", "error", err)
			os.Exit(1)
		}
		httpServer.TLSConfig = reloader.TLSConfig()
	}

	// Start session cleanup goroutine
	cleanupStop := hub.StartCleanup(60*time.Second, 5*time.Minute, logger)

	// Start server in a goroutine
	go func() {
		var err error
		if cfg.TLS.Enabled {
			err = httpServer.ListenAndServeTLS("", "")
		} else {
			err = httpServer.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	logger.Info("starting relay server", "listen", cfg.Listen, "tls", cfg.TLS.Enabled)

	// Handle SIGHUP for TLS cert reload
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)
	go func() {
		for range sigCh {
			if reloader != nil {
				logger.Info("received SIGHUP, reloading TLS certificates")
				reloader.Reload()
			}
		}
	}()

	// Wait for shutdown signal
	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-sigCtx.Done()
	logger.Info("shutting down gracefully", "timeout", shutdownTimeout)

	// Stop signal handlers
	signal.Stop(sigCh)

	// Stop session cleanup
	close(cleanupStop)

	// Close all active sessions
	hub.CloseAll()

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}

func parseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

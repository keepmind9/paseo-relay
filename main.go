package main

import (
	"crypto/tls"
	"flag"
	"log/slog"
	"net/http"
	"os"
)

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
	srv := NewRelayServer(hub, logger)

	logger.Info("starting relay server", "listen", cfg.Listen, "tls", cfg.TLS.Enabled)

	if cfg.TLS.Enabled {
		cert, err := tls.LoadX509KeyPair(cfg.TLS.Cert, cfg.TLS.Key)
		if err != nil {
			logger.Error("failed to load TLS certs", "error", err)
			os.Exit(1)
		}
		server := &http.Server{
			Addr:    cfg.Listen,
			Handler: srv,
			TLSConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
			},
		}
		if err := server.ListenAndServeTLS("", ""); err != nil {
			logger.Error("TLS server error", "error", err)
			os.Exit(1)
		}
	} else {
		if err := http.ListenAndServe(cfg.Listen, srv); err != nil {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}
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

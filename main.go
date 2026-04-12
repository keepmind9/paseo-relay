package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net/http"
)

func main() {
	listenAddr := flag.String("listen", "", "Listen address (default: 0.0.0.0:8080)")
	tlsCert := flag.String("tls-cert", "", "TLS certificate file path")
	tlsKey := flag.String("tls-key", "", "TLS private key file path")
	logLevel := flag.String("log-level", "", "Log level: debug, info, warn, error")
	configPath := flag.String("config", "", "Config file path")
	flag.Parse()

	cfg := ResolveConfig(*listenAddr, *tlsCert, *tlsKey, *logLevel, *configPath)

	hub := NewSessionHub()
	srv := NewRelayServer(hub)

	log.Printf("[relay] starting server on %s (TLS: %v)", cfg.Listen, cfg.TLS.Enabled)

	if cfg.TLS.Enabled {
		cert, err := tls.LoadX509KeyPair(cfg.TLS.Cert, cfg.TLS.Key)
		if err != nil {
			log.Fatalf("[relay] failed to load TLS certs: %v", err)
		}
		server := &http.Server{
			Addr:    cfg.Listen,
			Handler: srv,
			TLSConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
			},
		}
		if err := server.ListenAndServeTLS("", ""); err != nil {
			log.Fatalf("[relay] TLS server error: %v", err)
		}
	} else {
		if err := http.ListenAndServe(cfg.Listen, srv); err != nil {
			log.Fatalf("[relay] server error: %v", err)
		}
	}
}

package main

import (
	"crypto/tls"
	"log/slog"
	"sync"
)

// TLSReloader manages hot-reloading of TLS certificates.
// It holds the current certificate in memory and provides a GetCertificate
// callback for tls.Config, plus a Reload method to update from disk.
type TLSReloader struct {
	mu     sync.RWMutex
	cert   *tls.Certificate
	certFile string
	keyFile  string
	logger *slog.Logger
}

// NewTLSReloader creates a reloader and loads the initial certificate pair.
func NewTLSReloader(certFile, keyFile string, logger *slog.Logger) (*TLSReloader, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}

	return &TLSReloader{
		cert:     &cert,
		certFile: certFile,
		keyFile:  keyFile,
		logger:   logger,
	}, nil
}

// GetCertificate returns the current certificate for TLS handshakes.
// Intended to be used as tls.Config.GetCertificate.
func (r *TLSReloader) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cert, nil
}

// Reload loads the certificate pair from disk and swaps it in.
// Existing connections are not affected — only new TLS handshakes
// will use the new certificate.
func (r *TLSReloader) Reload() error {
	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		r.logger.Error("failed to reload TLS certs", "error", err)
		return err
	}

	r.mu.Lock()
	r.cert = &cert
	r.mu.Unlock()

	r.logger.Info("TLS certificates reloaded", "cert", r.certFile, "key", r.keyFile)
	return nil
}

// TLSConfig returns a *tls.Config that uses this reloader for certificate lookup.
func (r *TLSReloader) TLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: r.GetCertificate,
	}
}

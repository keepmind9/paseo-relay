package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, "0.0.0.0:8080", cfg.Listen)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.False(t, cfg.TLS.Enabled)
}

func TestLoadConfigFromFile(t *testing.T) {
	yamlContent := []byte(`
listen: "0.0.0.0:9090"
log_level: "debug"
tls:
  enabled: true
  cert: "/path/to/cert.pem"
  key: "/path/to/key.pem"
`)
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write(yamlContent)
	require.NoError(t, err)
	tmpFile.Close()

	cfg, err := LoadConfigFromFile(tmpFile.Name())
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0:9090", cfg.Listen)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.True(t, cfg.TLS.Enabled)
	assert.Equal(t, "/path/to/cert.pem", cfg.TLS.Cert)
	assert.Equal(t, "/path/to/key.pem", cfg.TLS.Key)
}

func TestLoadConfigFromFileNotFound(t *testing.T) {
	_, err := LoadConfigFromFile("/nonexistent/config.yaml")
	assert.Error(t, err)
}

func TestApplyEnvOverrides(t *testing.T) {
	cfg := DefaultConfig()

	t.Setenv("PASEO_LISTEN", "0.0.0.0:3000")
	t.Setenv("PASEO_LOG_LEVEL", "warn")
	t.Setenv("PASEO_TLS_CERT", "/env/cert.pem")
	t.Setenv("PASEO_TLS_KEY", "/env/key.pem")

	ApplyEnvOverrides(&cfg)
	assert.Equal(t, "0.0.0.0:3000", cfg.Listen)
	assert.Equal(t, "warn", cfg.LogLevel)
	assert.Equal(t, "/env/cert.pem", cfg.TLS.Cert)
	assert.Equal(t, "/env/key.pem", cfg.TLS.Key)
	assert.True(t, cfg.TLS.Enabled)
}

func TestResolveConfigPrecedence(t *testing.T) {
	cfg := ResolveConfig("0.0.0.0:7070", "", "", "debug", "")
	assert.Equal(t, "0.0.0.0:7070", cfg.Listen)
	assert.Equal(t, "debug", cfg.LogLevel)
}

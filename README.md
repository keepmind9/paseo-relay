# paseo-relay

A standalone Go relay server for [Paseo](https://github.com/getpaseo/paseo), fully compatible with the original Paseo relay protocol (v1 and v2).

The relay bridges WebSocket connections between the Paseo daemon (running on your machine) and mobile/desktop clients. It forwards encrypted traffic without inspecting content — all E2EE is handled end-to-end by the daemon and client.

## Why

The official Paseo relay runs on Cloudflare Workers. This project lets you self-host a relay on your own infrastructure without depending on Cloudflare.

## Features

- Full v1 and v2 protocol compatibility with the original relay
- Multiplexed connections — multiple clients per session
- Frame buffering (200 frames) for late-joining daemons
- Two-phase nudge/reset for unresponsive daemon detection
- TLS with hot-reload via SIGHUP (zero-downtime cert rotation)
- Graceful shutdown with 10s timeout
- Idle session cleanup (auto-reap after 5 minutes)
- Zero external dependencies beyond WebSocket and YAML libs
- Single static binary, easy to deploy

## Install

```bash
go build -o paseo-relay .
```

Or with Make:

```bash
make build
```

### Docker

```bash
docker build -t paseo-relay .
docker run -p 8080:8080 paseo-relay

# With TLS
docker run -p 443:8080 \
  -v /path/to/certs:/certs:ro \
  paseo-relay \
  --tls-cert /certs/cert.pem --tls-key /certs/key.pem
```

## Usage

```bash
# Start on default port 8080
./paseo-relay

# Custom listen address
./paseo-relay --listen 0.0.0.0:9090

# With TLS
./paseo-relay --tls-cert /path/to/cert.pem --tls-key /path/to/key.pem

# With config file
./paseo-relay --config /path/to/config.yaml  # see config.example.yaml
```

### Configuration

Sources (priority: flags > env > config file > defaults):

| Flag | Env | Default | Description |
|---|---|---|---|
| `--listen` | `PASEO_LISTEN` | `0.0.0.0:8080` | Listen address |
| `--tls-cert` | `PASEO_TLS_CERT` | — | TLS certificate path |
| `--tls-key` | `PASEO_TLS_KEY` | — | TLS private key path |
| `--log-level` | `PASEO_LOG_LEVEL` | `info` | Log level: debug, info, warn, error |
| `--config` | — | — | Config file path |

See [config.example.yaml](config.example.yaml) for a fully commented example.

Example `config.yaml`:

```yaml
listen: "0.0.0.0:8080"
log_level: "info"
tls:
  enabled: false
  cert: ""
  key: ""
```

### TLS Certificate Hot-Reload

Send `SIGHUP` to reload certificates without restarting:

```bash
kill -HUP $(pgrep paseo-relay)
```

Works with certbot: `certbot renew --deploy-hook "kill -HUP $(cat /run/paseo-relay.pid)"`

## Protocol

| Endpoint | Description |
|---|---|
| `GET /health` | Health check, returns `{"status":"ok"}` |
| `GET /ws` | WebSocket upgrade endpoint |

### WebSocket parameters

| Param | Required | Description |
|---|---|---|
| `serverId` | yes | Identifies the daemon session |
| `role` | yes | `server` or `client` |
| `v` | no | Protocol version: `1` or `2` (default: `1`) |
| `connectionId` | no | Per-client routing ID (required for v2 data sockets) |

### v2 connection flow

```
Daemon                          Relay                         Client
  │                               │                              │
  │  WS /ws?role=server&v=2       │                              │
  │  (control socket)              │                              │
  │──────────────────────────────►│                              │
  │  ◄── {type:"sync",...}        │                              │
  │                               │  WS /ws?role=client&v=2      │
  │                               │◄─────────────────────────────│
  │  ◄── {type:"connected",...}   │                              │
  │                               │                              │
  │  WS /ws?role=server&          │                              │
  │  connectionId=abc&v=2         │                              │
  │──────────────────────────────►│                              │
  │                               │  (E2EE handshake happens     │
  │                               │   over the relay — relay     │
  │                               │   cannot read content)       │
  │  ◄───── encrypted data ──────►│◄───── encrypted data ──────►│
```

## Configure Paseo daemon to use your relay

When pairing via QR code or link, the daemon embeds the relay endpoint in the connection offer. Update your daemon config to point to your self-hosted relay:

```
relay.paseo.sh:443  →  your-relay-host:8080
```

For TLS, use a reverse proxy like nginx or Caddy in front of the relay.

## Development

```bash
make build        # Build binary
make test         # Run tests
make fmt          # Format code
make clean        # Remove binary
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

[MIT](LICENSE)

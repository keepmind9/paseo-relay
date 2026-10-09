package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthEndpoint(t *testing.T) {
	hub := NewSessionHub(testLogger)
	srv := NewRelayServer(hub, testLogger)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
}

func TestWebSocketUpgradeMissingServerId(t *testing.T) {
	hub := NewSessionHub(testLogger)
	srv := NewRelayServer(hub, testLogger)

	req := httptest.NewRequest(http.MethodGet, "/ws?role=server&v=2", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Missing serverId")
}

func TestWebSocketUpgradeInvalidRole(t *testing.T) {
	hub := NewSessionHub(testLogger)
	srv := NewRelayServer(hub, testLogger)

	req := httptest.NewRequest(http.MethodGet, "/ws?serverId=test&role=invalid&v=2", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Missing or invalid role")
}

func TestWebSocketUpgradeInvalidVersion(t *testing.T) {
	hub := NewSessionHub(testLogger)
	srv := NewRelayServer(hub, testLogger)

	req := httptest.NewRequest(http.MethodGet, "/ws?serverId=test&role=server&v=99", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid v parameter")
}

func TestWebSocketV2FullFlow(t *testing.T) {
	hub := NewSessionHub(testLogger)
	srv := NewRelayServer(hub, testLogger)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// 1. Daemon connects control socket
	controlWs, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws?serverId=test-flow&role=server&v=2", nil)
	require.NoError(t, err)
	defer controlWs.Close()

	// Read initial sync (may be empty)
	controlWs.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := controlWs.ReadMessage()
	require.NoError(t, err)
	var syncMsg ControlMessage
	require.NoError(t, json.Unmarshal(msg, &syncMsg))
	assert.Equal(t, "sync", syncMsg.Type)

	// 2. Client connects
	clientWs, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws?serverId=test-flow&role=client&connectionId=conn_test1&v=2", nil)
	require.NoError(t, err)
	defer clientWs.Close()

	// 3. Control receives "connected"
	controlWs.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err = controlWs.ReadMessage()
	require.NoError(t, err)
	var connMsg ControlMessage
	require.NoError(t, json.Unmarshal(msg, &connMsg))
	assert.Equal(t, "connected", connMsg.Type)
	require.NotNil(t, connMsg.ConnectionID)
	assert.Equal(t, "conn_test1", *connMsg.ConnectionID)

	// 4. Daemon opens data socket
	dataWs, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws?serverId=test-flow&role=server&connectionId=conn_test1&v=2", nil)
	require.NoError(t, err)
	defer dataWs.Close()

	// 5. Send ping on control, expect pong
	require.NoError(t, controlWs.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`)))
	controlWs.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err = controlWs.ReadMessage()
	require.NoError(t, err)
	var pongMsg ControlMessage
	require.NoError(t, json.Unmarshal(msg, &pongMsg))
	assert.Equal(t, "pong", pongMsg.Type)
	assert.NotNil(t, pongMsg.Ts)

	// 6. Client sends data -> forwarded to daemon data socket
	require.NoError(t, clientWs.WriteMessage(websocket.TextMessage, []byte("hello from client")))
	dataWs.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err = dataWs.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "hello from client", string(msg))

	// 7. Daemon data socket sends -> forwarded to client
	require.NoError(t, dataWs.WriteMessage(websocket.TextMessage, []byte("hello from daemon")))
	clientWs.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err = clientWs.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "hello from daemon", string(msg))
}

// TestControlSyncAlwaysCarriesConnectionIdsArray mirrors the daemon's
// ready-handshake validation: a sync message only counts as valid when
// connectionIds is a JSON array (an empty list included). ControlMessage
// serializes with omitempty, so an empty list used to drop the field
// entirely and the daemon discarded the sync, hit its 8s ready timeout and
// terminated the socket without a close frame — surfacing on the relay as
// close 1006 in a 38-second reconnect loop for any idle daemon.
func TestControlSyncAlwaysCarriesConnectionIdsArray(t *testing.T) {
	tests := []struct {
		name         string
		prepare      func(t *testing.T, wsURL string) (cleanup func())
		wantSyncData string
	}{
		{
			name:         "idle daemon with no clients",
			prepare:      nil,
			wantSyncData: "[]",
		},
		{
			name: "daemon reconnecting with an existing client",
			prepare: func(t *testing.T, wsURL string) func() {
				clientWs, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws?serverId=sync-shape&role=client&connectionId=conn_1&v=2", nil)
				require.NoError(t, err)
				return func() { clientWs.Close() }
			},
			wantSyncData: `["conn_1"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := NewSessionHub(testLogger)
			srv := NewRelayServer(hub, testLogger)
			ts := httptest.NewServer(srv)
			defer ts.Close()

			wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

			if tt.prepare != nil {
				cleanup := tt.prepare(t, wsURL)
				defer cleanup()
			}

			controlWs, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws?serverId=sync-shape&role=server&v=2", nil)
			require.NoError(t, err)
			defer controlWs.Close()

			controlWs.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, msg, err := controlWs.ReadMessage()
			require.NoError(t, err)

			// Raw-message check: unmarshal into any or a struct would lose
			// the distinction between a missing field and [].
			var wire struct {
				Type          string          `json:"type"`
				ConnectionIDs json.RawMessage `json:"connectionIds"`
			}
			require.NoError(t, json.Unmarshal(msg, &wire))
			assert.Equal(t, "sync", wire.Type)
			assert.Equal(t, tt.wantSyncData, string(wire.ConnectionIDs),
				"daemon requires Array.isArray(connectionIds) on every sync")
		})
	}
}

func TestWebSocketV1Flow(t *testing.T) {
	hub := NewSessionHub(testLogger)
	srv := NewRelayServer(hub, testLogger)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Server connects
	serverWs, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws?serverId=v1-test&role=server&v=1", nil)
	require.NoError(t, err)
	defer serverWs.Close()

	// Client connects
	clientWs, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws?serverId=v1-test&role=client&v=1", nil)
	require.NoError(t, err)
	defer clientWs.Close()

	// Client -> Server
	require.NoError(t, clientWs.WriteMessage(websocket.TextMessage, []byte("hello v1")))
	serverWs.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := serverWs.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "hello v1", string(raw))

	// Server -> Client
	require.NoError(t, serverWs.WriteMessage(websocket.TextMessage, []byte("reply v1")))
	clientWs.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err = clientWs.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "reply v1", string(raw))
}

func TestBufferingBeforeDataSocket(t *testing.T) {
	hub := NewSessionHub(testLogger)
	srv := NewRelayServer(hub, testLogger)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Control socket
	controlWs, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws?serverId=buf-test&role=server&v=2", nil)
	require.NoError(t, err)
	defer controlWs.Close()

	// Drain sync
	controlWs.SetReadDeadline(time.Now().Add(2 * time.Second))
	controlWs.ReadMessage()

	// Client connects and sends messages BEFORE daemon data socket exists
	clientWs, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws?serverId=buf-test&role=client&connectionId=conn_buf&v=2", nil)
	require.NoError(t, err)
	defer clientWs.Close()

	// Drain connected
	controlWs.SetReadDeadline(time.Now().Add(2 * time.Second))
	controlWs.ReadMessage()

	require.NoError(t, clientWs.WriteMessage(websocket.TextMessage, []byte("buffered_1")))
	require.NoError(t, clientWs.WriteMessage(websocket.TextMessage, []byte("buffered_2")))

	// Now daemon opens data socket — should receive buffered messages
	dataWs, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws?serverId=buf-test&role=server&connectionId=conn_buf&v=2", nil)
	require.NoError(t, err)
	defer dataWs.Close()

	dataWs.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := dataWs.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "buffered_1", string(raw))

	dataWs.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err = dataWs.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "buffered_2", string(raw))
}

func TestClientDisconnectClosesDataSocket(t *testing.T) {
	hub := NewSessionHub(testLogger)
	srv := NewRelayServer(hub, testLogger)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Control
	controlWs, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws?serverId=disc-test&role=server&v=2", nil)
	require.NoError(t, err)
	defer controlWs.Close()
	controlWs.SetReadDeadline(time.Now().Add(2 * time.Second))
	controlWs.ReadMessage() // drain sync

	// Client
	clientWs, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws?serverId=disc-test&role=client&connectionId=conn_disc&v=2", nil)
	require.NoError(t, err)
	controlWs.SetReadDeadline(time.Now().Add(2 * time.Second))
	controlWs.ReadMessage() // drain connected

	// Data socket
	dataWs, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws?serverId=disc-test&role=server&connectionId=conn_disc&v=2", nil)
	require.NoError(t, err)

	// Close client
	clientWs.Close()

	// Data socket should be closed by the relay
	dataWs.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err = dataWs.ReadMessage()
	assert.Error(t, err, "data socket should be closed after client disconnect")
	dataWs.Close()
}

func TestResolveVersion(t *testing.T) {
	assert.Equal(t, "1", resolveVersion(""))
	assert.Equal(t, "1", resolveVersion("1"))
	assert.Equal(t, "2", resolveVersion("2"))
	assert.Equal(t, "", resolveVersion("3"))
	assert.Equal(t, "", resolveVersion("nope"))
}

func TestGenerateConnectionID(t *testing.T) {
	id := generateConnectionID()
	assert.True(t, strings.HasPrefix(id, "conn_"))
	assert.Equal(t, 21, len(id), "conn_ + 16 hex chars")
}

// recordingHandler captures slog records so tests can assert on log levels.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *recordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(name string) slog.Handler       { return h }

// recordsMatching returns the messages of captured records at the given level
// whose message equals msg.
func (h *recordingHandler) recordsMatching(level slog.Level, msg string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var matched []string
	for _, r := range h.records {
		if r.Level == level && r.Message == msg {
			matched = append(matched, r.Message)
		}
	}
	return matched
}

// TestReadPumpLogsGracefulCloseAsInfo covers the log classification of
// readPump teardown: a peer-initiated graceful close (close frame with 1000,
// 1001 or 1005) is part of the connection lifecycle and logs at Info, while
// an abrupt teardown without a close frame (abnormal closure) logs at Warn.
func TestReadPumpLogsGracefulCloseAsInfo(t *testing.T) {
	tests := []struct {
		name         string
		closeCode    int
		wantLogLevel slog.Level
	}{
		{name: "normal closure 1000", closeCode: websocket.CloseNormalClosure, wantLogLevel: slog.LevelInfo},
		{name: "going away 1001", closeCode: websocket.CloseGoingAway, wantLogLevel: slog.LevelInfo},
		{name: "no status 1005", closeCode: websocket.CloseNoStatusReceived, wantLogLevel: slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &recordingHandler{}
			logger := slog.New(handler)
			hub := NewSessionHub(logger)
			srv := NewRelayServer(hub, logger)
			ts := httptest.NewServer(srv)
			defer ts.Close()

			wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
			clientWs, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws?serverId=close-log&role=server&v=2", nil)
			require.NoError(t, err)

			// Peer sends a close frame with the code under test, then drops.
			require.NoError(t, clientWs.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(tt.closeCode, "bye"), time.Now().Add(time.Second)))
			clientWs.Close()

			deadline := time.Now().Add(3 * time.Second)
			// Poll for the teardown record itself, not just any record at the
			// expected level: the "WebSocket connected" handshake record is
			// also logged at Info, which would mask a misclassified close.
			for time.Now().Before(deadline) {
				if msgs := handler.recordsMatching(tt.wantLogLevel, "connection closed"); len(msgs) > 0 {
					return // teardown classified as expected
				}
				if warns := handler.recordsMatching(slog.LevelWarn, "connection lost"); len(warns) > 0 {
					t.Fatalf("graceful close logged as Warn: %v", warns)
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatalf("no \"connection closed\" log at level %v within timeout", tt.wantLogLevel)
		})
	}
}

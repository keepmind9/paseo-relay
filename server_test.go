package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

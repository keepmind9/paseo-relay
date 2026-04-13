package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	readTimeout = 60 * time.Second
	nudgeDelay  = 10 * time.Second
	nudgeSecond = 5 * time.Second
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// RelayServer is the HTTP handler for the relay.
type RelayServer struct {
	hub    *SessionHub
	logger *slog.Logger
}

// NewRelayServer creates a new relay server backed by the given hub.
func NewRelayServer(hub *SessionHub, logger *slog.Logger) *RelayServer {
	return &RelayServer{hub: hub, logger: logger}
}

// ServeHTTP routes requests to /health or /ws.
func (rs *RelayServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/health":
		rs.handleHealth(w, r)
	case "/ws":
		rs.handleWebSocket(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (rs *RelayServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	data, _ := json.Marshal(map[string]string{"status": "ok"})
	w.Write(data)
}

func (rs *RelayServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	serverID := q.Get("serverId")
	if serverID == "" {
		http.Error(w, "Missing serverId parameter", http.StatusBadRequest)
		return
	}

	role := q.Get("role")
	if role != "server" && role != "client" {
		http.Error(w, "Missing or invalid role parameter", http.StatusBadRequest)
		return
	}

	version := resolveVersion(q.Get("v"))
	if version == "" {
		http.Error(w, "Invalid v parameter (expected 1 or 2)", http.StatusBadRequest)
		return
	}

	connectionID := strings.TrimSpace(q.Get("connectionId"))

	// v2 client without connectionId gets one assigned
	if version == "2" && role == "client" && connectionID == "" {
		connectionID = generateConnectionID()
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		rs.logger.Error("WebSocket upgrade failed",
			"serverId", serverID,
			"role", role,
			"version", version,
			"connectionId", connectionID,
			"error", err,
		)
		return
	}

	rs.logger.Info("WebSocket connected",
		"serverId", serverID,
		"role", role,
		"version", version,
		"connectionId", connectionID,
		"remoteAddr", ws.RemoteAddr().String(),
	)

	conn := &ClientConn{
		Ws:           ws,
		Role:         ConnectionRole(role),
		Version:      ProtocolVersion(version),
		ServerID:     serverID,
		ConnectionID: connectionID,
		CreatedAt:    time.Now(),
	}

	session := rs.hub.GetOrCreateSession(serverID)

	switch {
	case version == "1":
		if conn.Role == RoleServer {
			session.SetV1Server(conn)
		} else {
			session.SetV1Client(conn)
		}
		go rs.readPump(session, conn)
	case conn.IsControl():
		session.RegisterControl(conn)
		go rs.readPump(session, conn)
	case conn.IsServerData():
		session.RegisterDataSocket(conn)
		go rs.readPump(session, conn)
	case conn.IsClient():
		session.RegisterClient(conn)
		go rs.nudgeOrResetControl(session, connectionID)
		go rs.readPump(session, conn)
	}
}

// readPump reads messages from a WebSocket and dispatches them.
func (rs *RelayServer) readPump(session *Session, conn *ClientConn) {
	defer func() {
		conn.Close()
		rs.handleDisconnect(session, conn)
	}()

	conn.Ws.SetReadDeadline(time.Now().Add(readTimeout))

	for {
		msgType, data, err := conn.Ws.ReadMessage()
		if err != nil {
			isNormalClose := websocket.IsCloseError(err, websocket.CloseNormalClosure)
			isPeerClosed := strings.Contains(err.Error(), "use of closed network connection")
			if !isNormalClose && !isPeerClosed {
				rs.logger.Warn("readPump error",
					"serverId", conn.ServerID,
					"role", string(conn.Role),
					"version", string(conn.Version),
					"connectionId", conn.ConnectionID,
					"error", err,
				)
			}
			return
		}

		conn.Ws.SetReadDeadline(time.Now().Add(readTimeout))

		switch {
		case conn.Version == "1":
			rs.handleV1Message(session, conn, msgType, data)
		case conn.IsControl():
			if msgType == websocket.TextMessage {
				session.HandleControlMessage(conn, string(data))
			}
		case conn.IsClient():
			session.HandleClientMessage(conn.ConnectionID, msgType, data)
		case conn.IsServerData():
			session.HandleDataMessage(conn.ConnectionID, msgType, data)
		}
	}
}

// handleV1Message forwards v1 messages to the opposite role.
func (rs *RelayServer) handleV1Message(session *Session, conn *ClientConn, msgType int, data []byte) {
	if conn.Role == RoleServer {
		if client := session.GetV1Client(); client != nil {
			client.Send(msgType, data)
		}
	} else {
		if server := session.GetV1Server(); server != nil {
			server.Send(msgType, data)
		}
	}
}

// handleDisconnect cleans up when a WebSocket disconnects.
func (rs *RelayServer) handleDisconnect(session *Session, conn *ClientConn) {
	switch {
	case conn.Version == "1":
		if conn.Role == RoleServer {
			session.ClearV1ServerIf(conn)
		} else {
			session.ClearV1ClientIf(conn)
		}
	case conn.IsControl():
		session.ClearControlIf(conn)
	case conn.IsClient():
		session.RemoveClient(conn, conn.ConnectionID)
	case conn.IsServerData():
		session.RemoveDataSocketIf(conn.ConnectionID, conn)
	}
}

// nudgeOrResetControl implements the two-phase timeout from the original relay.
func (rs *RelayServer) nudgeOrResetControl(session *Session, connectionID string) {
	time.Sleep(nudgeDelay)

	if session.HasServerDataSocket(connectionID) {
		return
	}
	if !session.HasClientSocket(connectionID) {
		return
	}

	session.SendSync()

	time.Sleep(nudgeSecond)

	if session.HasServerDataSocket(connectionID) {
		return
	}
	if !session.HasClientSocket(connectionID) {
		return
	}

	rs.logger.Warn("force-closing control socket (daemon unresponsive)", "connectionId", connectionID)
	session.CloseControl()
}

// resolveVersion returns "1" or "2", or "" for invalid.
func resolveVersion(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" || v == "1" {
		return "1"
	}
	if v == "2" {
		return "2"
	}
	return ""
}

// generateConnectionID creates a random connection ID.
func generateConnectionID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "conn_" + hex.EncodeToString(b)
}

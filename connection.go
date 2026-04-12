package main

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ConnectionRole identifies whether a WebSocket belongs to the daemon or client.
type ConnectionRole string

const (
	RoleServer ConnectionRole = "server"
	RoleClient ConnectionRole = "client"
)

// ProtocolVersion is the relay protocol version (1 or 2).
type ProtocolVersion string

const (
	Version1 ProtocolVersion = "1"
	Version2 ProtocolVersion = "2"
)

// ClientConn wraps a WebSocket connection with its relay metadata.
type ClientConn struct {
	Ws           *websocket.Conn
	Role         ConnectionRole
	Version      ProtocolVersion
	ServerID     string
	ConnectionID string // empty for v1 and v2 control sockets
	CreatedAt    time.Time

	mu sync.Mutex
}

// Send writes a message to the WebSocket connection. Thread-safe.
func (c *ClientConn) Send(messageType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Ws.WriteMessage(messageType, data)
}

// Close closes the underlying WebSocket connection.
func (c *ClientConn) Close() error {
	if c.Ws == nil {
		return nil
	}
	return c.Ws.Close()
}

// IsControl returns true if this is a v2 daemon control socket.
func (c *ClientConn) IsControl() bool {
	return c.Version == Version2 && c.Role == RoleServer && c.ConnectionID == ""
}

// IsServerData returns true if this is a v2 daemon per-connection data socket.
func (c *ClientConn) IsServerData() bool {
	return c.Version == Version2 && c.Role == RoleServer && c.ConnectionID != ""
}

// IsClient returns true if this is a client socket.
func (c *ClientConn) IsClient() bool {
	return c.Role == RoleClient
}

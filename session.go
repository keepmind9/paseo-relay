package main

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const maxPendingFrames = 200

// Session manages all relay state for a single serverId.
type Session struct {
	mu            sync.RWMutex
	serverID      string
	control       *ClientConn
	dataSockets   map[string]*ClientConn  // connectionId -> daemon data socket
	clientSockets map[string][]*ClientConn // connectionId -> client sockets
	pending       map[string][][]byte      // connectionId -> buffered frame data
	pendingTypes  map[string][]int         // connectionId -> buffered frame message types
	v1Server      *ClientConn
	v1Client      *ClientConn
}

// NewSession creates a new session for the given serverId.
func NewSession(serverID string) *Session {
	return &Session{
		serverID:      serverID,
		dataSockets:   make(map[string]*ClientConn),
		clientSockets: make(map[string][]*ClientConn),
		pending:       make(map[string][][]byte),
		pendingTypes:  make(map[string][]int),
	}
}

// RegisterControl sets the daemon control socket, replacing any existing one.
// Sends a sync message with currently connected clients.
func (s *Session) RegisterControl(conn *ClientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.control != nil {
		s.control.Close()
	}
	s.control = conn

	// Send sync with current connection list
	s.sendSyncLocked()
}

// GetControl returns the current control connection.
func (s *Session) GetControl() *ClientConn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.control
}

// ClearControl removes the control connection (e.g. on disconnect).
func (s *Session) ClearControl() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.control = nil
}

// RegisterClient adds a client socket for the given connectionId.
// Sends a "connected" notification to the control socket.
func (s *Session) RegisterClient(conn *ClientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clientSockets[conn.ConnectionID] = append(s.clientSockets[conn.ConnectionID], conn)

	s.notifyControlLocked(ControlMessage{
		Type:         "connected",
		ConnectionID: &conn.ConnectionID,
	})
}

// RegisterDataSocket adds a daemon data socket for the given connectionId.
// Replaces any existing data socket. Flushes buffered frames.
func (s *Session) RegisterDataSocket(conn *ClientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.dataSockets[conn.ConnectionID]; ok {
		existing.Close()
	}
	s.dataSockets[conn.ConnectionID] = conn

	// Flush pending frames
	if frames, ok := s.pending[conn.ConnectionID]; ok {
		types := s.pendingTypes[conn.ConnectionID]
		delete(s.pending, conn.ConnectionID)
		delete(s.pendingTypes, conn.ConnectionID)
		for i, frame := range frames {
			msgType := websocket.TextMessage
			if i < len(types) {
				msgType = types[i]
			}
			if err := conn.Send(msgType, frame); err != nil {
				// Re-buffer on failure
				s.bufferFrameLocked(conn.ConnectionID, msgType, frame)
				break
			}
		}
	}
}

// RemoveClient removes a client socket. If it was the last client for that
// connectionId, closes the data socket and sends "disconnected" to control.
func (s *Session) RemoveClient(conn *ClientConn, connectionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	clients := s.clientSockets[connectionID]
	filtered := make([]*ClientConn, 0, len(clients))
	for _, c := range clients {
		if c != conn {
			filtered = append(filtered, c)
		}
	}

	if len(filtered) > 0 {
		s.clientSockets[connectionID] = filtered
		return
	}

	// Last client removed
	delete(s.clientSockets, connectionID)
	delete(s.pending, connectionID)
	delete(s.pendingTypes, connectionID)

	// Close data socket
	if dataSocket, ok := s.dataSockets[connectionID]; ok {
		dataSocket.Close()
		delete(s.dataSockets, connectionID)
	}

	s.notifyControlLocked(ControlMessage{
		Type:         "disconnected",
		ConnectionID: &connectionID,
	})
}

// RemoveDataSocket removes a daemon data socket and closes all matching clients.
func (s *Session) RemoveDataSocket(connectionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.dataSockets, connectionID)
	delete(s.pending, connectionID)
	delete(s.pendingTypes, connectionID)

	for _, client := range s.clientSockets[connectionID] {
		client.Close()
	}
	delete(s.clientSockets, connectionID)
}

// HandleClientMessage forwards a message from a client to the daemon data socket.
// If no data socket exists, buffers the frame.
func (s *Session) HandleClientMessage(connectionID string, msgType int, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dataSocket, ok := s.dataSockets[connectionID]
	if !ok {
		s.bufferFrameLocked(connectionID, msgType, data)
		return
	}
	if err := dataSocket.Send(msgType, data); err != nil {
		log.Printf("[relay] failed to forward client->server(%s): %v", connectionID, err)
		s.bufferFrameLocked(connectionID, msgType, data)
	}
}

// HandleDataMessage forwards a message from daemon data socket to all clients.
func (s *Session) HandleDataMessage(connectionID string, msgType int, data []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clients := s.clientSockets[connectionID]
	for _, client := range clients {
		if err := client.Send(msgType, data); err != nil {
			log.Printf("[relay] failed to forward server->client(%s): %v", connectionID, err)
		}
	}
}

// HandleControlMessage processes a message received on the control socket.
func (s *Session) HandleControlMessage(conn *ClientConn, raw string) {
	msg, err := ParseControlMessage(raw)
	if err != nil {
		return
	}
	if msg.Type == "ping" {
		ts := time.Now().UnixMilli()
		s.mu.RLock()
		defer s.mu.RUnlock()
		if s.control != nil {
			s.sendControlLocked(ControlMessage{Type: "pong", Ts: &ts})
		}
	}
}

// ListConnectionIDs returns all connectionIds that have active clients.
func (s *Session) ListConnectionIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var ids []string
	for id, clients := range s.clientSockets {
		if len(clients) > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

// PendingCount returns the number of buffered frames for a connectionId.
func (s *Session) PendingCount(connectionID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.pending[connectionID])
}

// SetV1Server sets the v1 server socket, replacing any existing one.
func (s *Session) SetV1Server(conn *ClientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.v1Server != nil {
		s.v1Server.Close()
	}
	s.v1Server = conn
}

// GetV1Server returns the v1 server socket.
func (s *Session) GetV1Server() *ClientConn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.v1Server
}

// ClearV1ServerIf clears the v1 server only if it matches the given connection.
// This prevents a stale goroutine from clearing a replacement connection.
func (s *Session) ClearV1ServerIf(conn *ClientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.v1Server == conn {
		s.v1Server = nil
	}
}

// SetV1Client sets the v1 client socket.
func (s *Session) SetV1Client(conn *ClientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.v1Client = conn
}

// GetV1Client returns the v1 client socket.
func (s *Session) GetV1Client() *ClientConn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.v1Client
}

// ClearV1ClientIf clears the v1 client only if it matches the given connection.
func (s *Session) ClearV1ClientIf(conn *ClientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.v1Client == conn {
		s.v1Client = nil
	}
}

// ClearControlIf clears the control socket only if it matches the given connection.
func (s *Session) ClearControlIf(conn *ClientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.control == conn {
		s.control = nil
	}
}

// RemoveDataSocketIf removes a data socket only if it matches the given connection.
func (s *Session) RemoveDataSocketIf(connectionID string, conn *ClientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dataSockets[connectionID] != conn {
		return
	}
	delete(s.dataSockets, connectionID)
	delete(s.pending, connectionID)
	delete(s.pendingTypes, connectionID)
	for _, client := range s.clientSockets[connectionID] {
		client.Close()
	}
	delete(s.clientSockets, connectionID)
}

// HasServerDataSocket returns true if a server data socket exists for the given connectionId.
func (s *Session) HasServerDataSocket(connectionID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.dataSockets[connectionID]
	return ok
}

// HasClientSocket returns true if any client socket exists for the given connectionId.
func (s *Session) HasClientSocket(connectionID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clientSockets[connectionID]) > 0
}

// SendSync sends a sync message to the control socket.
func (s *Session) SendSync() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendSyncLocked()
}

// CloseControl force-closes the control socket so the daemon reconnects.
func (s *Session) CloseControl() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.control != nil {
		s.control.Close()
		s.control = nil
	}
}

// notifyControlLocked sends a control message. Must be called with mu held.
func (s *Session) notifyControlLocked(msg ControlMessage) {
	if s.control == nil {
		return
	}
	s.sendControlLocked(msg)
}

// sendControlLocked marshals and sends a control message. Must be called with mu held.
func (s *Session) sendControlLocked(msg ControlMessage) {
	if s.control == nil || s.control.Ws == nil {
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[relay] failed to marshal control message: %v", err)
		return
	}
	if err := s.control.Send(websocket.TextMessage, data); err != nil {
		log.Printf("[relay] failed to send to control: %v", err)
		s.control.Close()
	}
}

// sendSyncLocked sends a sync message with current connection IDs.
func (s *Session) sendSyncLocked() {
	var ids []string
	for id, clients := range s.clientSockets {
		if len(clients) > 0 {
			ids = append(ids, id)
		}
	}
	s.notifyControlLocked(ControlMessage{
		Type:          "sync",
		ConnectionIDs: ids,
	})
}

// bufferFrameLocked appends a frame to the pending buffer.
// Caps at maxPendingFrames, dropping oldest first.
func (s *Session) bufferFrameLocked(connectionID string, msgType int, data []byte) {
	buf := make([]byte, len(data))
	copy(buf, data)

	s.pending[connectionID] = append(s.pending[connectionID], buf)
	s.pendingTypes[connectionID] = append(s.pendingTypes[connectionID], msgType)

	if len(s.pending[connectionID]) > maxPendingFrames {
		s.pending[connectionID] = s.pending[connectionID][len(s.pending[connectionID])-maxPendingFrames:]
		s.pendingTypes[connectionID] = s.pendingTypes[connectionID][len(s.pendingTypes[connectionID])-maxPendingFrames:]
	}
}

package main

import (
	"log/slog"
	"sync"
)

// SessionHub is a thread-safe registry of all active sessions.
type SessionHub struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	logger   *slog.Logger
}

// NewSessionHub creates a new empty session hub.
func NewSessionHub(logger *slog.Logger) *SessionHub {
	return &SessionHub{
		sessions: make(map[string]*Session),
		logger:   logger,
	}
}

// GetOrCreateSession returns the session for the given serverId, creating one if needed.
func (h *SessionHub) GetOrCreateSession(serverID string) *Session {
	h.mu.Lock()
	defer h.mu.Unlock()

	if s, ok := h.sessions[serverID]; ok {
		return s
	}
	s := NewSession(serverID, h.logger)
	h.sessions[serverID] = s
	return s
}

// RemoveSession removes a session from the hub.
func (h *SessionHub) RemoveSession(serverID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, serverID)
}

// ActiveCount returns the number of active sessions.
func (h *SessionHub) ActiveCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.sessions)
}

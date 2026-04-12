package main

import (
	"log/slog"
	"sync"
	"time"
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

// CloseAll closes all sessions and removes them from the hub.
func (h *SessionHub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for id, s := range h.sessions {
		s.CloseAll()
		delete(h.sessions, id)
	}
}

// StartCleanup launches a background goroutine that periodically removes
// idle sessions. Returns a channel that should be closed to stop the goroutine.
func (h *SessionHub) StartCleanup(interval, maxIdle time.Duration, logger *slog.Logger) chan struct{} {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				h.cleanupIdleSessions(maxIdle, logger)
			}
		}
	}()
	return stop
}

// cleanupIdleSessions removes sessions that have been idle for longer than maxIdle.
func (h *SessionHub) cleanupIdleSessions(maxIdle time.Duration, logger *slog.Logger) {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	for id, s := range h.sessions {
		if s.IsIdle(maxIdle, now) {
			s.CloseAll()
			delete(h.sessions, id)
			logger.Info("cleaned up idle session", "serverId", id)
		}
	}
}

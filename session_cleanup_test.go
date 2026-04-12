package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSessionIsEmpty(t *testing.T) {
	s := NewSession("test-server", testLogger)

	// New session is empty
	s.mu.RLock()
	empty := s.isEmptyLocked()
	s.mu.RUnlock()
	assert.True(t, empty, "new session should be empty")

	// Add a control connection
	control := &ClientConn{
		Role:      RoleServer,
		Version:   Version2,
		ServerID:  "test-server",
		CreatedAt: time.Now(),
	}
	s.RegisterControl(control)

	s.mu.RLock()
	empty = s.isEmptyLocked()
	s.mu.RUnlock()
	assert.False(t, empty, "session with control should not be empty")

	// Remove control
	s.ClearControl()

	s.mu.RLock()
	empty = s.isEmptyLocked()
	s.mu.RUnlock()
	assert.True(t, empty, "session without connections should be empty")
}

func TestSessionIsIdle(t *testing.T) {
	s := NewSession("test-server", testLogger)

	// New session has no idleSince set (no connections ever registered)
	now := time.Now()
	assert.False(t, s.IsIdle(5*time.Minute, now), "new session without idleSince should not be idle")

	// Add then remove a connection to trigger idle tracking
	control := &ClientConn{
		Role:      RoleServer,
		Version:   Version2,
		ServerID:  "test-server",
		CreatedAt: time.Now(),
	}
	s.RegisterControl(control)
	s.ClearControl()

	// Just became idle — not yet expired
	now = time.Now()
	assert.False(t, s.IsIdle(5*time.Minute, now), "just-idle session should not be expired")

	// Simulate 6 minutes passing
	futureNow := time.Now().Add(6 * time.Minute)
	assert.True(t, s.IsIdle(5*time.Minute, futureNow), "session idle for 6min should be expired with 5min threshold")
}

func TestSessionIsIdleResetsOnNewConnection(t *testing.T) {
	s := NewSession("test-server", testLogger)

	// Add and remove to trigger idle
	control := &ClientConn{
		Role:      RoleServer,
		Version:   Version2,
		ServerID:  "test-server",
		CreatedAt: time.Now(),
	}
	s.RegisterControl(control)
	s.ClearControl()

	// Re-add a connection
	control2 := &ClientConn{
		Role:      RoleServer,
		Version:   Version2,
		ServerID:  "test-server",
		CreatedAt: time.Now(),
	}
	s.RegisterControl(control2)

	futureNow := time.Now().Add(6 * time.Minute)
	assert.False(t, s.IsIdle(5*time.Minute, futureNow), "session with active connection should not be idle")
}

func TestSessionCloseAll(t *testing.T) {
	s := NewSession("test-server", testLogger)

	control := &ClientConn{
		Role:      RoleServer,
		Version:   Version2,
		ServerID:  "test-server",
		CreatedAt: time.Now(),
	}
	s.RegisterControl(control)

	client := &ClientConn{
		Role:         RoleClient,
		Version:      Version2,
		ServerID:     "test-server",
		ConnectionID: "conn_close",
		CreatedAt:    time.Now(),
	}
	s.RegisterClient(client)

	// Buffer some data
	s.HandleClientMessage("conn_close", 1, []byte("data"))
	assert.Equal(t, 1, s.PendingCount("conn_close"))

	s.CloseAll()

	// All state should be cleared
	assert.Nil(t, s.GetControl())
	assert.Equal(t, 0, len(s.clientSockets))
	assert.Equal(t, 0, len(s.dataSockets))
	assert.Equal(t, 0, s.PendingCount("conn_close"))
}

func TestSessionCloseAllIdempotent(t *testing.T) {
	s := NewSession("test-server", testLogger)

	// CloseAll on empty session should not panic
	s.CloseAll()
	s.CloseAll()

	assert.Nil(t, s.GetControl())
}

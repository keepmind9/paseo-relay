package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHubCloseAll(t *testing.T) {
	hub := NewSessionHub(testLogger)

	_ = hub.GetOrCreateSession("server-1")
	_ = hub.GetOrCreateSession("server-2")

	control := &ClientConn{
		Role:      RoleServer,
		Version:   Version2,
		ServerID:  "server-1",
		CreatedAt: time.Now(),
	}
	hub.GetOrCreateSession("server-1").RegisterControl(control)

	assert.Equal(t, 2, hub.ActiveCount())

	hub.CloseAll()

	assert.Equal(t, 0, hub.ActiveCount())
}

func TestHubCleanupIdleSessions(t *testing.T) {
	hub := NewSessionHub(testLogger)

	// Create a session and make it idle
	s := hub.GetOrCreateSession("idle-server")
	control := &ClientConn{
		Role:      RoleServer,
		Version:   Version2,
		ServerID:  "idle-server",
		CreatedAt: time.Now(),
	}
	s.RegisterControl(control)
	s.ClearControl()

	// Create an active session
	activeSession := hub.GetOrCreateSession("active-server")
	activeControl := &ClientConn{
		Role:      RoleServer,
		Version:   Version2,
		ServerID:  "active-server",
		CreatedAt: time.Now(),
	}
	activeSession.RegisterControl(activeControl)

	assert.Equal(t, 2, hub.ActiveCount())

	// Run cleanup with maxIdle=0 to ensure idle session is cleaned
	// We need to manipulate idleSince to simulate time passage
	s.mu.Lock()
	s.idleSince = time.Now().Add(-10 * time.Minute)
	s.mu.Unlock()

	hub.cleanupIdleSessions(5*time.Minute, testLogger)

	assert.Equal(t, 1, hub.ActiveCount())

	// The remaining session should be the active one
	remaining := hub.GetOrCreateSession("active-server")
	assert.NotNil(t, remaining)
}

func TestHubStartCleanupStops(t *testing.T) {
	hub := NewSessionHub(testLogger)

	stop := hub.StartCleanup(50*time.Millisecond, 1*time.Minute, testLogger)

	// Create and idle a session
	s := hub.GetOrCreateSession("to-clean")
	control := &ClientConn{
		Role:      RoleServer,
		Version:   Version2,
		ServerID:  "to-clean",
		CreatedAt: time.Now(),
	}
	s.RegisterControl(control)
	s.ClearControl()

	// Set idleSince far in the past
	s.mu.Lock()
	s.idleSince = time.Now().Add(-10 * time.Minute)
	s.mu.Unlock()

	// Wait for at least one cleanup cycle
	time.Sleep(100 * time.Millisecond)

	// Stop the cleanup goroutine
	close(stop)

	// Session should be cleaned up
	assert.Equal(t, 0, hub.ActiveCount())
}

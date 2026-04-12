package main

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestHubGetOrCreateSession(t *testing.T) {
	hub := NewSessionHub(testLogger)

	s1 := hub.GetOrCreateSession("server-1")
	assert.NotNil(t, s1)

	s2 := hub.GetOrCreateSession("server-1")
	assert.Equal(t, s1, s2, "same serverId should return same session")

	s3 := hub.GetOrCreateSession("server-2")
	assert.NotEqual(t, s1, s3, "different serverId should return different session")
}

func TestHubRemoveSession(t *testing.T) {
	hub := NewSessionHub(testLogger)

	s := hub.GetOrCreateSession("server-1")
	hub.RemoveSession("server-1")

	// After removal, GetOrCreateSession should return a different instance
	s2 := hub.GetOrCreateSession("server-1")
	// Verify by checking they are different pointers
	assert.NotSame(t, s, s2, "removed session should create new one")
	// But both should be valid sessions
	assert.NotNil(t, s)
	assert.NotNil(t, s2)
}

func TestHubActiveSessionCount(t *testing.T) {
	hub := NewSessionHub(testLogger)

	assert.Equal(t, 0, hub.ActiveCount())

	hub.GetOrCreateSession("a")
	hub.GetOrCreateSession("b")
	assert.Equal(t, 2, hub.ActiveCount())

	hub.RemoveSession("a")
	assert.Equal(t, 1, hub.ActiveCount())
}

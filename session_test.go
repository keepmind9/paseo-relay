package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockConn is a test helper that records sent messages without a real WebSocket.
type mockConn struct {
	role         ConnectionRole
	version      ProtocolVersion
	serverID     string
	connectionID string
	sent         []string
	closed       bool
}

func newMockConn(v ProtocolVersion, role ConnectionRole, serverID, connID string) *mockConn {
	return &mockConn{
		version:      v,
		role:         role,
		serverID:     serverID,
		connectionID: connID,
	}
}

func (m *mockConn) Send(msgType int, data []byte) error {
	m.sent = append(m.sent, string(data))
	return nil
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func (m *mockConn) sentMessages() []string { return m.sent }
func (m *mockConn) isClosed() bool         { return m.closed }

// toClientConn wraps a mockConn into a ClientConn-like interface for session tests.
// Since Session methods use *ClientConn, we embed the mockConn fields directly.
type testClientConn struct {
	ClientConn
	mock *mockConn
}

func newTestClientConn(v ProtocolVersion, role ConnectionRole, serverID, connID string) *testClientConn {
	m := newMockConn(v, role, serverID, connID)
	return &testClientConn{
		ClientConn: ClientConn{
			Version:      v,
			Role:         role,
			ServerID:     serverID,
			ConnectionID: connID,
			CreatedAt:    time.Now(),
		},
		mock: m,
	}
}

// Override Send and Close on testClientConn by using an interface approach.
// Instead, we create helper functions that build real ClientConn objects
// and override their Send/Close via embedding.

// For session testing, we use a wrapper approach:
// Create a ClientConn, then replace its Send/Close behavior.
// Since Go doesn't allow method override on structs, we test at a higher level
// using integration tests with real WebSockets (see server_test.go and e2e_test.go).

// Instead, let's test the Session's internal state directly.

func TestSessionControlConnect(t *testing.T) {
	s := NewSession("test-server")

	control := &ClientConn{
		Role:      RoleServer,
		Version:   Version2,
		ServerID:  "test-server",
		CreatedAt: time.Now(),
	}

	s.RegisterControl(control)
	assert.Equal(t, control, s.GetControl())

	// New control replaces old
	control2 := &ClientConn{
		Role:      RoleServer,
		Version:   Version2,
		ServerID:  "test-server",
		CreatedAt: time.Now(),
	}
	s.RegisterControl(control2)
	assert.Equal(t, control2, s.GetControl())
}

func TestSessionClientConnectAndBuffer(t *testing.T) {
	s := NewSession("test-server")

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
		ConnectionID: "conn_abc",
		CreatedAt:    time.Now(),
	}

	s.RegisterClient(client)
	assert.Equal(t, 1, len(s.clientSockets["conn_abc"]))
}

func TestSessionClientMessageBuffering(t *testing.T) {
	s := NewSession("test-server")

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
		ConnectionID: "conn_buf",
		CreatedAt:    time.Now(),
	}
	s.RegisterClient(client)

	// Client sends message before daemon data socket exists
	s.HandleClientMessage("conn_buf", 1, []byte("hello"))
	assert.Equal(t, 1, s.PendingCount("conn_buf"))
}

func TestSessionPendingFrameLimit(t *testing.T) {
	s := NewSession("test-server")

	// Buffer 250 messages — should cap at 200
	for i := 0; i < 250; i++ {
		s.HandleClientMessage("conn_limit", 1, []byte("msg"))
	}
	assert.Equal(t, 200, s.PendingCount("conn_limit"))
}

func TestSessionListConnectionIDs(t *testing.T) {
	s := NewSession("test-server")

	control := &ClientConn{
		Role:      RoleServer,
		Version:   Version2,
		ServerID:  "test-server",
		CreatedAt: time.Now(),
	}
	s.RegisterControl(control)

	client1 := &ClientConn{
		Role:         RoleClient,
		Version:      Version2,
		ServerID:     "test-server",
		ConnectionID: "conn_a",
		CreatedAt:    time.Now(),
	}
	s.RegisterClient(client1)

	client2 := &ClientConn{
		Role:         RoleClient,
		Version:      Version2,
		ServerID:     "test-server",
		ConnectionID: "conn_b",
		CreatedAt:    time.Now(),
	}
	s.RegisterClient(client2)

	ids := s.ListConnectionIDs()
	assert.ElementsMatch(t, []string{"conn_a", "conn_b"}, ids)
}

func TestSessionRemoveClientLastOne(t *testing.T) {
	s := NewSession("test-server")

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
		ConnectionID: "conn_disc",
		CreatedAt:    time.Now(),
	}
	s.RegisterClient(client)

	// Should not panic when removing
	s.RemoveClient(client, "conn_disc")

	// Client list should be empty
	assert.Equal(t, 0, len(s.clientSockets["conn_disc"]))
}

func TestSessionRemoveDataSocketClosesClients(t *testing.T) {
	// This is tested more thoroughly in e2e_test.go with real WebSockets
	s := NewSession("test-server")

	client := &ClientConn{
		Role:         RoleClient,
		Version:      Version2,
		ServerID:     "test-server",
		ConnectionID: "conn_ddisc",
		CreatedAt:    time.Now(),
	}
	s.clientSockets["conn_ddisc"] = []*ClientConn{client}

	s.RemoveDataSocket("conn_ddisc")
	assert.Equal(t, 0, len(s.clientSockets["conn_ddisc"]))
}

func TestSessionV1ServerClient(t *testing.T) {
	s := NewSession("test-server")

	server := &ClientConn{Role: RoleServer, Version: Version1, ServerID: "test-server"}
	client := &ClientConn{Role: RoleClient, Version: Version1, ServerID: "test-server"}

	s.SetV1Server(server)
	assert.Equal(t, server, s.GetV1Server())

	s.SetV1Client(client)
	assert.Equal(t, client, s.GetV1Client())

	// Replace server
	server2 := &ClientConn{Role: RoleServer, Version: Version1, ServerID: "test-server"}
	s.SetV1Server(server2)
	assert.Equal(t, server2, s.GetV1Server())
}

func TestControlMessageParsing(t *testing.T) {
	msg := ControlMessage{
		Type:          "sync",
		ConnectionIDs: []string{"a", "b"},
	}
	data, err := json.Marshal(msg)
	require.NoError(t, err)

	parsed, err := ParseControlMessage(string(data))
	require.NoError(t, err)
	assert.Equal(t, "sync", parsed.Type)
	assert.Equal(t, []string{"a", "b"}, parsed.ConnectionIDs)
}

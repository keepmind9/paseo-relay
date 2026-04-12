package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestControlMessageMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		msg  ControlMessage
	}{
		{
			name: "ping",
			msg:  ControlMessage{Type: "ping"},
		},
		{
			name: "pong",
			msg:  ControlMessage{Type: "pong", Ts: ptr(int64(1712923200000))},
		},
		{
			name: "sync",
			msg:  ControlMessage{Type: "sync", ConnectionIDs: []string{"conn_abc123", "conn_def456"}},
		},
		{
			name: "connected",
			msg:  ControlMessage{Type: "connected", ConnectionID: ptr("conn_abc123")},
		},
		{
			name: "disconnected",
			msg:  ControlMessage{Type: "disconnected", ConnectionID: ptr("conn_abc123")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			assert.NoError(t, err)

			var parsed ControlMessage
			assert.NoError(t, json.Unmarshal(data, &parsed))
			assert.Equal(t, tt.msg.Type, parsed.Type)
		})
	}
}

func TestParseControlMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    ControlMessage
		wantErr bool
	}{
		{
			name:  "ping",
			input: `{"type":"ping"}`,
			want:  ControlMessage{Type: "ping"},
		},
		{
			name:  "pong with ts",
			input: `{"type":"pong","ts":1712923200000}`,
			want:  ControlMessage{Type: "pong", Ts: ptr(int64(1712923200000))},
		},
		{
			name:  "sync",
			input: `{"type":"sync","connectionIds":["a","b"]}`,
			want:  ControlMessage{Type: "sync", ConnectionIDs: []string{"a", "b"}},
		},
		{
			name:    "invalid json",
			input:   `not json`,
			wantErr: true,
		},
		{
			name:    "missing type",
			input:   `{"foo":"bar"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseControlMessage(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want.Type, got.Type)
		})
	}
}

func ptr[T any](v T) *T { return &v }

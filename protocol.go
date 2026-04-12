package main

import (
	"encoding/json"
	"fmt"
)

// ControlMessage represents a control-plane JSON message exchanged on the
// daemon's control socket. The relay never inspects application data — only
// these well-known control types.
type ControlMessage struct {
	Type          string   `json:"type"`
	Ts            *int64   `json:"ts,omitempty"`
	ConnectionIDs []string `json:"connectionIds,omitempty"`
	ConnectionID  *string  `json:"connectionId,omitempty"`
}

// ParseControlMessage parses a JSON string into a ControlMessage.
// Returns an error if the input is not valid JSON or lacks a "type" field.
func ParseControlMessage(raw string) (ControlMessage, error) {
	var msg ControlMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return ControlMessage{}, fmt.Errorf("invalid control message: %w", err)
	}
	if msg.Type == "" {
		return ControlMessage{}, fmt.Errorf("control message missing type field")
	}
	return msg, nil
}

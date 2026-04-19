package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/gorilla/websocket"
)

type Message struct {
	Type      string          `json:"type"`
	Role      string          `json:"role,omitempty"`
	EdgeID    string          `json:"edge_id,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	SDP       string          `json:"sdp,omitempty"`
	Candidate json.RawMessage `json:"candidate,omitempty"`
}

// Connect dials BE, registers as edge, then reads messages until disconnected.
func (e *Edge) Connect(beURL string) error {
	conn, _, err := websocket.DefaultDialer.Dial(beURL, nil)
	if err != nil {
		return fmt.Errorf("dial %s: %w", beURL, err)
	}
	e.wsConn = conn
	defer conn.Close()

	if err := e.sendMessage(Message{
		Type:   "register",
		Role:   "edge",
		EdgeID: e.edgeID,
	}); err != nil {
		return fmt.Errorf("register: %w", err)
	}
	log.Printf("[signaling] registered as edge: %s", e.edgeID)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Println("[signaling] json parse error:", err)
			continue
		}

		e.dispatch(msg)
	}
}

func (e *Edge) dispatch(msg Message) {
	switch msg.Type {
	case "new_viewer":
		log.Printf("[signaling] new_viewer: session=%s", msg.SessionID)
		if err := e.handleNewViewer(msg.SessionID); err != nil {
			log.Println("[signaling] handleNewViewer error:", err)
		}

	case "answer":
		log.Printf("[signaling] answer: session=%s", msg.SessionID)
		if err := e.handleAnswer(msg.SessionID, msg.SDP); err != nil {
			log.Println("[signaling] handleAnswer error:", err)
		}

	case "ice_candidate":
		if err := e.handleRemoteICE(msg.SessionID, msg.Candidate); err != nil {
			log.Println("[signaling] handleRemoteICE error:", err)
		}

	default:
		log.Printf("[signaling] unknown type: %s", msg.Type)
	}
}

// sendMessage is safe to call from multiple goroutines (ICE callbacks + main loop).
func (e *Edge) sendMessage(msg Message) error {
	e.wsMu.Lock()
	defer e.wsMu.Unlock()
	return e.wsConn.WriteJSON(msg)
}

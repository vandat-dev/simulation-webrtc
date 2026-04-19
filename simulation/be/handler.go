package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// SafeConn wraps websocket.Conn with a write mutex.
// gorilla/websocket connections are NOT concurrent-safe for writes,
// so multiple goroutines forwarding to the same conn need this wrapper.
type SafeConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func NewSafeConn(conn *websocket.Conn) *SafeConn {
	return &SafeConn{conn: conn}
}

func (s *SafeConn) WriteJSON(v interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.WriteJSON(v)
}

func (s *SafeConn) ReadMessage() (int, []byte, error) {
	return s.conn.ReadMessage()
}

func (s *SafeConn) Close() error {
	return s.conn.Close()
}

// Message is the common envelope for all WebSocket messages.
// Candidate uses json.RawMessage so BE can forward it verbatim
// without caring whether it's a string or an object.
type Message struct {
	Type      string          `json:"type"`
	Role      string          `json:"role,omitempty"`
	EdgeID    string          `json:"edge_id,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	SDP       string          `json:"sdp,omitempty"`
	Candidate json.RawMessage `json:"candidate,omitempty"`
}

func HandleWS(hub *Hub, w http.ResponseWriter, r *http.Request) {
	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade error:", err)
		return
	}
	conn := NewSafeConn(rawConn)
	defer conn.Close()

	// identity is set once on "register" and reused for cleanup
	var (
		role      string
		edgeID    string
		sessionID string
	)

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			log.Println("read error:", err)
			break
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Println("json parse error:", err)
			continue
		}

		switch msg.Type {

		case "register":
			role = msg.Role
			switch msg.Role {
			case "edge":
				edgeID = msg.EdgeID
				hub.RegisterEdge(edgeID, conn)
				log.Printf("[edge] registered: %s", edgeID)

			case "browser":
				sessionID = msg.SessionID
				hub.RegisterBrowser(sessionID, msg.EdgeID, conn)
				log.Printf("[browser] registered: session=%s edge=%s", sessionID, msg.EdgeID)

				// Notify the edge that a new viewer wants to watch
				edgeConn := hub.GetEdge(msg.EdgeID)
				if edgeConn == nil {
					log.Printf("[browser] edge %s not found", msg.EdgeID)
					continue
				}
				if err := edgeConn.WriteJSON(Message{
					Type:      "new_viewer",
					SessionID: sessionID,
				}); err != nil {
					log.Println("[browser] notify edge error:", err)
				}
			}

		case "offer":
			// Direction: Edge → Browser
			session := hub.GetSession(msg.SessionID)
			if session == nil {
				log.Printf("[offer] session %s not found", msg.SessionID)
				continue
			}
			if err := session.BrowserConn.WriteJSON(msg); err != nil {
				log.Println("[offer] forward to browser error:", err)
			}

		case "answer":
			// Direction: Browser → Edge
			session := hub.GetSession(msg.SessionID)
			if session == nil {
				log.Printf("[answer] session %s not found", msg.SessionID)
				continue
			}
			edgeConn := hub.GetEdge(session.EdgeID)
			if edgeConn == nil {
				log.Printf("[answer] edge %s not found", session.EdgeID)
				continue
			}
			if err := edgeConn.WriteJSON(msg); err != nil {
				log.Println("[answer] forward to edge error:", err)
			}

		case "ice_candidate":
			session := hub.GetSession(msg.SessionID)
			if session == nil {
				log.Printf("[ice] session %s not found", msg.SessionID)
				continue
			}
			if role == "edge" {
				// Direction: Edge → Browser
				if err := session.BrowserConn.WriteJSON(msg); err != nil {
					log.Println("[ice] edge→browser error:", err)
				}
			} else {
				// Direction: Browser → Edge
				edgeConn := hub.GetEdge(session.EdgeID)
				if edgeConn == nil {
					log.Printf("[ice] edge %s not found", session.EdgeID)
					continue
				}
				if err := edgeConn.WriteJSON(msg); err != nil {
					log.Println("[ice] browser→edge error:", err)
				}
			}
		}
	}

	// Cleanup on disconnect
	switch role {
	case "edge":
		hub.RemoveEdge(edgeID)
		log.Printf("[edge] disconnected: %s", edgeID)
	case "browser":
		hub.RemoveSession(sessionID)
		log.Printf("[browser] disconnected: session=%s", sessionID)
	}
}

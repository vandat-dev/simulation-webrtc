package main

import "sync"

type Session struct {
	BrowserConn *SafeConn
	EdgeID      string
}

type Hub struct {
	edges    map[string]*SafeConn
	sessions map[string]*Session
	mu       sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		edges:    make(map[string]*SafeConn),
		sessions: make(map[string]*Session),
	}
}

func (h *Hub) RegisterEdge(edgeID string, conn *SafeConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.edges[edgeID] = conn
}

func (h *Hub) RegisterBrowser(sessionID, edgeID string, conn *SafeConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions[sessionID] = &Session{BrowserConn: conn, EdgeID: edgeID}
}

func (h *Hub) GetEdge(edgeID string) *SafeConn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.edges[edgeID]
}

func (h *Hub) GetSession(sessionID string) *Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sessions[sessionID]
}

func (h *Hub) RemoveEdge(edgeID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.edges, edgeID)
}

func (h *Hub) RemoveSession(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, sessionID)
}

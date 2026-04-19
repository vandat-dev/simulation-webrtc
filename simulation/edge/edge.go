package main

import (
	"log"
	"sync"

	"github.com/go-gst/go-gst/gst"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

type Edge struct {
	edgeID string
	device string // e.g. /dev/video0

	// WebSocket
	wsConn *websocket.Conn
	wsMu   sync.Mutex

	// WebRTC — one shared track, one PeerConnection per session
	track   *webrtc.TrackLocalStaticSample
	peers   map[string]*webrtc.PeerConnection
	peersMu sync.RWMutex

	// GStreamer — single pipeline, chạy liên tục từ khi edge khởi động
	pipeline *gst.Pipeline
}

func NewEdge(edgeID, device string) (*Edge, error) {
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video",
		"gstreamer",
	)
	if err != nil {
		return nil, err
	}

	return &Edge{
		edgeID: edgeID,
		device: device,
		track:  track,
		peers:  make(map[string]*webrtc.PeerConnection),
	}, nil
}

func (e *Edge) addPeer(sessionID string, pc *webrtc.PeerConnection) {
	e.peersMu.Lock()
	defer e.peersMu.Unlock()
	e.peers[sessionID] = pc
}

func (e *Edge) removePeer(sessionID string) {
	e.peersMu.Lock()
	defer e.peersMu.Unlock()
	if pc, ok := e.peers[sessionID]; ok {
		pc.Close()
		delete(e.peers, sessionID)
		log.Printf("[edge] peer removed: session=%s remaining=%d", sessionID, len(e.peers))
	}
}

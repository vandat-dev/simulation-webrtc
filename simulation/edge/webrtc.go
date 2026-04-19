package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/pion/ice/v2"
	"github.com/pion/webrtc/v3"
)

var iceConfig = webrtc.Configuration{
	ICEServers: []webrtc.ICEServer{
		{URLs: []string{"stun:stun.l.google.com:19302"}},
		//{URLs: []string{"stun:192.168.1.9:3478"}},
	},
}

// newPeerConnection tạo PeerConnection với mDNS resolve enabled.
func newPeerConnection() (*webrtc.PeerConnection, error) {
	s := webrtc.SettingEngine{}
	s.SetICEMulticastDNSMode(ice.MulticastDNSModeQueryOnly)

	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("register codecs: %w", err)
	}

	api := webrtc.NewAPI(
		webrtc.WithSettingEngine(s),
		webrtc.WithMediaEngine(m), // ← thêm dòng này
	)
	return api.NewPeerConnection(iceConfig)
}

// handleNewViewer tạo PeerConnection mới cho browser viewer vừa kết nối.
func (e *Edge) handleNewViewer(sessionID string) error {
	pc, err := newPeerConnection()
	if err != nil {
		return fmt.Errorf("new peer connection: %w", err)
	}

	if _, err := pc.AddTrack(e.track); err != nil {
		pc.Close()
		return fmt.Errorf("add track: %w", err)
	}

	// Forward gathered ICE candidates to the browser via BE.
	// OnICECandidate fires in a pion goroutine — sendMessage is mutex-protected.
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			log.Printf("[ice] session=%s gathering complete", sessionID)
			return
		}
		log.Printf("[ice] session=%s local candidate: %s", sessionID, c.String())
		raw, err := json.Marshal(c.ToJSON())
		if err != nil {
			log.Println("[webrtc] marshal ICE error:", err)
			return
		}
		if err := e.sendMessage(Message{
			Type:      "ice_candidate",
			SessionID: sessionID,
			Candidate: raw,
		}); err != nil {
			log.Println("[webrtc] send ICE error:", err)
		}
	})

	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Printf("[ice] session=%s connection state: %s", sessionID, state)
	})

	// Clean up when WebRTC connection closes or fails.
	// OnConnectionStateChange fires in a pion goroutine.
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[webrtc] session=%s state=%s", sessionID, state)
		switch state {
		case webrtc.PeerConnectionStateDisconnected,
			webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateClosed:
			e.removePeer(sessionID)
		}
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		pc.Close()
		return fmt.Errorf("create offer: %w", err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		pc.Close()
		return fmt.Errorf("set local description: %w", err)
	}

	// Register the peer before sending offer — answer may arrive very fast.
	e.addPeer(sessionID, pc)

	if err := e.sendMessage(Message{
		Type:      "offer",
		SessionID: sessionID,
		SDP:       offer.SDP,
	}); err != nil {
		e.removePeer(sessionID)
		return fmt.Errorf("send offer: %w", err)
	}

	log.Printf("[webrtc] offer sent: session=%s", sessionID)
	return nil
}

func (e *Edge) handleAnswer(sessionID, sdp string) error {
	e.peersMu.RLock()
	pc := e.peers[sessionID]
	e.peersMu.RUnlock()

	if pc == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}
	return pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  sdp,
	})
}

func (e *Edge) handleRemoteICE(sessionID string, raw json.RawMessage) error {
	e.peersMu.RLock()
	pc := e.peers[sessionID]
	e.peersMu.RUnlock()

	if pc == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	var candidate webrtc.ICECandidateInit
	if err := json.Unmarshal(raw, &candidate); err != nil {
		return fmt.Errorf("unmarshal ICE candidate: %w", err)
	}
	log.Printf("[ice] session=%s remote candidate: %s", sessionID, candidate.Candidate)
	return pc.AddICECandidate(candidate)
}

# WebRTC P2P Simulation — Design Doc

## Goal

Build a WebRTC simulation system where an Edge device captures video from `/dev/video0`
via GStreamer and streams directly to multiple Browser viewers via P2P WebRTC.
Backend acts as a pure WebSocket signaling relay — it never touches media.
Each Browser gets its own PeerConnection with the Edge; GStreamer encodes once and
fans out to all active tracks.

Context: Edge is behind NAT, BE has a public IP. ICE uses Google STUN for NAT traversal.

---

## WebSocket Message API

Single endpoint: `ws://BE_HOST:9090/ws`

All messages are JSON.

### Edge → BE

| type            | fields                        | description                     |
|-----------------|-------------------------------|---------------------------------|
| `register`      | `role:"edge"`, `edge_id`      | Edge đăng ký với BE             |
| `offer`         | `session_id`, `sdp`           | Forward Offer SDP tới Browser   |
| `ice_candidate` | `session_id`, `candidate`     | Forward ICE candidate tới Browser |

### Browser → BE

| type            | fields                                     | description                      |
|-----------------|--------------------------------------------|----------------------------------|
| `register`      | `role:"browser"`, `edge_id`, `session_id`  | Browser đăng ký, muốn xem edge   |
| `answer`        | `session_id`, `sdp`                        | Forward Answer SDP tới Edge      |
| `ice_candidate` | `session_id`, `candidate`                  | Forward ICE candidate tới Edge   |

### BE → Edge

| type            | fields                    | description                        |
|-----------------|---------------------------|------------------------------------|
| `new_viewer`    | `session_id`              | Có Browser mới muốn xem            |
| `answer`        | `session_id`, `sdp`       | Forward Answer từ Browser          |
| `ice_candidate` | `session_id`, `candidate` | Forward ICE candidate từ Browser   |

### BE → Browser

| type            | fields                    | description                        |
|-----------------|---------------------------|------------------------------------|
| `offer`         | `session_id`, `sdp`       | Forward Offer từ Edge              |
| `ice_candidate` | `session_id`, `candidate` | Forward ICE candidate từ Edge      |

---

## Internal State

### BE (Hub)

```
Hub {
    edges:    map[edge_id]    → WebSocket conn
    sessions: map[session_id] → { browserConn, edge_id }
    mu:       sync.RWMutex
}
```

### Edge

```
EdgeState {
    pipeline: *gst.Pipeline                      // 1 pipeline duy nhất
    track:    *webrtc.TrackLocalStaticSample      // shared track, fan-out
    peers:    map[session_id] → *webrtc.PeerConnection
    mu:       sync.RWMutex
}
```

---

## Architecture & Flow

```
1.  BE start → Hub{ edges:{}, sessions:{} }

2.  Edge connect WS → register{ role:"edge", edge_id:"edge-01" }
        → Hub.edges["edge-01"] = conn
        → Edge CHỜ, chưa làm gì thêm

3.  Browser connect WS → register{ role:"browser", edge_id:"edge-01", session_id:"sess-abc" }
        → Hub.sessions["sess-abc"] = { browserConn, edge_id:"edge-01" }
        → BE gửi new_viewer{ session_id:"sess-abc" } tới Edge

4.  Edge nhận new_viewer:
        a. Nếu pipeline chưa chạy → khởi động GStreamer pipeline
        b. Nếu shared track chưa tạo → tạo TrackLocalStaticSample
        c. Tạo PeerConnection mới cho "sess-abc" với ICE config (STUN)
        d. Add shared track vào PeerConnection
        e. CreateOffer → SetLocalDescription → gửi offer{ session_id, sdp } lên BE

5.  BE forward offer → Browser (lookup sessions["sess-abc"].browserConn)

6.  Browser nhận offer:
        a. Tạo RTCPeerConnection với ICE config (STUN)
        b. SetRemoteDescription(offer)
        c. CreateAnswer → SetLocalDescription
        d. Gửi answer{ session_id, sdp } lên BE

7.  BE forward answer → Edge (lookup sessions["sess-abc"] → edge_id → edges["edge-01"])
        Edge: SetRemoteDescription(answer)

8.  ICE exchange (diễn ra song song với bước 6-7):
        Edge: onICECandidate → gửi ice_candidate lên BE → BE forward tới Browser
        Browser: onicecandidate → gửi ice_candidate lên BE → BE forward tới Edge
        pion/browser tự chọn đường kết nối tốt nhất (host → srflx)

9.  WebRTC connected:
        fan-out goroutine đọc sample từ appsink → track.WriteSample()
        pion distribute tới từng PeerConnection đang active
        Browser gắn stream vào <video> và hiển thị

10. Browser disconnect:
        BE xóa session khỏi Hub
        Edge nhận tín hiệu (hoặc detect WS close) → đóng PeerConnection, xóa khỏi peers map
        Nếu peers map rỗng → dừng GStreamer pipeline
```

---

## GStreamer Pipeline

```
v4l2src device=/dev/video0 ! videoconvert ! x264enc tune=zerolatency ! appsink name=sink
```

Fan-out goroutine (chạy liên tục khi pipeline active):

```
for {
    sample = appsink.PullSample()
    mu.RLock()
    for _, pc := range peers {
        track.WriteSample(sample)
    }
    mu.RUnlock()
}
```

Một `track.WriteSample()` — pion tự gửi tới tất cả PeerConnection đã add track này.

---

## ICE Config

Áp dụng cho cả **Edge (pion)** và **Browser (JS)**:

```
ICEServers: [ stun:stun.l.google.com:19302 ]
```

Edge sau NAT → STUN trả về srflx candidate (public IP) → Browser kết nối được.
TURN chưa cần ở giai đoạn này.

---

## Project Structure

```
simulation_webrtc/
├── go.mod
├── simulation/
│   ├── be/
│   │   ├── main.go       — start HTTP/WS server, khởi tạo Hub
│   │   ├── hub.go        — Hub struct, edges/sessions map, routing logic
│   │   └── handler.go    — WebSocket upgrader, parse/dispatch messages
│   ├── edge/
│   │   ├── main.go       — connect WS tới BE, khởi tạo EdgeState
│   │   ├── signaling.go  — WS client, read/write loop
│   │   ├── webrtc.go     — tạo PeerConnection per session, quản lý peers map
│   │   └── gstreamer.go  — GStreamer pipeline, appsink fan-out goroutine
│   └── fe/
│       └── index.html    — vanilla JS, WebSocket client, RTCPeerConnection, <video>
└── docs/plans/
```

---

## Dependencies

```
BE:
  github.com/gorilla/websocket

Edge:
  github.com/gorilla/websocket
  github.com/pion/webrtc/v3
  github.com/go-gst/go-gst

Frontend:
  Browser native WebRTC API (no npm, no framework)
```

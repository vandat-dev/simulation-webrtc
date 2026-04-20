# WebRTC Signaling + SFU — Design Doc

## Goal

Build a WebRTC system where a web browser and an edge device are both clients. The backend (BE) acts as a signaling server and SFU (Selective Forwarding Unit). After signaling is complete, the edge captures video from `/dev/video0` via GStreamer and pushes the stream to BE/SFU, which then distributes it to one or more browsers.

---

## System Components

```
BE (public IP)
  ├── Signaling Server  — relay SDP/ICE over WebSocket
  ├── SFU               — receive video from Edge, forward to Browsers
  └── TURN Server       — fallback relay when UDP is blocked

EDGE (behind NAT)
  ├── WebSocket client  — signaling with BE
  ├── GStreamer pipeline — capture /dev/video0
  └── pion/webrtc       — WebRTC publisher to SFU

BROWSER
  ├── WebSocket client  — signaling with BE
  └── WebRTC API        — subscriber from SFU, display <video>
```

---

## API Design

### WebSocket Endpoint

Single endpoint: `ws://BE_HOST/ws`

All messages are JSON. First message after connect must be `register`.

### Message Types

**Edge → BE:**
```json
{ "type": "register",       "role": "edge", "edge_id": "edge-01" }
{ "type": "publish_offer",  "edge_id": "edge-01", "sdp": "..." }
{ "type": "ice_candidate",  "edge_id": "edge-01", "candidate": "..." }
```

**BE → Edge:**
```json
{ "type": "prepare_publish", "edge_id": "edge-01" }
{ "type": "publish_answer",  "edge_id": "edge-01", "sdp": "..." }
{ "type": "ice_candidate",   "edge_id": "edge-01", "candidate": "..." }
```

**Browser → BE:**
```json
{ "type": "register",         "role": "browser", "edge_id": "edge-01", "session_id": "sess-abc" }
{ "type": "subscribe_answer", "session_id": "sess-abc", "sdp": "..." }
{ "type": "ice_candidate",    "session_id": "sess-abc", "candidate": "..." }
```

**BE → Browser:**
```json
{ "type": "subscribe_offer", "session_id": "sess-abc", "sdp": "..." }
{ "type": "ice_candidate",   "session_id": "sess-abc", "candidate": "..." }
```

---

## Architecture & Flow

### Tổng quan

```
EDGE (behind NAT)                BE (public IP)               BROWSER
       │                               │                           │
       │ 1. WebSocket (TCP, qua NAT)   │                           │
       │──────────────────────────────►│                           │
       │    register edge-01           │                           │
       │    [CHỜ, không làm gì thêm]   │                           │
       │                               │   2. WebSocket (TCP)      │
       │                               │◄──────────────────────────│
       │                               │    register, xem edge-01  │
       │                               │                           │
       │◄── new_viewer (sess-abc) ─────│                           │
       │                               │                           │
       │──── publish_offer (SDP) ─────►│                           │
       │◄─── publish_answer (SDP) ─────│                           │
       │◄═══ ICE candidates ══════════►│                           │
       │                               │                           │
       │  [Edge ↔ SFU connected]       │──── subscribe_offer ─────►│
       │                               │◄─── subscribe_answer ─────│
       │                               │◄═══ ICE candidates ══════►│
       │                               │                           │
       │                               │  [SFU ↔ Browser connected]│
       │                               │                           │
       │═══ video (WebRTC) ═══════════►│═══ video (WebRTC) ════════►│
```

---

### PHASE 1: Khởi động

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
STEP 1 — BE khởi động
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  BE start, lắng nghe tại:
    ws://BE_HOST/ws    ← signaling (TCP)
    BE_HOST:3478       ← TURN server (TCP, fallback)

  BE khởi tạo 2 danh sách trống:
    edges    = {}  ← lưu các Edge đang online
    sessions = {}  ← lưu các Browser đang xem


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
STEP 2 — Edge khởi động, kết nối BE và CHỜ
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Edge mở WebSocket tới BE
  → Đây là kết nối TCP outbound, qua NAT được bình thường

  Edge gửi đăng ký:
    { "type": "register", "role": "edge", "edge_id": "edge-01" }

  BE nhận được → lưu lại:
    edges = { "edge-01": <ws conn của Edge> }

  Edge giữ kết nối WebSocket và CHỜ
  → Chưa tạo PeerConnection, chưa bật GStreamer, chưa làm gì thêm
  → Chỉ hành động khi BE thông báo có Browser muốn xem
```

---

### PHASE 2: Browser yêu cầu xem → trigger toàn bộ signaling

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
STEP 3 — Browser mở trang, kết nối BE, yêu cầu xem edge-01
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Browser tự tạo session_id ngẫu nhiên, ví dụ: "sess-abc"
  Browser mở WebSocket tới BE
  Browser gửi đăng ký:
    { "type": "register", "role": "browser",
      "edge_id": "edge-01", "session_id": "sess-abc" }

  BE nhận được → lưu lại:
    sessions = { "sess-abc": { conn: <ws conn>, edge_id: "edge-01" } }

  BE nhìn vào sessions["sess-abc"] → thấy edge_id = "edge-01"
  BE nhìn vào edges["edge-01"]    → thấy ws conn của Edge
  BE gửi thông báo cho Edge:
    { "type": "new_viewer", "session_id": "sess-abc" }

  → Đây là trigger bắt đầu toàn bộ quá trình signaling


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
STEP 4 — Edge tạo PeerConnection và Offer
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Edge khởi động GStreamer pipeline:
    /dev/video0 → v4l2src → videoconvert → x264enc → appsink
    appsink xuất từng frame vào bộ nhớ để pion đọc

  Edge tạo PeerConnection với ICE config:
    ICEServers: [
      { url: "stun:stun.l.google.com:19302" },   ← thử trước
      { url: "turn:BE_HOST:3478",                 ← fallback
        username: "user", credential: "pass" }
    ]

  Edge add video track (từ GStreamer) vào PeerConnection
  Edge tạo Offer SDP — nội dung: "tôi có video H264, chỉ gửi (sendonly)"
  Edge gửi Offer lên BE:
    { "type": "publish_offer", "edge_id": "edge-01", "sdp": "..." }

  LocalDescription của Edge = Offer này


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
STEP 5 — BE/SFU nhận Offer, tạo Answer
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  BE tạo PeerConnection để nhận video từ Edge
  BE set RemoteDescription = Offer của Edge
    → BE hiểu Edge sẽ gửi video H264

  BE tạo Answer SDP — nội dung: "tôi đồng ý nhận video (recvonly)"
  BE set LocalDescription = Answer này
  BE gửi Answer lại cho Edge:
    { "type": "publish_answer", "edge_id": "edge-01", "sdp": "..." }

  Edge nhận được → set RemoteDescription = Answer
    → Edge hiểu BE đã đồng ý nhận video


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
STEP 6 — ICE: Edge tìm đường kết nối tới BE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  pion tự động thu thập 3 loại ICE candidates (chạy song song):

  [Candidate 1] Host — UDP trực tiếp
    Edge dùng IP local của mình
    Chỉ hoạt động nếu Edge có public IP (VPS, cloud VM...)
    → Thường KHÔNG dùng được vì Edge sau NAT

  [Candidate 2] Srflx — qua Google STUN (trường hợp bình thường)
    Edge gửi UDP packet tới stun.l.google.com:19302
    STUN server nhìn thấy packet → trả về public IP của Edge
    STUN trả lời: "public IP của mày là 1.2.3.4:5000"
    → Edge biết IP thật để gửi cho BE
    → Hoạt động khi outbound UDP không bị chặn

  [Candidate 3] Relay — qua TURN tại BE (fallback)
    Google STUN không trả lời được → UDP bị chặn
    Edge kết nối TURN server tại BE qua TCP
    TURN cấp cho Edge một địa chỉ relay
    → Dùng khi UDP bị chặn hoàn toàn

  Sau khi thu thập xong, Edge gửi từng candidate lên BE qua WebSocket:
    { "type": "ice_candidate", "edge_id": "edge-01", "candidate": "..." }
    (gửi nhiều lần, mỗi lần 1 candidate)

  BE cũng gửi candidate của mình lại cho Edge:
    BE có public IP → chỉ có host candidate
    { "type": "ice_candidate", "edge_id": "edge-01", "candidate": "BE_IP:port" }


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
STEP 7 — ICE chọn đường kết nối tốt nhất
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  pion thử kết nối tất cả candidates cùng lúc:

    Candidate 1 (host)  ──UDP trực tiếp────────────► BE
    Candidate 2 (srflx) ──UDP public IP────────────► BE
    Candidate 3 (relay) ──TCP──► TURN──────────────► BE

  Cái nào kết nối được trước → dùng cái đó, hủy các cái còn lại
  Thứ tự ưu tiên: Host > Srflx > Relay
  pion xử lý hoàn toàn tự động — developer không cần viết logic này


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
STEP 8 — Edge ↔ SFU kết nối thành công
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  WebRTC connection được thiết lập giữa Edge và BE/SFU
  Edge bắt đầu stream video liên tục lên BE/SFU
  BE/SFU lưu video track này lại để phân phối cho Browser
```

---

### PHASE 3: Browser đăng ký xem (Subscriber)

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
STEP 9 — Browser mở trang, kết nối BE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Browser tự tạo session_id ngẫu nhiên, ví dụ: "sess-abc"
  Browser mở WebSocket tới BE
  Browser gửi đăng ký:
    { "type": "register", "role": "browser",
      "edge_id": "edge-01", "session_id": "sess-abc" }

  BE nhận được → lưu lại:
    sessions = { "sess-abc": { conn: <ws conn>, edge_id: "edge-01" } }


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
STEP 10 — BE/SFU tạo Offer cho Browser
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  BE tạo PeerConnection mới cho Browser sess-abc
  BE lấy video track đang nhận từ Edge → add vào PeerConnection này
  BE tạo Offer SDP — nội dung: "tôi có video của edge-01, chỉ gửi (sendonly)"
  BE set LocalDescription = Offer này
  BE gửi Offer cho Browser:
    { "type": "subscribe_offer", "session_id": "sess-abc", "sdp": "..." }


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
STEP 11 — Browser tạo Answer
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Browser tạo PeerConnection với ICE config (giống Edge: STUN + TURN)
  Browser set RemoteDescription = Offer của BE
    → Browser hiểu BE sẽ gửi video H264

  Browser tạo Answer SDP — nội dung: "tôi đồng ý nhận video (recvonly)"
  Browser set LocalDescription = Answer này
  Browser gửi Answer lên BE:
    { "type": "subscribe_answer", "session_id": "sess-abc", "sdp": "..." }

  BE nhận được → set RemoteDescription = Answer


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
STEP 12 — ICE: Browser tìm đường kết nối tới BE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Hoàn toàn tương tự Step 6-7 của Edge:

  [Candidate 1] Host    → Browser có public IP (hiếm)
  [Candidate 2] Srflx   → hỏi Google STUN → lấy public IP → UDP tới BE
  [Candidate 3] Relay   → UDP bị chặn → TURN relay qua TCP

  Browser gửi candidates lên BE qua WebSocket
  BE gửi candidates của mình lại cho Browser
  pion chọn đường tốt nhất tự động


━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
STEP 13 — Browser ↔ SFU kết nối thành công
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  WebRTC connection được thiết lập giữa BE/SFU và Browser
  BE/SFU bắt đầu forward video từ Edge sang Browser sess-abc
  Browser hiển thị video trong thẻ <video>
```

---

### PHASE 4: Nhiều Browser cùng xem

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
STEP 14 — Browser B, C đăng ký (lặp lại Step 9-13)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Mỗi Browser mới lặp lại step 9-13 với session_id khác nhau
  BE/SFU tạo thêm PeerConnection cho mỗi Browser

  /dev/video0
      │
      ▼
  GStreamer (Edge)
      │
      ▼
  pion (Edge) ──1 luồng duy nhất──► BE/SFU ──► Browser A (sess-abc)
                                           ──► Browser B (sess-xyz)
                                           ──► Browser C (sess-def)

  Edge chỉ upload 1 luồng dù có bao nhiêu Browser ✅
```

---

## ICE Fallback Summary

```
Scenario                        Path Used
────────────────────────────────────────────────────────
Edge/Browser has public IP      Direct UDP (fastest)
Edge/Browser behind NAT         STUN → discover public IP → UDP
UDP completely blocked          TURN at BE → TCP relay (fallback)

Note: STUN is always Google's (stun.l.google.com:19302)
      No self-hosted STUN needed at BE
      If Google STUN fails → UDP is blocked → jump directly to TURN
```

---

## BE Internal State

```go
type Hub struct {
    edges    map[string]*EdgeClient    // edge_id → { wsConn, peerConn, videoTrack }
    sessions map[string]*Session       // session_id → { wsConn, edge_id, peerConn }
}
```

---

## Domain Entities

**EdgeClient**
- `edge_id` string
- `wsConn` WebSocket connection
- `peerConn` pion PeerConnection (publisher)
- `videoTrack` pion TrackLocalStaticRTP

**Session**
- `session_id` string
- `edge_id` string
- `wsConn` WebSocket connection
- `peerConn` pion PeerConnection (subscriber)

---

## GStreamer Pipeline (Edge)

```
v4l2src device=/dev/video0
  ! videoconvert
  ! x264enc tune=zerolatency
  ! appsink name=sink
```

- `v4l2src`: reads raw frames from `/dev/video0`
- `videoconvert`: converts color space for encoder
- `x264enc tune=zerolatency`: H.264 encode with minimal buffering
- `appsink`: outputs frames into Go memory buffer via `go-gst`

pion reads each sample from appsink and writes to `VideoTrack`.

---

## Dependencies

| Component | Library |
|-----------|---------|
| BE signaling | `gorilla/websocket` |
| BE SFU + TURN | `pion/webrtc`, `pion/turn` |
| Edge WebRTC | `pion/webrtc` |
| Edge GStreamer | `go-gst/gst` |
| Frontend | Browser native WebRTC API (vanilla JS) |
| STUN | `stun.l.google.com:19302` (Google, free) |

---

## Project Structure

```
simulation_webrtc/
├── be/
│   ├── main.go
│   ├── hub.go        ← manages edges + sessions maps
│   └── handler.go    ← WebSocket handler, SFU logic
├── edge/
│   ├── main.go
│   ├── signaling.go  ← WebSocket client to BE
│   ├── webrtc.go     ← pion PeerConnection (publisher)
│   └── gstreamer.go  ← GStreamer pipeline → appsink → pion track
└── fe/
    └── index.html    ← vanilla JS, WebSocket + WebRTC API, <video> tag
```

---

## Trade-off Analysis

**SFU at BE vs P2P:**
- P2P: Edge uploads N streams (1 per browser) — does not scale
- SFU: Edge uploads 1 stream, BE distributes — scales well
- Chosen SFU because it matches real-world camera streaming use case

**STUN strategy:**
- Use Google STUN directly from Edge/Browser — free, reliable, no maintenance
- No self-hosted STUN at BE — pointless because if Google STUN fails, UDP is blocked, STUN at BE also fails
- TURN at BE as final fallback for UDP-blocked environments

# WebRTC Step-by-step Flow

---

## PHASE 1: Khởi động

```
BƯỚC 1: BE khởi động
━━━━━━━━━━━━━━━━━━━━
BE start server, lắng nghe tại ws://localhost:9090/ws
BE chuẩn bị 2 cái "danh sách trống":
  - danh sách edges:    {}  ← chưa có edge nào
  - danh sách sessions: {}  ← chưa có browser nào


BƯỚC 2: Edge khởi động
━━━━━━━━━━━━━━━━━━━━━━
Edge boot lên, mở kết nối WebSocket tới BE
Edge gửi lời chào:
  { "type": "register", "role": "edge", "edge_id": "edge-01" }

BE nhận được → lưu vào danh sách:
  edges = { "edge-01": <conn của edge> }

Edge giữ kết nối WebSocket và CHỜ
→ Chưa tạo PeerConnection, chưa bật GStreamer, chưa làm gì thêm
→ Chỉ hành động khi BE thông báo có Browser muốn xem


BƯỚC 3: Browser mở trang web
━━━━━━━━━━━━━━━━━━━━━━━━━━━━
User mở index.html trên Chrome
Browser tự tạo một session_id ngẫu nhiên, ví dụ: "sess-abc"
Browser mở kết nối WebSocket tới BE
Browser gửi lời chào:
  { "type": "register", "role": "browser",
    "edge_id": "edge-01", "session_id": "sess-abc" }

BE nhận được → lưu vào danh sách:
  sessions = { "sess-abc": { browserConn: <conn>, edge_id: "edge-01" } }
```

---

## PHASE 2: Signaling (trao đổi thông tin kết nối)

```
BƯỚC 4: BE thông báo Edge có viewer mới
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
BE nhìn vào sessions["sess-abc"] → thấy edge_id = "edge-01"
BE nhìn vào edges["edge-01"]    → thấy conn của Edge
BE gửi cho Edge:
  { "type": "new_viewer", "session_id": "sess-abc" }

→ Đây là trigger bắt đầu toàn bộ quá trình signaling


BƯỚC 5: Edge tạo PeerConnection và Offer
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Edge nhận được new_viewer
Edge khởi động GStreamer pipeline:
  /dev/video0 → v4l2src → videoconvert → x264enc → appsink
Edge tạo 1 PeerConnection mới cho session "sess-abc"
Edge add video track vào PeerConnection (track này lấy từ GStreamer appsink)
Edge tạo Offer SDP (bản mô tả: "tôi có video H264, tôi chỉ gửi")
Edge set LocalDescription = Offer này
Edge gửi Offer lên BE:
  { "type": "offer", "session_id": "sess-abc", "sdp": "v=0\r\no=..." }


BƯỚC 6: BE forward Offer sang Browser
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
BE nhìn vào sessions["sess-abc"] → thấy browserConn
BE forward y nguyên sang Browser:
  { "type": "offer", "session_id": "sess-abc", "sdp": "v=0\r\no=..." }


BƯỚC 7: Browser tạo Answer
━━━━━━━━━━━━━━━━━━━━━━━━━━
Browser nhận Offer
Browser tạo PeerConnection với ICE config:
  ICEServers: [
    { url: "stun:stun.l.google.com:19302" },  ← thử trước
    { url: "turn:BE_HOST:3478",                ← fallback nếu UDP bị chặn
      username: "user", credential: "pass" }
  ]
Browser set RemoteDescription = Offer vừa nhận
  → Browser hiểu Edge sẽ gửi video H264
Browser tạo Answer SDP (bản trả lời: "OK tôi đồng ý nhận video")
Browser set LocalDescription = Answer này
Browser gửi Answer lên BE:
  { "type": "answer", "session_id": "sess-abc", "sdp": "v=0\r\no=..." }


BƯỚC 8: BE forward Answer sang Edge
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
BE nhìn vào sessions["sess-abc"] → thấy edge_id = "edge-01"
BE nhìn vào edges["edge-01"]    → thấy conn của Edge
BE forward y nguyên sang Edge:
  { "type": "answer", "session_id": "sess-abc", "sdp": "v=0\r\no=..." }

Edge nhận Answer → set RemoteDescription
  → Edge hiểu Browser đã đồng ý nhận video


BƯỚC 9: Trao đổi ICE Candidates
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
ICE candidate là gì?
→ Là địa chỉ IP + port mà mỗi bên có thể dùng để kết nối tới nhau
→ Mỗi bên thu thập 3 loại candidate (chạy song song):

  [Host]  IP local của mình
          → chỉ dùng được nếu có public IP trực tiếp

  [Srflx] Hỏi Google STUN qua UDP → lấy public IP
          → dùng khi Edge/Browser sau NAT, UDP ra ngoài được

  [Relay] Kết nối TURN tại BE qua TCP
          → fallback khi UDP bị chặn hoàn toàn

Edge thu thập candidates → gửi lên BE → BE forward sang Browser:
  { "type": "ice_candidate", "session_id": "sess-abc", "candidate": "1.2.3.4:5000" }

Browser thu thập candidates → gửi lên BE → BE forward sang Edge:
  { "type": "ice_candidate", "session_id": "sess-abc", "candidate": "5.6.7.8:9000" }

pion thử kết nối tất cả candidates cùng lúc, chọn cái tốt nhất
→ Developer không cần xử lý logic này, pion tự động làm

(Bước 9 diễn ra song song với bước 7-8, không cần đợi nhau)
```

---

## PHASE 3: Kết nối và Stream video

```
BƯỚC 10: WebRTC kết nối thành công
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Edge và Browser đã trao đổi xong SDP + ICE candidates
pion chọn được đường kết nối tốt nhất (host / srflx / relay)
WebRTC connection được thiết lập
→ BE không tham gia vào luồng video, chỉ giữ WebSocket connections


BƯỚC 11: GStreamer bắt đầu capture và stream
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
/dev/video0 → v4l2src → videoconvert → x264enc → appsink
                                                      ↓
                                              pion đọc từng frame
                                                      ↓
                                          gửi qua WebRTC video track
                                                      ↓
                                               tới Browser


BƯỚC 12: Browser hiển thị video
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Browser nhận WebRTC stream
Gắn stream vào thẻ <video> trong HTML
User thấy video từ /dev/video0 của Edge
```

---

## Tóm tắt timeline

```
t=0s   Edge connect BE, đăng ký "edge-01", CHỜ
t=1s   Browser mở trang, connect BE, đăng ký muốn xem "edge-01"
t=1s   BE báo Edge: có viewer mới "sess-abc"  ← trigger bắt đầu
t=1s   Edge bật GStreamer, tạo PeerConnection, tạo offer → BE → Browser
t=1s   Browser tạo answer → BE → Edge
t=1s   ICE candidates trao đổi qua BE (song song với offer/answer)
t=2s   WebRTC kết nối thành công
t=2s   Video bắt đầu chảy từ Edge → Browser
t=...  BE giữ WebSocket connections, không tham gia vào video
```

---

## ICE Fallback

```
Tình huống                      Con đường được chọn
────────────────────────────────────────────────────────────
Edge/Browser có public IP       Host candidate (UDP trực tiếp)
Edge/Browser sau NAT            Srflx candidate (STUN → UDP public)
UDP bị chặn hoàn toàn           Relay candidate (TURN → TCP tại BE)

Lưu ý:
  - STUN luôn dùng của Google: stun.l.google.com:19302
  - Không cần tự host STUN tại BE
  - Nếu Google STUN không đến được → UDP bị chặn → nhảy thẳng TURN
  - pion xử lý toàn bộ tự động, developer chỉ cần khai báo ICE config
```

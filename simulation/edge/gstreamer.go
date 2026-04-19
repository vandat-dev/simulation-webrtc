package main

import (
	"fmt"
	"log"
	"time"

	"github.com/go-gst/go-gst/gst"
	"github.com/go-gst/go-gst/gst/app"
	"github.com/pion/webrtc/v3/pkg/media"
)

// startPipeline builds and starts the GStreamer pipeline.
// Called once from main before connecting to BE.
func (e *Edge) startPipeline() error {
	if e.pipeline != nil {
		return nil
	}

	// Low-latency tuning:
	//   speed-preset=ultrafast  — encode càng nhanh càng tốt
	//   key-int-max=30          — keyframe mỗi 30 frames (~1s), giúp browser decode ngay
	//   bitrate=2000            — 2Mbps, đủ cho 720p/30fps LAN
	//   sync=false              — appsink không đợi pipeline clock, push ngay khi có frame
	//   drop=true max-buffers=1 — nếu pion write chậm, bỏ frame cũ thay vì block pipeline
	pipelineStr := fmt.Sprintf(
		"v4l2src device=%s ! videoconvert ! "+
			"x264enc tune=zerolatency speed-preset=ultrafast key-int-max=30 bitrate=2000 ! "+
			"video/x-h264,stream-format=byte-stream ! "+
			"appsink name=sink sync=false drop=true max-buffers=1",
		e.device,
	)

	pipeline, err := gst.NewPipelineFromString(pipelineStr)
	if err != nil {
		return fmt.Errorf("new pipeline: %w", err)
	}

	sinkElem, err := pipeline.GetElementByName("sink")
	if err != nil || sinkElem == nil {
		return fmt.Errorf("appsink element not found in pipeline")
	}
	sink := app.SinkFromElement(sinkElem)

	if err := pipeline.SetState(gst.StatePlaying); err != nil {
		return fmt.Errorf("pipeline failed to transition to PLAYING: %w", err)
	}

	e.pipeline = pipeline
	log.Println("[gstreamer] pipeline started:", pipelineStr)

	go e.sampleLoop(sink)
	return nil
}

// sampleLoop pulls encoded H264 frames from appsink and writes them to the
// shared pion track. pion fans out each sample to all active PeerConnections.
func (e *Edge) sampleLoop(sink *app.Sink) {
	for {
		sample := sink.PullSample()
		if sample == nil {
			log.Println("[gstreamer] appsink closed")
			return
		}

		buf := sample.GetBuffer()
		if buf == nil {
			continue
		}

		// Duration từ GStreamer buffer; fallback 33ms (~30fps)
		d := time.Duration(buf.Duration())
		if d <= 0 || d > time.Second {
			d = 33 * time.Millisecond
		}

		// Copy bytes ra khỏi GStreamer memory trước khi buffer bị free
		raw := buf.Bytes()
		data := make([]byte, len(raw))
		copy(data, raw)

		if err := e.track.WriteSample(media.Sample{
			Data:     data,
			Duration: d,
		}); err != nil {
			log.Println("[gstreamer] write sample error:", err)
		}
	}
}

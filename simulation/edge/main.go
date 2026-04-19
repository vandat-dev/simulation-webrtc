package main

import (
	"log"
	"os"

	"github.com/go-gst/go-gst/gst"
)

func main() {
	gst.Init(nil)

	beURL := getEnv("BE_URL", "ws://localhost:9090/ws")
	edgeID := getEnv("EDGE_ID", "edge-01")
	device := getEnv("VIDEO_DEVICE", "/dev/video0")

	edge, err := NewEdge(edgeID, device)
	if err != nil {
		log.Fatal("new edge:", err)
	}

	// Khởi GStreamer ngay từ đầu — không cần chờ viewer.
	if err := edge.startPipeline(); err != nil {
		log.Fatal("start pipeline:", err)
	}

	log.Printf("connecting to BE: %s as edge %s (device: %s)", beURL, edgeID, device)
	if err := edge.Connect(beURL); err != nil {
		log.Fatal("connect:", err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

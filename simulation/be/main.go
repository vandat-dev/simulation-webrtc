package main

import (
	"log"
	"net/http"
)

func main() {
	hub := NewHub()
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		HandleWS(hub, w, r)
	})
	http.Handle("/", http.FileServer(http.Dir("simulation/fe")))
	log.Println("BE signaling server listening on :9090")
	log.Println("FE available at http://localhost:9090/?edge_id=edge-01")
	log.Fatal(http.ListenAndServe(":9090", nil))
}

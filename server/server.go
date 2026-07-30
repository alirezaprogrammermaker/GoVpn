package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type Response struct {
	Message  string            `json:"message"`
	Method   string            `json:"method"`
	Path     string            `json:"path"`
	Headers  map[string][]string `json:"headers"`
	Source   string            `json:"source"`
}

func handler(w http.ResponseWriter, r *http.Request) {
	log.Printf("[SERVER] 📩 Received: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

	resp := Response{
		Message: "Hello from target server!",
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: r.Header,
		Source:  "proxied-request",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	jsonBytes, _ := json.MarshalIndent(resp, "", "  ")
	w.Write(jsonBytes)

	log.Printf("[SERVER] ✅ Response sent successfully")
}

func main() {
	port := ":9090"
	fmt.Println("🎯 Target server starting on", port)
	http.HandleFunc("/", handler)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("❌ Failed to start server: %v", err)
	}
}

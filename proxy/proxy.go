package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

func handleProxy(w http.ResponseWriter, r *http.Request) {
	log.Printf("[PROXY] %s %s", r.Method, r.URL.String())

	// Create a new request to the target server
	targetReq, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
	if err != nil {
		http.Error(w, "Failed to create request: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Copy original headers
	for key, values := range r.Header {
		for _, value := range values {
			targetReq.Header.Add(key, value)
		}
	}

	// Send request to target server
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(targetReq)
	if err != nil {
		http.Error(w, "Failed to reach target: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers back to client
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	bytesWritten, err := io.Copy(w, resp.Body)
	if err != nil {
		log.Printf("[PROXY] Error copying response: %v", err)
		return
	}

	log.Printf("[PROXY] ✅ Response sent: %d bytes (status: %d)", bytesWritten, resp.StatusCode)
}

func main() {
	port := ":8080"
	fmt.Println("🚀 Proxy server starting on", port)
	fmt.Println("   Waiting for requests...")

	http.HandleFunc("/", handleProxy)

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("❌ Failed to start proxy: %v", err)
	}
}

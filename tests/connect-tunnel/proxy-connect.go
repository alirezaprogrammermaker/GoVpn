package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// handleConnect handles HTTPS CONNECT tunneling
func handleConnect(w http.ResponseWriter, r *http.Request) {
	log.Printf("[CONNECT] %s → %s", r.RemoteAddr, r.Host)

	// Connect to target server
	targetConn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		http.Error(w, "Failed to connect: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer targetConn.Close()

	// Hijack the connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "Hijack failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Send 200 Connection Established
	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		log.Printf("[CONNECT] Failed to send 200: %v", err)
		return
	}

	log.Printf("[CONNECT] ✅ Tunnel established: %s ↔ %s", r.RemoteAddr, r.Host)

	// Bidirectional copy
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(targetConn, clientConn)
	}()

	go func() {
		defer wg.Done()
		io.Copy(clientConn, targetConn)
	}()

	wg.Wait()
	log.Printf("[CONNECT] 🔒 Tunnel closed: %s", r.Host)
}

// handleHTTP handles regular HTTP requests (forward proxy)
func handleHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("[HTTP] %s %s", r.Method, r.URL.String())

	// Create outgoing request
	outReq, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Copy headers
	for key, values := range r.Header {
		for _, value := range values {
			outReq.Header.Add(key, value)
		}
	}

	// Send request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(outReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

	log.Printf("[HTTP] ✅ Response: %d", resp.StatusCode)
}

func main() {
	port := ":8081"
	fmt.Println("🚀 CONNECT-capable proxy starting on", port)
	fmt.Println("   Supports: HTTP forward + HTTPS CONNECT tunneling")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			handleConnect(w, r)
		} else {
			handleHTTP(w, r)
		}
	})

	if err := http.ListenAndServe(port, handler); err != nil {
		log.Fatalf("❌ Failed to start: %v", err)
	}
}

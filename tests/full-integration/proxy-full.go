package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	listenPort = ":8082"
	cfWorker   = "https://govpn-worker.social-panel.workers.dev"
)

// ═══════════════════════════════════════════════════════════════
//  CONNECT Handler (HTTPS Tunneling)
// ═══════════════════════════════════════════════════════════════

func handleConnect(w http.ResponseWriter, r *http.Request) {
	log.Printf("[CONNECT] 🔗 %s → %s", r.RemoteAddr, r.Host)

	// Connect to target
	targetConn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		http.Error(w, "Connection failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer targetConn.Close()

	// Hijack client connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijack not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "Hijack failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Send 200 OK
	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		return
	}

	log.Printf("[CONNECT] ✅ Tunnel: %s ↔ %s", r.RemoteAddr, r.Host)

	// Bidirectional copy
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(targetConn, clientConn) }()
	go func() { defer wg.Done(); io.Copy(clientConn, targetConn) }()
	wg.Wait()

	log.Printf("[CONNECT] 🔒 Closed: %s", r.Host)
}

// ═══════════════════════════════════════════════════════════════
//  HTTP Forward Handler
// ═══════════════════════════════════════════════════════════════

func handleHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("[HTTP] %s %s", r.Method, r.URL.String())

	outReq, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	for key, values := range r.Header {
		for _, value := range values {
			outReq.Header.Add(key, value)
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(outReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

	log.Printf("[HTTP] ✅ %d", resp.StatusCode)
}

// ═══════════════════════════════════════════════════════════════
//  CF Worker Proxy Handler (bypass censorship)
// ═══════════════════════════════════════════════════════════════

func handleCFProxy(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		// Return API info
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":    "GoVpn Full Proxy",
			"version": "1.0.0",
			"endpoints": map[string]string{
				"/":           "This info",
				"/health":     "Health check",
				"/cf?url=...": "Forward through CF Worker",
			},
			"features": []string{
				"HTTP Forward Proxy",
				"HTTPS CONNECT Tunneling",
				"CF Worker Bypass",
				"Base64 URL support",
			},
		})
		return
	}

	// Try base64 decode
	if decoded, err := base64.StdEncoding.DecodeString(targetURL); err == nil {
		targetURL = string(decoded)
	}

	log.Printf("[CF-BYPASS] 🚀 %s → %s", r.RemoteAddr, targetURL)

	// Forward through CF Worker
	cfURL := fmt.Sprintf("%s/proxy?url=%s", cfWorker, url.QueryEscape(targetURL))
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(cfURL)
	if err != nil {
		http.Error(w, "CF Worker failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("X-Proxy-By", "GoVpn-Full")
	w.Header().Set("X-Via", "CF-Worker")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)

	log.Printf("[CF-BYPASS] ✅ %d (%d bytes)", resp.StatusCode, len(body))
}

// ═══════════════════════════════════════════════════════════════
//  Health Check
// ═══════════════════════════════════════════════════════════════

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"message":   "GoVpn Full Proxy is alive! 🚀",
		"timestamp": time.Now().Format(time.RFC3339),
		"port":      listenPort,
	})
}

// ═══════════════════════════════════════════════════════════════
//  Main Router
// ═══════════════════════════════════════════════════════════════

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║           GoVpn Full Proxy Server                           ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Features:                                                  ║")
	fmt.Println("║    ✅ HTTP Forward Proxy                                    ║")
	fmt.Println("║    ✅ HTTPS CONNECT Tunneling                               ║")
	fmt.Println("║    ✅ CF Worker Bypass                                      ║")
	fmt.Println("║    ✅ Base64 URL Encoding                                   ║")
	fmt.Printf("║  Listening on: %-43s  ║\n", listenPort)
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodConnect:
			handleConnect(w, r)
		case r.URL.Path == "/health":
			handleHealth(w, r)
		case r.URL.Path == "/cf":
			handleCFProxy(w, r)
		default:
			handleHTTP(w, r)
		}
	})

	if err := http.ListenAndServe(listenPort, handler); err != nil {
		log.Fatalf("❌ Server failed: %v", err)
	}
}

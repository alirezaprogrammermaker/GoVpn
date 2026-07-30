package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	listenPort = ":8083"
	relayURL   = "https://govpn-relay.social-panel.workers.dev"
)

// ═══════════════════════════════════════════════════════════════
//  Relay Client (same as runner, but used by the proxy)
// ═══════════════════════════════════════════════════════════════

type RelayClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewRelayClient(baseURL string) *RelayClient {
	return &RelayClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type EnqueueRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    *string           `json:"body,omitempty"`
}

type EnqueueResponse struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Position int    `json:"position"`
}

type ResultResponse struct {
	ID      string            `json:"id"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// Enqueue submits a request to the relay queue
func (c *RelayClient) Enqueue(req EnqueueRequest) (*EnqueueResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal enqueue: %w", err)
	}

	resp, err := c.httpClient.Post(c.baseURL+"/enqueue", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("enqueue request: %w", err)
	}
	defer resp.Body.Close()

	var enqueueResp EnqueueResponse
	if err := json.NewDecoder(resp.Body).Decode(&enqueueResp); err != nil {
		return nil, fmt.Errorf("decode enqueue response: %w", err)
	}

	return &enqueueResp, nil
}

// WaitForResult polls for the response (with timeout)
func (c *RelayClient) WaitForResult(id string, timeout time.Duration) (*ResultResponse, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := c.httpClient.Get(fmt.Sprintf("%s/result/%s", c.baseURL, id))
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			time.Sleep(500 * time.Millisecond)
			continue
		}
		resp.Body.Close()

		// Check if we got the full response
		if status, ok := result["status"].(float64); ok && status > 0 {
			headers := make(map[string]string)
			if h, ok := result["headers"].(map[string]interface{}); ok {
				for k, v := range h {
					headers[k] = fmt.Sprintf("%v", v)
				}
			}

			body, _ := result["body"].(string)

			return &ResultResponse{
				ID:      id,
				Status:  int(status),
				Headers: headers,
				Body:    body,
			}, nil
		}

		// Still processing
		time.Sleep(500 * time.Millisecond)
	}

	return nil, fmt.Errorf("timeout waiting for result after %v", timeout)
}

// ═══════════════════════════════════════════════════════════════
//  Proxy Handler
// ═══════════════════════════════════════════════════════════════

type ProxyHandler struct {
	relay *RelayClient
}

func NewProxyHandler(relayURL string) *ProxyHandler {
	return &ProxyHandler{
		relay: NewRelayClient(relayURL),
	}
}

func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/health":
		p.handleHealth(w, r)
	case r.URL.Path == "/relay":
		p.handleRelayProxy(w, r)
	default:
		p.handleDirectProxy(w, r)
	}
}

// Health check for the local proxy
func (p *ProxyHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": "GoVpn Relay Proxy is alive! 🚀",
		"port":    listenPort,
		"relay":   relayURL,
	})
}

// Relay proxy - routes request through GitHub Actions runner
func (p *ProxyHandler) handleRelayProxy(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		// Show usage
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":    "GoVpn Relay Proxy",
			"version": "1.0.0",
			"endpoints": map[string]string{
				"/":             "This info",
				"/health":       "Health check",
				"/relay?url=...": "Forward through GH Actions relay",
				"/<any>":        "Direct proxy (no relay)",
			},
			"usage": "GET /relay?url=https://example.com",
		})
		return
	}

	log.Printf("[RELAY] 🚀 %s → %s", r.RemoteAddr, targetURL)

	// Collect request headers
	headers := make(map[string]string)
	for key := range r.Header {
		headers[key] = r.Header.Get(key)
	}

	// Read request body if present
	var bodyStr *string
	if r.Body != nil && r.Method != "GET" && r.Method != "HEAD" {
		bodyBytes, err := io.ReadAll(r.Body)
		if err == nil && len(bodyBytes) > 0 {
			s := string(bodyBytes)
			bodyStr = &s
		}
	}

	// Enqueue the request
	enqueueResp, err := p.relay.Enqueue(EnqueueRequest{
		Method:  r.Method,
		URL:     targetURL,
		Headers: headers,
		Body:    bodyStr,
	})
	if err != nil {
		log.Printf("[RELAY] ❌ Enqueue failed: %v", err)
		http.Error(w, "Failed to enqueue: "+err.Error(), http.StatusBadGateway)
		return
	}

	log.Printf("[RELAY] 📥 Enqueued: %s (position: %d)", enqueueResp.ID, enqueueResp.Position)

	// Wait for result (up to 60 seconds)
	result, err := p.relay.WaitForResult(enqueueResp.ID, 60*time.Second)
	if err != nil {
		log.Printf("[RELAY] ⏰ Timeout: %v", err)
		http.Error(w, "Timeout waiting for relay response: "+err.Error(), http.StatusGatewayTimeout)
		return
	}

	log.Printf("[RELAY] ✅ Response: %d (%d bytes)", result.Status, len(result.Body))

	// Write response back to client
	for key, value := range result.Headers {
		w.Header().Set(key, value)
	}
	w.Header().Set("X-Proxy-By", "GoVpn-Relay")
	w.Header().Set("X-Via", "GH-Actions")
	w.Header().Set("X-Request-Id", result.ID)
	w.WriteHeader(result.Status)
	w.Write([]byte(result.Body))
}

// Direct proxy (existing functionality - no relay)
func (p *ProxyHandler) handleDirectProxy(w http.ResponseWriter, r *http.Request) {
	log.Printf("[DIRECT] %s %s", r.Method, r.URL.String())

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
}

// ═══════════════════════════════════════════════════════════════
//  Main
// ═══════════════════════════════════════════════════════════════

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║           GoVpn Relay Proxy Server                           ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Features:                                                  ║")
	fmt.Println("║    ✅ HTTP/HTTPS Forward Proxy                              ║")
	fmt.Println("║    ✅ GitHub Actions Relay (via CF Worker)                  ║")
	fmt.Println("║    ✅ Async Request/Response Pattern                        ║")
	fmt.Printf("║  Listening on: %-43s  ║\n", listenPort)
	fmt.Printf("║  Relay URL:    %-43s  ║\n", relayURL)
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	handler := NewProxyHandler(relayURL)

	// Handle CONNECT for HTTPS tunneling
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			handleConnect(w, r)
		} else {
			handler.ServeHTTP(w, r)
		}
	})

	server := &http.Server{
		Addr:         listenPort,
		Handler:      mux,
		ReadTimeout:  90 * time.Second,
		WriteTimeout: 90 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("❌ Server failed: %v", err)
	}
}

// ═══════════════════════════════════════════════════════════════
//  CONNECT Handler (HTTPS Tunneling - direct, not via relay)
// ═══════════════════════════════════════════════════════════════

func handleConnect(w http.ResponseWriter, r *http.Request) {
	log.Printf("[CONNECT] 🔗 %s → %s", r.RemoteAddr, r.Host)

	// For CONNECT tunnels, we connect directly (raw TCP).
	// The relay is only for HTTP request/response forwarding.
	targetConn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		http.Error(w, "Connection failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer targetConn.Close()

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

	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		return
	}

	log.Printf("[CONNECT] ✅ Tunnel: %s ↔ %s", r.RemoteAddr, r.Host)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(targetConn, clientConn) }()
	go func() { defer wg.Done(); io.Copy(clientConn, targetConn) }()
	wg.Wait()

	log.Printf("[CONNECT] 🔒 Closed: %s", r.Host)
}

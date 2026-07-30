package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ═══════════════════════════════════════════════════════════════
//  Configuration
// ═══════════════════════════════════════════════════════════════

var (
	relayURL   string
	runnerID   string
	pollDelay  time.Duration
	reqTimeout time.Duration
	maxRuntime time.Duration
)

func init() {
	flag.StringVar(&relayURL, "relay", envOrDefault("RELAY_URL", "https://govpn-relay.social-panel.workers.dev"),
		"CF Worker relay URL")
	flag.StringVar(&runnerID, "id", envOrDefault("RUNNER_ID", fmt.Sprintf("runner-%d", time.Now().Unix())),
		"Runner ID for identification")
	flag.DurationVar(&pollDelay, "poll-delay", 500*time.Millisecond,
		"Delay between polling attempts")
	flag.DurationVar(&reqTimeout, "req-timeout", 30*time.Second,
		"Timeout for each HTTP request to target")
	flag.DurationVar(&maxRuntime, "max-runtime", 5*time.Hour+50*time.Minute,
		"Maximum runtime before graceful shutdown")
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ═══════════════════════════════════════════════════════════════
//  Relay API Types
// ═══════════════════════════════════════════════════════════════

type PendingRequest struct {
	ID        string            `json:"id"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Body      *string           `json:"body"`
	Timestamp int64             `json:"timestamp"`
	Status    string            `json:"status"`
}

type PollResponse struct {
	Empty bool `json:"empty"`
	PendingRequest
}

type RelayResponse struct {
	ID      string            `json:"id"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// ═══════════════════════════════════════════════════════════════
//  Relay Client
// ═══════════════════════════════════════════════════════════════

type RelayClient struct {
	baseURL    string
	runnerID   string
	httpClient *http.Client
}

func NewRelayClient(baseURL, runnerID string) *RelayClient {
	return &RelayClient{
		baseURL:  baseURL,
		runnerID: runnerID,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Poll requests the next pending item from the relay
func (c *RelayClient) Poll() (*PollResponse, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/poll", nil)
	if err != nil {
		return nil, fmt.Errorf("create poll request: %w", err)
	}
	req.Header.Set("X-Runner-Id", c.runnerID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll request: %w", err)
	}
	defer resp.Body.Close()

	var pollResp PollResponse
	if err := json.NewDecoder(resp.Body).Decode(&pollResp); err != nil {
		return nil, fmt.Errorf("decode poll response: %w", err)
	}

	return &pollResp, nil
}

// SubmitResponse posts the execution result back to the relay
func (c *RelayClient) SubmitResponse(id string, relayResp RelayResponse) error {
	body, err := json.Marshal(relayResp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/response/"+id, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create response request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Runner-Id", c.runnerID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("submit response: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("submit response failed: %d %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// Health checks relay connectivity
func (c *RelayClient) Health() error {
	resp, err := c.httpClient.Get(c.baseURL + "/health")
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: %d", resp.StatusCode)
	}

	return nil
}

// ═══════════════════════════════════════════════════════════════
//  Request Executor
// ═══════════════════════════════════════════════════════════════

func executeRequest(pending *PendingRequest) RelayResponse {
	logf("⚡ Executing: %s %s", pending.Method, pending.URL)

	client := &http.Client{
		Timeout: reqTimeout,
		// Don't follow redirects automatically - let the client handle them
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	var bodyReader io.Reader
	if pending.Body != nil {
		bodyReader = bytes.NewReader([]byte(*pending.Body))
	}

	req, err := http.NewRequest(pending.Method, pending.URL, bodyReader)
	if err != nil {
		logf("❌ Failed to create request: %v", err)
		return RelayResponse{
			ID:     pending.ID,
			Status: 502,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: fmt.Sprintf(`{"error": "Failed to create request: %s"}`, err.Error()),
		}
	}

	// Set headers from the original request
	for key, value := range pending.Headers {
		// Skip hop-by-hop headers
		if isHopByHopHeader(key) {
			continue
		}
		req.Header.Set(key, value)
	}

	// Set a identifying header
	req.Header.Set("X-Relay-By", "GoVpn-GH-Actions")
	req.Header.Set("X-Runner-Id", runnerID)

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		logf("❌ Request failed (%v): %v", elapsed, err)
		return RelayResponse{
			ID:     pending.ID,
			Status: 502,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: fmt.Sprintf(`{"error": "Request failed: %s", "elapsed_ms": %d}`, err.Error(), elapsed.Milliseconds()),
		}
	}
	defer resp.Body.Close()

	// Read response body (limit to 10MB)
	limitedBody := io.LimitReader(resp.Body, 10*1024*1024)
	bodyBytes, err := io.ReadAll(limitedBody)
	if err != nil {
		logf("❌ Failed to read response body: %v", err)
		return RelayResponse{
			ID:     pending.ID,
			Status: 502,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: fmt.Sprintf(`{"error": "Failed to read response: %s"}`, err.Error()),
		}
	}

	// Collect response headers
	headers := make(map[string]string)
	for key := range resp.Header {
		if !isHopByHopHeader(key) {
			headers[key] = resp.Header.Get(key)
		}
	}

	logf("✅ Response: %d (%d bytes, %v)", resp.StatusCode, len(bodyBytes), elapsed)

	return RelayResponse{
		ID:      pending.ID,
		Status:  resp.StatusCode,
		Headers: headers,
		Body:    string(bodyBytes),
	}
}

func isHopByHopHeader(key string) bool {
	switch http.CanonicalHeaderKey(key) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Te", "Trailers", "Transfer-Encoding", "Upgrade":
		return true
	}
	return false
}

// ═══════════════════════════════════════════════════════════════
//  Main Loop
// ═══════════════════════════════════════════════════════════════

func main() {
	flag.Parse()

	printBanner()

	// Check relay connectivity
	client := NewRelayClient(relayURL, runnerID)
	if err := client.Health(); err != nil {
		logf("⚠️  Relay health check failed: %v", err)
		logf("    Continuing anyway - relay might not be deployed yet")
	} else {
		logf("✅ Relay is healthy")
	}

	// Main polling loop
	startTime := time.Now()
	requestCount := 0
	errorCount := 0

	logf("🔄 Starting polling loop (poll delay: %v)", pollDelay)

	for {
		// Check max runtime
		elapsed := time.Since(startTime)
		if elapsed >= maxRuntime {
			logf("⏰ Max runtime reached (%v). Shutting down.", maxRuntime)
			break
		}

		remaining := maxRuntime - elapsed
		if remaining < 10*time.Minute {
			logf("⏰ Shutting down in %v...", remaining)
		}

		// Poll for pending requests
		pollResp, err := client.Poll()
		if err != nil {
			errorCount++
			logf("❌ Poll error (#%d): %v", errorCount, err)
			time.Sleep(pollDelay * 2) // Back off on error
			continue
		}

		if pollResp.Empty {
			// No pending requests - wait and try again
			time.Sleep(pollDelay)
			continue
		}

		// We got a request - execute it
		requestCount++
		logf("📥 Request #%d: %s %s (id: %s)", requestCount, pollResp.Method, pollResp.URL, pollResp.ID)

		relayResp := executeRequest(&pollResp.PendingRequest)

		// Submit response back to relay
		if err := client.SubmitResponse(pollResp.ID, relayResp); err != nil {
			logf("❌ Failed to submit response: %v", err)
		} else {
			logf("📤 Response submitted for %s", pollResp.ID)
		}

		// Small delay between requests to avoid overwhelming
		time.Sleep(50 * time.Millisecond)
	}

	// Final stats
	logf("╔══════════════════════════════════════════════════════════════╗")
	logf("║                    Runner Stats                              ║")
	logf("╠══════════════════════════════════════════════════════════════╣")
	logf("║  Runner ID:       %-41s ║", runnerID)
	logf("║  Runtime:         %-41v ║", time.Since(startTime).Round(time.Second))
	logf("║  Requests:        %-41d ║", requestCount)
	logf("║  Errors:          %-41d ║", errorCount)
	logf("╚══════════════════════════════════════════════════════════════╝")
}

func printBanner() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║           GoVpn GitHub Actions Relay Runner                  ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Relay URL:   %-45s ║\n", relayURL)
	fmt.Printf("║  Runner ID:   %-45s ║\n", runnerID)
	fmt.Printf("║  Poll Delay:  %-45s ║\n", pollDelay)
	fmt.Printf("║  Req Timeout: %-45s ║\n", reqTimeout)
	fmt.Printf("║  Max Runtime: %-45s ║\n", maxRuntime)
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
}

func logf(format string, args ...interface{}) {
	prefix := time.Now().Format("15:04:05")
	fmt.Printf("[%s] %s\n", prefix, fmt.Sprintf(format, args...))
}

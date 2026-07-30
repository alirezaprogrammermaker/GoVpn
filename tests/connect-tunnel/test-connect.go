package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	connectProxy = "http://localhost:8081"
	cfWorkerURL  = "https://govpn-worker.social-panel.workers.dev"
)

type TestResult struct {
	Name       string
	StatusCode int
	Success    bool
	Duration   time.Duration
	Error      string
	BodyLen    int
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

func printHeader(title string) {
	fmt.Printf("\n╔══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║  %-58s  ║\n", title)
	fmt.Printf("╚══════════════════════════════════════════════════════════════╝\n")
}

func printResult(r TestResult) {
	status := "✅ PASS"
	if !r.Success {
		status = "❌ FAIL"
	}
	errMsg := ""
	if r.Error != "" {
		errMsg = fmt.Sprintf(" [%s]", truncate(r.Error, 35))
	}
	fmt.Printf("  %-45s %s (%dms, %dB)%s\n",
		r.Name, status, r.Duration.Milliseconds(), r.BodyLen, errMsg)
}

// Create HTTP client that uses our CONNECT proxy
func createProxyClient() *http.Client {
	proxyURL, _ := url.Parse(connectProxy)
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: false,
			},
		},
		Timeout: 30 * time.Second,
	}
}

// Test 1: Direct connection (no proxy)
func testDirect(target string) TestResult {
	start := time.Now()
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		return TestResult{Name: "Direct (no proxy)", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return TestResult{
		Name: fmt.Sprintf("Direct → %s", truncate(target, 35)),
		StatusCode: resp.StatusCode, Success: resp.StatusCode == 200,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 2: Through CONNECT proxy → HTTPS target
func testConnectProxy(target string) TestResult {
	start := time.Now()
	client := createProxyClient()
	resp, err := client.Get(target)
	if err != nil {
		return TestResult{Name: "CONNECT Proxy → HTTPS", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return TestResult{
		Name: fmt.Sprintf("CONNECT → %s", truncate(target, 35)),
		StatusCode: resp.StatusCode, Success: resp.StatusCode == 200,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 3: CONNECT proxy → CF Worker → target
func testConnectToCFWorker(target string) TestResult {
	start := time.Now()
	client := createProxyClient()

	cfProxyURL := fmt.Sprintf("%s/proxy?url=%s", cfWorkerURL, url.QueryEscape(target))
	resp, err := client.Get(cfProxyURL)
	if err != nil {
		return TestResult{Name: "CONNECT → CF Worker → Target", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return TestResult{
		Name: fmt.Sprintf("CONNECT→CF→%s", truncate(target, 30)),
		StatusCode: resp.StatusCode, Success: resp.StatusCode == 200,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 4: Full chain - CONNECT proxy → CF Worker → Apps Script
func testFullChain() TestResult {
	start := time.Now()
	client := createProxyClient()

	appsScriptURL := "https://script.google.com/macros/s/AKfycbzEvNqf9p_4EuY7KGdXN3BAJLUfMJp6jsR5SQHFJfhI6nCter-8EDHSs-OjCP4DHXJ3/exec?action=health"
	cfProxyURL := fmt.Sprintf("%s/proxy?url=%s", cfWorkerURL, url.QueryEscape(appsScriptURL))

	resp, err := client.Get(cfProxyURL)
	if err != nil {
		return TestResult{Name: "Full Chain: Proxy→CF→AppsScript", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Verify response
	success := resp.StatusCode == 200
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err == nil {
		if status, ok := data["status"]; ok {
			success = success && status == "ok"
		}
	}

	return TestResult{
		Name: "Full Chain: Proxy→CF→AppsScript",
		StatusCode: resp.StatusCode, Success: success,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 5: POST through CONNECT proxy
func testConnectPost(target string) TestResult {
	start := time.Now()
	client := createProxyClient()

	payload := `{"test": "CONNECT tunnel POST", "via": "proxy"}`
	resp, err := client.Post(target, "application/json", strings.NewReader(payload))
	if err != nil {
		return TestResult{Name: "CONNECT POST", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return TestResult{
		Name: fmt.Sprintf("CONNECT POST → %s", truncate(target, 30)),
		StatusCode: resp.StatusCode, Success: resp.StatusCode == 200,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 6: Multiple concurrent connections
func testConcurrent() TestResult {
	start := time.Now()
	client := createProxyClient()

	targets := []string{
		"https://govpn-worker.social-panel.workers.dev/health",
		"https://govpn-api.vercel.app/api/health",
	}

	results := make(chan bool, len(targets))
	for _, target := range targets {
		go func(t string) {
			resp, err := client.Get(t)
			if err != nil {
				results <- false
				return
			}
			defer resp.Body.Close()
			io.ReadAll(resp.Body)
			results <- resp.StatusCode == 200
		}(target)
	}

	pass := 0
	for i := 0; i < len(targets); i++ {
		if <-results {
			pass++
		}
	}

	return TestResult{
		Name: fmt.Sprintf("Concurrent: %d/%d targets", pass, len(targets)),
		Success: pass == len(targets),
		Duration: time.Since(start),
	}
}

func main() {
	printHeader("CONNECT Tunnel Proxy Test Suite")

	fmt.Println("\n📋 Tests:")
	fmt.Println("  1. Direct connection (baseline)")
	fmt.Println("  2. CONNECT proxy → HTTPS target")
	fmt.Println("  3. CONNECT proxy → CF Worker → target")
	fmt.Println("  4. Full Chain: Proxy → CF Worker → Apps Script")
	fmt.Println("  5. POST through CONNECT proxy")
	fmt.Println("  6. Concurrent connections")

	var results []TestResult

	// Test 1: Direct
	results = append(results, testDirect(cfWorkerURL+"/health"))

	// Test 2: Through CONNECT proxy
	results = append(results, testConnectProxy(cfWorkerURL+"/health"))

	// Test 3: CONNECT → CF Worker → httpbin
	results = append(results, testConnectToCFWorker("https://httpbin.org/get"))

	// Test 4: Full chain
	results = append(results, testFullChain())

	// Test 5: POST
	results = append(results, testConnectPost("https://httpbin.org/post"))

	// Test 6: Concurrent
	results = append(results, testConcurrent())

	// Print results
	printHeader("Results")
	pass, fail := 0, 0
	for _, r := range results {
		printResult(r)
		if r.Success {
			pass++
		} else {
			fail++
		}
	}

	fmt.Printf("\n╔══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║  Total: %d | Pass: %d | Fail: %d                              ║\n", pass+fail, pass, fail)
	fmt.Printf("╚══════════════════════════════════════════════════════════════╝\n")

	// Architecture
	if len(os.Args) > 1 && os.Args[1] == "--diagram" {
		fmt.Println(`
┌─────────────────────────────────────────────────────────────────┐
│              CONNECT Tunnel Architecture                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Client                Proxy                Target              │
│    │                    │                    │                   │
│    │──CONNECT host:443─►│                    │                   │
│    │◄──200 Connected────│                    │                   │
│    │                    │                    │                   │
│    │═══════════════════ TLS Tunnel ═══════════════════════════►│
│    │                    │                    │                   │
│    │──GET /path────────►│═══════════════════►│                   │
│    │◄──200 OK──────────│◄══════════════════│                   │
│    │                    │                    │                   │
│  Censor sees:        Proxy sees:          Target sees:          │
│  CONNECT tunnel      Encrypted bytes      Proxy IP              │
│  (can't inspect)     (can't decrypt)      (client hidden)       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘`)
	}
}

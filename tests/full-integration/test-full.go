package main

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	proxyURL    = "http://localhost:8082"
	cfWorkerURL = "https://govpn-worker.social-panel.workers.dev"
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
		errMsg = fmt.Sprintf(" [%s]", truncate(r.Error, 30))
	}
	fmt.Printf("  %-50s %s (%dms)%s\n", r.Name, status, r.Duration.Milliseconds(), errMsg)
}

// Create client with CONNECT proxy
func createProxyClient() *http.Client {
	proxyParsed, _ := url.Parse(proxyURL)
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyParsed),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: false,
			},
		},
		Timeout: 30 * time.Second,
	}
}

// ═══════════════════════════════════════════════════════════════
//  Test Cases
// ═══════════════════════════════════════════════════════════════

// Test 1: Proxy health check
func testHealth() TestResult {
	start := time.Now()
	resp, err := http.Get(proxyURL + "/health")
	if err != nil {
		return TestResult{Name: "Proxy Health Check", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data map[string]interface{}
	json.Unmarshal(body, &data)
	success := resp.StatusCode == 200
	if status, ok := data["status"]; ok {
		success = success && status == "ok"
	}

	return TestResult{
		Name: "Proxy Health Check", StatusCode: resp.StatusCode,
		Success: success, Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 2: HTTP forward (no proxy)
func testDirectHTTPS(target string) TestResult {
	start := time.Now()
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		return TestResult{Name: "Direct HTTPS", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return TestResult{
		Name: fmt.Sprintf("Direct → %s", truncate(target, 40)),
		StatusCode: resp.StatusCode, Success: resp.StatusCode == 200,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 3: CONNECT tunnel → HTTPS
func testConnectHTTPS(target string) TestResult {
	start := time.Now()
	client := createProxyClient()
	resp, err := client.Get(target)
	if err != nil {
		return TestResult{Name: "CONNECT Tunnel", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return TestResult{
		Name: fmt.Sprintf("CONNECT → %s", truncate(target, 40)),
		StatusCode: resp.StatusCode, Success: resp.StatusCode == 200,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 4: CF Worker bypass (through proxy)
func testCFBypass(target string) TestResult {
	start := time.Now()
	client := createProxyClient()
	cfURL := fmt.Sprintf("%s/cf?url=%s", proxyURL, url.QueryEscape(target))
	resp, err := client.Get(cfURL)
	if err != nil {
		return TestResult{Name: "CF Bypass", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return TestResult{
		Name: fmt.Sprintf("CF Bypass → %s", truncate(target, 40)),
		StatusCode: resp.StatusCode, Success: resp.StatusCode == 200,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 5: CF Bypass with base64 URL
func testCFBypassBase64(target string) TestResult {
	start := time.Now()
	client := createProxyClient()
	encoded := base64.StdEncoding.EncodeToString([]byte(target))
	cfURL := fmt.Sprintf("%s/cf?url=%s", proxyURL, encoded)
	resp, err := client.Get(cfURL)
	if err != nil {
		return TestResult{Name: "CF Bypass (base64)", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return TestResult{
		Name: fmt.Sprintf("CF Bypass (b64) → %s", truncate(target, 35)),
		StatusCode: resp.StatusCode, Success: resp.StatusCode == 200,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 6: Full chain - Proxy → CF Worker → Apps Script
func testFullChain() TestResult {
	start := time.Now()
	client := createProxyClient()

	appsScriptURL := "https://script.google.com/macros/s/AKfycbzEvNqf9p_4EuY7KGdXN3BAJLUfMJp6jsR5SQHFJfhI6nCter-8EDHSs-OjCP4DHXJ3/exec?action=health"
	cfURL := fmt.Sprintf("%s/cf?url=%s", proxyURL, url.QueryEscape(appsScriptURL))

	resp, err := client.Get(cfURL)
	if err != nil {
		return TestResult{Name: "Full Chain: Proxy→CF→AppsScript", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Verify response
	success := resp.StatusCode == 200
	var data map[string]interface{}
	if json.Unmarshal(body, &data) == nil {
		if status, ok := data["status"]; ok {
			success = success && status == "ok"
		}
	}

	return TestResult{
		Name: "Full Chain: Proxy→CF→AppsScript", StatusCode: resp.StatusCode,
		Success: success, Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 7: POST through CONNECT tunnel
func testConnectPOST(target string) TestResult {
	start := time.Now()
	client := createProxyClient()

	payload := `{"test": "full integration", "via": "connect-tunnel"}`
	resp, err := client.Post(target, "application/json", strings.NewReader(payload))
	if err != nil {
		return TestResult{Name: "CONNECT POST", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return TestResult{
		Name: fmt.Sprintf("POST → %s", truncate(target, 42)),
		StatusCode: resp.StatusCode, Success: resp.StatusCode == 200,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 8: Multiple targets via CONNECT
func testMultipleTargets() TestResult {
	start := time.Now()
	client := createProxyClient()

	targets := []string{
		"https://govpn-worker.social-panel.workers.dev/health",
		"https://govpn-api.vercel.app/api/health",
	}

	pass := 0
	for _, target := range targets {
		resp, err := client.Get(target)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				pass++
			}
		}
	}

	return TestResult{
		Name: fmt.Sprintf("Multi-target: %d/%d", pass, len(targets)),
		Success: pass == len(targets), Duration: time.Since(start),
	}
}

// Test 9: API info endpoint
func testAPIInfo() TestResult {
	start := time.Now()
	resp, err := http.Get(proxyURL + "/cf")
	if err != nil {
		return TestResult{Name: "API Info Endpoint", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var data map[string]interface{}
	success := resp.StatusCode == 200
	if json.Unmarshal(body, &data) == nil {
		if name, ok := data["name"]; ok {
			success = success && name == "GoVpn Full Proxy"
		}
	}

	return TestResult{
		Name: "API Info Endpoint", StatusCode: resp.StatusCode,
		Success: success, Duration: time.Since(start), BodyLen: len(body),
	}
}

// ═══════════════════════════════════════════════════════════════
//  Main
// ═══════════════════════════════════════════════════════════════

func main() {
	printHeader("GoVpn Full Integration Test Suite")

	fmt.Println("\n📋 Tests:")
	fmt.Println("  1.  Proxy health check")
	fmt.Println("  2.  Direct HTTPS (baseline)")
	fmt.Println("  3.  CONNECT tunnel → HTTPS")
	fmt.Println("  4.  CONNECT tunnel → CF Worker")
	fmt.Println("  5.  CF Bypass (base64 URL)")
	fmt.Println("  6.  Full Chain: Proxy→CF→Apps Script")
	fmt.Println("  7.  POST through CONNECT")
	fmt.Println("  8.  Multiple targets")
	fmt.Println("  9.  API info endpoint")

	var results []TestResult

	// Run all tests
	results = append(results, testHealth())
	results = append(results, testDirectHTTPS(cfWorkerURL+"/health"))
	results = append(results, testConnectHTTPS(cfWorkerURL+"/health"))
	results = append(results, testCFBypass("https://httpbin.org/get"))
	results = append(results, testCFBypassBase64("https://httpbin.org/get"))
	results = append(results, testFullChain())
	results = append(results, testConnectPOST("https://httpbin.org/post"))
	results = append(results, testMultipleTargets())
	results = append(results, testAPIInfo())

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

	// Architecture diagram
	fmt.Println(`
╔═══════════════════════════════════════════════════════════════════╗
║                    GoVpn Full Proxy Architecture                  ║
╠═══════════════════════════════════════════════════════════════════╣
║                                                                   ║
║   ┌─────────┐                                                    ║
║   │ Client  │                                                    ║
║   └────┬────┘                                                    ║
║        │                                                         ║
║        ├─── HTTP ────────────────────────► Target (direct)       ║
║        │                                                         ║
║        ├─── CONNECT ─────────────────────► Target (HTTPS)        ║
║        │    (TLS tunnel)                                         ║
║        │                                                         ║
║        ├─── /cf?url=... ──► CF Worker ──► Target (bypass)        ║
║        │                    (Cloudflare)                         ║
║        │                                                         ║
║        └─── /cf?url=b64... ► CF Worker ─► Target (encoded)       ║
║                         (base64 URL)                             ║
║                                                                   ║
║   Features:                                                       ║
║     ✅ HTTP Forward Proxy                                        ║
║     ✅ HTTPS CONNECT Tunneling                                   ║
║     ✅ CF Worker Anti-Censorship                                 ║
║     ✅ Base64 URL Encoding                                       ║
║     ✅ Concurrent Connections                                    ║
║                                                                   ║
╚═══════════════════════════════════════════════════════════════════╝`)
}

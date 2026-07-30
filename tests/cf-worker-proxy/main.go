package main

import (
	"encoding/base64"
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
	workerURL    = "https://govpn-worker.social-panel.workers.dev"
	localProxy   = "http://localhost:8080"
	testTarget   = "https://httpbin.org/get"
	localTarget  = "http://localhost:9090/test"
)

type TestResult struct {
	Name       string
	Method     string
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
	fmt.Printf("  %-8s %-40s %s (%dms, %dB)%s\n",
		r.Method, r.Name, status, r.Duration.Milliseconds(), r.BodyLen, errMsg)
}

// Test 1: Direct request to CF Worker health endpoint
func testWorkerHealth() TestResult {
	start := time.Now()
	resp, err := http.Get(workerURL + "/health")
	if err != nil {
		return TestResult{Name: "CF Worker Health", Method: "GET", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return TestResult{
		Name: "CF Worker Health", Method: "GET",
		StatusCode: resp.StatusCode, Success: resp.StatusCode == 200,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 2: CF Worker proxy → target (direct, no local proxy)
func testWorkerProxyDirect(target string) TestResult {
	start := time.Now()
	proxyURL := fmt.Sprintf("%s/proxy?url=%s", workerURL, url.QueryEscape(target))
	resp, err := http.Get(proxyURL)
	if err != nil {
		return TestResult{Name: "CF→Target (direct)", Method: "GET", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return TestResult{
		Name: fmt.Sprintf("CF→%s", truncate(target, 30)), Method: "GET",
		StatusCode: resp.StatusCode, Success: resp.StatusCode == 200,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 3: CF Worker proxy with base64 encoded URL
func testWorkerProxyBase64(target string) TestResult {
	start := time.Now()
	encoded := base64.StdEncoding.EncodeToString([]byte(target))
	proxyURL := fmt.Sprintf("%s/proxy?url=%s", workerURL, encoded)
	resp, err := http.Get(proxyURL)
	if err != nil {
		return TestResult{Name: "CF→Target (base64)", Method: "GET", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return TestResult{
		Name: fmt.Sprintf("CF→%s (b64)", truncate(target, 25)), Method: "GET",
		StatusCode: resp.StatusCode, Success: resp.StatusCode == 200,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 4: Local proxy → CF Worker → target
// NOTE: Current proxy only supports HTTP (no CONNECT for HTTPS tunneling)
// This test documents the limitation
func testLocalProxyToCF(target string) TestResult {
	start := time.Now()
	cfProxyURL := fmt.Sprintf("%s/proxy?url=%s", workerURL, url.QueryEscape(target))

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(localProxy)),
		},
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(cfProxyURL)
	if err != nil {
		// Expected: proxy doesn't support HTTPS CONNECT
		return TestResult{
			Name: "Local→CF→Target (HTTPS)", Method: "GET",
			Error: "Proxy lacks CONNECT support for HTTPS: " + truncate(err.Error(), 40),
			Duration: time.Since(start),
		}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return TestResult{
		Name: fmt.Sprintf("Local→CF→%s", truncate(target, 25)), Method: "GET",
		StatusCode: resp.StatusCode, Success: resp.StatusCode == 200,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 4b: Local proxy → HTTP target (demonstrates proxy works for HTTP)
func testLocalProxyHTTP(target string) TestResult {
	start := time.Now()
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(localProxy)),
		},
		Timeout: 30 * time.Second,
	}
	resp, err := client.Get(target)
	if err != nil {
		return TestResult{Name: "Local→Target (HTTP)", Method: "GET", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return TestResult{
		Name: fmt.Sprintf("Local→%s (HTTP)", truncate(target, 28)), Method: "GET",
		StatusCode: resp.StatusCode, Success: resp.StatusCode == 200,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 5: POST through CF Worker proxy
func testWorkerProxyPost(target string) TestResult {
	start := time.Now()
	proxyURL := fmt.Sprintf("%s/proxy?url=%s", workerURL, url.QueryEscape(target))

	payload := `{"test": "hello from GoVpn", "via": "cf-worker-proxy"}`
	resp, err := http.Post(proxyURL, "application/json", strings.NewReader(payload))
	if err != nil {
		return TestResult{Name: "CF→Target POST", Method: "POST", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return TestResult{
		Name: fmt.Sprintf("CF→%s POST", truncate(target, 25)), Method: "POST",
		StatusCode: resp.StatusCode, Success: resp.StatusCode == 200,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

// Test 6: Chain test - multiple hops
func testChain(target string) TestResult {
	start := time.Now()

	// Step 1: Request to CF Worker
	proxyURL := fmt.Sprintf("%s/proxy?url=%s", workerURL, url.QueryEscape(target))

	// Step 2: Through local proxy (if available)
	client := &http.Client{Timeout: 30 * time.Second}

	resp, err := client.Get(proxyURL)
	if err != nil {
		return TestResult{Name: "Chain Test", Method: "GET", Error: err.Error(), Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Verify response contains expected data
	success := resp.StatusCode == 200
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err == nil {
		if msg, ok := data["message"]; ok {
			success = success && strings.Contains(msg.(string), "GoVpn")
		}
	}

	return TestResult{
		Name: "Chain: CF Worker → Apps Script", Method: "GET",
		StatusCode: resp.StatusCode, Success: success,
		Duration: time.Since(start), BodyLen: len(body),
	}
}

func mustParseURL(rawURL string) *url.URL {
	u, _ := url.Parse(rawURL)
	return u
}

func main() {
	printHeader("Cloudflare Worker Proxy Test Suite")

	fmt.Println("\n📋 Tests:")
	fmt.Println("  1. CF Worker health check")
	fmt.Println("  2. CF Worker → target (direct)")
	fmt.Println("  3. CF Worker → target (base64 URL)")
	fmt.Println("  4. Local proxy → CF Worker → target (HTTPS)")
	fmt.Println("  5. Local proxy → target (HTTP only)")
	fmt.Println("  6. CF Worker → target (POST)")
	fmt.Println("  7. Chain: CF Worker → Apps Script")

	var results []TestResult

	// Test 1: Worker health
	results = append(results, testWorkerHealth())

	// Test 2: Direct proxy
	results = append(results, testWorkerProxyDirect(testTarget))

	// Test 3: Base64 proxy
	results = append(results, testWorkerProxyBase64(testTarget))

	// Test 4 & 5: Local proxy tests (only if --with-proxy)
	if len(os.Args) > 1 && os.Args[1] == "--with-proxy" {
		fmt.Println("\n⚠️  Testing with local proxy (make sure proxy is running on :8080)")
		results = append(results, testLocalProxyToCF(testTarget))
		results = append(results, testLocalProxyHTTP("http://localhost:9090/test"))
	} else {
		fmt.Println("\n⏭️  Skipping local proxy tests (use --with-proxy to enable)")
	}

	// Test 6: POST through CF Worker
	results = append(results, testWorkerProxyPost(testTarget))

	// Test 7: Chain test - CF Worker → Apps Script
	appsScriptURL := "https://script.google.com/macros/s/AKfycbzEvNqf9p_4EuY7KGdXN3BAJLUfMJp6jsR5SQHFJfhI6nCter-8EDHSs-OjCP4DHXJ3/exec?action=health"
	results = append(results, testChain(appsScriptURL))

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

	// Print architecture diagram
	fmt.Println(`
┌─────────────────────────────────────────────────────────────────┐
│                    Traffic Flow                                  │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Censor sees:     Client ──────► Cloudflare (legitimate)        │
│                                                                  │
│  Actually:         Client ──► CF Worker ──► Target (hidden)     │
│                                                                  │
│  With local proxy: Client ──► Proxy ──► CF Worker ──► Target    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘`)
}

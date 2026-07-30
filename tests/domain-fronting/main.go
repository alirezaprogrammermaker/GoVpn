package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	targetHost = "script.google.com"
	targetPath = "/macros/s/AKfycbzEvNqf9p_4EuY7KGdXN3BAJLUfMJp6jsR5SQHFJfhI6nCter-8EDHSs-OjCP4DHXJ3/exec"
)

var frontDomains = []string{
	"www.google.com",
	"googleapis.com",
	"www.googleapis.com",
	"fonts.googleapis.com",
	"storage.googleapis.com",
}

type TestResult struct {
	Name       string
	Front      string
	SNI        string
	HostHeader string
	StatusCode int
	Success    bool
	Error      string
}

func resolveIP(host string) string {
	ips, err := net.LookupHost(host)
	if err != nil {
		return ""
	}
	if len(ips) > 0 {
		return ips[0]
	}
	return ""
}

func testDirect() TestResult {
	fmt.Println("\n📡 TEST 1: Direct Connection (Baseline)")
	fmt.Println("  Connecting to:", targetHost)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("https://" + targetHost + targetPath)
	if err != nil {
		return TestResult{Name: "Direct", Success: false, Error: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("  Status: %d\n", resp.StatusCode)
	fmt.Printf("  Body: %s\n", truncate(string(body), 100))
	return TestResult{Name: "Direct", StatusCode: resp.StatusCode, Success: resp.StatusCode == 200}
}

func testDomainFronting(frontDomain string) TestResult {
	fmt.Printf("\n📡 TEST 2: Domain Fronting\n")
	fmt.Printf("  Front Domain (SNI): %s\n", frontDomain)
	fmt.Printf("  Host Header: %s\n", targetHost)

	targetIP := resolveIP(targetHost)
	if targetIP == "" {
		return TestResult{Name: "Fronting", Front: frontDomain, Error: "Cannot resolve target IP"}
	}
	fmt.Printf("  Target IP: %s\n", targetIP)

	// Create custom TLS config with front domain as SNI
	tlsConfig := &tls.Config{
		ServerName:         frontDomain,
		InsecureSkipVerify: false,
	}

	// Custom dialer to connect to target IP
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", targetIP+":443", tlsConfig)
	if err != nil {
		return TestResult{
			Name: "Fronting", Front: frontDomain, SNI: frontDomain,
			HostHeader: targetHost, Error: "TLS dial failed: " + err.Error(),
		}
	}
	defer conn.Close()

	fmt.Printf("  TLS Connected! SNI sent: %s\n", frontDomain)
	fmt.Printf("  TLS State: ServerName=%s\n", conn.ConnectionState().ServerName)

	// Build HTTP request with different Host header
	reqLine := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", targetPath, targetHost)
	_, err = conn.Write([]byte(reqLine))
	if err != nil {
		return TestResult{
			Name: "Fronting", Front: frontDomain, SNI: frontDomain,
			HostHeader: targetHost, Error: "Write failed: " + err.Error(),
		}
	}

	// Read response
	buf := make([]byte, 4096)
	n, _ := conn.Read(buf)
	response := string(buf[:n])

	// Parse status code
	statusCode := 0
	if strings.HasPrefix(response, "HTTP/") {
		parts := strings.SplitN(response, " ", 3)
		if len(parts) >= 2 {
			fmt.Sscanf(parts[1], "%d", &statusCode)
		}
	}

	fmt.Printf("  Status Code: %d\n", statusCode)
	if statusCode == 200 {
		fmt.Println("  ✅ DOMAIN FRONTING WORKS!")
	} else if statusCode == 421 {
		fmt.Println("  ⚠️  421 Misdirected Request - Fronting BLOCKED")
	} else {
		fmt.Printf("  ❌ Failed (status: %d)\n", statusCode)
	}

	// Print response snippet
	if idx := strings.Index(response, "\r\n\r\n"); idx >= 0 {
		body := response[idx+4:]
		fmt.Printf("  Body: %s\n", truncate(body, 150))
	}

	return TestResult{
		Name: "Fronting", Front: frontDomain, SNI: frontDomain,
		HostHeader: targetHost, StatusCode: statusCode,
		Success: statusCode == 200,
	}
}

func testSNIOverride(frontDomain string) TestResult {
	fmt.Printf("\n📡 TEST 3: SNI Override (HTTPS Client)\n")
	fmt.Printf("  URL Host: %s\n", frontDomain)
	fmt.Printf("  SNI: %s\n", frontDomain)
	fmt.Printf("  Host Header: %s\n", targetHost)

	targetIP := resolveIP(targetHost)
	if targetIP == "" {
		return TestResult{Name: "SNI", Front: frontDomain, Error: "Cannot resolve target IP"}
	}

	// Create transport with custom TLS
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			ServerName: frontDomain,
		},
		DialTLS: func(network, addr string) (net.Conn, error) {
			// Always connect to target IP but use front domain for SNI
			return tls.Dial("tcp", targetIP+":443", &tls.Config{
				ServerName: frontDomain,
			})
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects
		},
	}

	req, _ := http.NewRequest("GET", "https://"+frontDomain+targetPath, nil)
	req.Host = targetHost // Set Host header to target

	resp, err := client.Do(req)
	if err != nil {
		return TestResult{
			Name: "SNI", Front: frontDomain, SNI: frontDomain,
			HostHeader: targetHost, Error: err.Error(),
		}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("  Status: %d\n", resp.StatusCode)
	if resp.StatusCode == 200 {
		fmt.Println("  ✅ SNI REWRITE WORKS!")
	} else {
		fmt.Printf("  ❌ Failed\n")
	}
	fmt.Printf("  Body: %s\n", truncate(string(body), 150))

	return TestResult{
		Name: "SNI", Front: frontDomain, SNI: frontDomain,
		HostHeader: targetHost, StatusCode: resp.StatusCode,
		Success: resp.StatusCode == 200,
	}
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

func printSummary(results []TestResult) {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    TEST SUMMARY                             ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	for _, r := range results {
		status := "❌ FAIL"
		if r.Success {
			status = "✅ PASS"
		}
		front := r.Front
		if front == "" {
			front = "direct"
		}
		fmt.Printf("║  %-12s | Front: %-25s | %s ║\n", r.Name, front, status)
	}
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
}

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║       Domain Fronting / SNI Rewriting Test                  ║")
	fmt.Println("║       Target: Google Apps Script                            ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	if len(os.Args) > 1 && os.Args[1] == "--quick" {
		fmt.Println("\n[Quick mode: testing first front domain only]")
		frontDomains = frontDomains[:1]
	}

	var results []TestResult

	// Test 1: Direct
	results = append(results, testDirect())

	// Test 2 & 3: Domain fronting with each front domain
	for _, front := range frontDomains {
		results = append(results, testDomainFronting(front))
		results = append(results, testSNIOverride(front))
	}

	printSummary(results)
}

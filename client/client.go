package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

func main() {
	proxyURL, err := url.Parse("http://localhost:8080")
	if err != nil {
		fmt.Printf("❌ Failed to parse proxy URL: %v\n", err)
		os.Exit(1)
	}

	// Create HTTP client with proxy
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 30 * time.Second,
	}

	targetURL := "http://localhost:9090/test"
	fmt.Printf("📡 Sending request to %s via proxy localhost:8080...\n\n", targetURL)

	// Send GET request through the proxy
	resp, err := client.Get(targetURL)
	if err != nil {
		fmt.Printf("❌ Request failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// Read and display response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("❌ Failed to read response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Response received!\n")
	fmt.Printf("   Status: %s\n", resp.Status)
	fmt.Printf("   Content-Length: %d bytes\n\n", len(body))
	fmt.Println("--- Response Body ---")
	fmt.Println(string(body))
}

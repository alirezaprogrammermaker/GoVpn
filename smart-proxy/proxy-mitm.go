package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	listenPort = ":8085"
	cfWorker   = "https://govpn-worker.social-panel.workers.dev"
)

var (
	caCert    *x509.Certificate
	caKey     *ecdsa.PrivateKey
	certCache = make(map[string]*tls.Certificate)
	cacheMu   sync.RWMutex
)

// ═══════════════════════════════════════════════════════════════
//  CA Certificate Loading
// ═══════════════════════════════════════════════════════════════

func loadCA() error {
	// Load CA certificate
	certPEM, err := os.ReadFile("certs/ca.pem")
	if err != nil {
		return fmt.Errorf("failed to read ca.pem: %w", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("failed to decode ca.pem")
	}
	caCert, err = x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse ca.pem: %w", err)
	}

	// Load CA private key
	keyPEM, err := os.ReadFile("certs/ca-key.pem")
	if err != nil {
		return fmt.Errorf("failed to read ca-key.pem: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return fmt.Errorf("failed to decode ca-key.pem")
	}
	caKey, err = x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse ca-key.pem: %w", err)
	}

	log.Println("✅ CA certificate loaded successfully")
	return nil
}

// ═══════════════════════════════════════════════════════════════
//  Certificate Generation (on-the-fly)
// ═══════════════════════════════════════════════════════════════

func generateCert(host string) (*tls.Certificate, error) {
	cacheMu.RLock()
	if cert, ok := certCache[host]; ok {
		cacheMu.RUnlock()
		return cert, nil
	}
	cacheMu.RUnlock()

	// Generate new key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	// Create certificate template
	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"GoVpn Proxy"},
			CommonName:   host,
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
	}

	// Add SAN
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}

	// Sign with CA
	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, err
	}

	cert := &tls.Certificate{
		Certificate: [][]byte{certDER, caCert.Raw},
		PrivateKey:  key,
	}

	// Cache it
	cacheMu.Lock()
	certCache[host] = cert
	cacheMu.Unlock()

	return cert, nil
}

// ═══════════════════════════════════════════════════════════════
//  MITM CONNECT Handler
// ═══════════════════════════════════════════════════════════════

func handleConnect(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if !strings.Contains(host, ":") {
		host = host + ":443"
	}
	hostname := strings.Split(host, ":")[0]

	log.Printf("[CONNECT] 🔗 %s → %s", r.RemoteAddr, host)

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

	// Send 200 Connection Established
	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		return
	}

	// Generate certificate for this host
	cert, err := generateCert(hostname)
	if err != nil {
		log.Printf("[CONNECT] ❌ Cert generation failed: %v", err)
		return
	}

	// TLS handshake with client (using fake cert)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{*cert},
	}
	tlsConn := tls.Server(clientConn, tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		log.Printf("[CONNECT] ❌ TLS handshake failed: %v", err)
		return
	}
	defer tlsConn.Close()

	log.Printf("[CONNECT] ✅ MITM TLS established: %s", host)

	// Read HTTP request from client (through TLS)
	reader := bufio.NewReader(tlsConn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		log.Printf("[CONNECT] ❌ Failed to read request: %v", err)
		return
	}

	// Build full URL
	scheme := "https"
	fullURL := scheme + "://" + host + req.URL.String()

	log.Printf("[CONNECT] 📡 %s %s", req.Method, fullURL)

	// Forward through CF Worker
	cfURL := fmt.Sprintf("%s/proxy?url=%s", cfWorker, url.QueryEscape(fullURL))

	// Create outgoing request
	outReq, err := http.NewRequest(req.Method, cfURL, req.Body)
	if err != nil {
		log.Printf("[CONNECT] ❌ Failed to create request: %v", err)
		return
	}

	// Copy headers
	for key, values := range req.Header {
		for _, value := range values {
			outReq.Header.Add(key, value)
		}
	}

	// Send through CF Worker
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(outReq)
	if err != nil {
		log.Printf("[CONNECT] ❌ CF Worker failed: %v", err)
		errResp := "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"
		tlsConn.Write([]byte(errResp))
		return
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[CONNECT] ❌ Failed to read response: %v", err)
		return
	}

	// Write response back to client
	responseLine := fmt.Sprintf("HTTP/1.1 %d %s\r\n", resp.StatusCode, http.StatusText(resp.StatusCode))
	tlsConn.Write([]byte(responseLine))

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			headerLine := fmt.Sprintf("%s: %s\r\n", key, value)
			tlsConn.Write([]byte(headerLine))
		}
	}
	tlsConn.Write([]byte("\r\n"))
	tlsConn.Write(body)

	log.Printf("[CONNECT] ✅ Response: %d (%d bytes)", resp.StatusCode, len(body))
}

// ═══════════════════════════════════════════════════════════════
//  HTTP Handler (through CF Worker)
// ═══════════════════════════════════════════════════════════════

func handleHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("[HTTP] %s %s", r.Method, r.URL.String())

	// Forward through CF Worker
	cfURL := fmt.Sprintf("%s/proxy?url=%s", cfWorker, url.QueryEscape(r.URL.String()))

	outReq, err := http.NewRequest(r.Method, cfURL, r.Body)
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
//  Web Interface
// ═══════════════════════════════════════════════════════════════

const webInterface = `<!DOCTYPE html>
<html dir="rtl" lang="fa">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>GoVpn Smart Proxy</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: 'Segoe UI', Tahoma, sans-serif;
            background: linear-gradient(135deg, #0f0c29, #302b63, #24243e);
            min-height: 100vh;
            display: flex;
            flex-direction: column;
            align-items: center;
            color: #fff;
        }
        .container { max-width: 800px; width: 90%; margin-top: 50px; }
        h1 {
            text-align: center; margin-bottom: 30px; font-size: 2.5em;
            background: linear-gradient(90deg, #00d2ff, #3a7bd5);
            -webkit-background-clip: text; -webkit-text-fill-color: transparent;
        }
        .status-box {
            text-align: center; padding: 15px; margin-bottom: 20px;
            background: rgba(40,167,69,0.2); border-radius: 10px;
            border: 1px solid #28a745;
        }
        .info {
            margin-top: 20px; padding: 15px;
            background: rgba(255,255,255,0.05); border-radius: 10px;
            font-size: 14px; line-height: 1.8;
        }
        code {
            background: rgba(255,255,255,0.1);
            padding: 2px 6px; border-radius: 4px;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>🚀 GoVpn Smart Proxy</h1>
        <div class="status-box">
            ✅ پروکسی فعال و آماده استفاده<br>
            <small>پورت پروکسی: localhost:8085</small>
        </div>
        <div class="info">
            <strong>📋 نحوه استفاده:</strong><br><br>
            <strong>روش ۱: تنظیم در ویندوز</strong><br>
            Settings → Network → Proxy → Manual<br>
            Address: <code>localhost</code> | Port: <code>8085</code><br><br>
            <strong>روش ۲: تنظیم در Firefox</strong><br>
            Settings → Network → Manual Proxy<br>
            HTTP Proxy: <code>localhost</code> | Port: <code>8085</code><br>
            ✅ Also use for HTTPS<br><br>
            <strong>⚠️ مهم:</strong> باید گواهی <code>certs/ca.pem</code> رو در ویندوز نصب کنید<br>
            (دوبار کلیک کنید → Install Certificate → Local Machine → Trusted Root)
        </div>
    </div>
</body>
</html>`

// ═══════════════════════════════════════════════════════════════
//  Main
// ═══════════════════════════════════════════════════════════════

func main() {
	// Load CA certificate
	if err := loadCA(); err != nil {
		log.Fatalf("❌ Failed to load CA: %v", err)
	}

	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║           GoVpn MITM Proxy v3.0                             ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Features:                                                  ║")
	fmt.Println("║    ✅ MITM HTTPS (routes through CF Worker)                 ║")
	fmt.Println("║    ✅ HTTP Forward (through CF Worker)                      ║")
	fmt.Println("║    ✅ Anti-Censorship                                       ║")
	fmt.Printf("║  📡 Proxy: localhost%s                              ║\n", listenPort)
	fmt.Println("║  🌐 Web UI: http://localhost:8085                           ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodConnect:
			handleConnect(w, r)
		case r.URL.Path == "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, webInterface)
		case r.URL.Path == "/health":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			handleHTTP(w, r)
		}
	})

	if err := http.ListenAndServe(listenPort, handler); err != nil {
		log.Fatalf("❌ Server failed: %v", err)
	}
}

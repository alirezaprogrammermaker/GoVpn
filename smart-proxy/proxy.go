package main

import (
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
	listenPort = ":8085"
	cfWorker   = "https://govpn-worker.social-panel.workers.dev"
)

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
        .container {
            max-width: 800px;
            width: 90%;
            margin-top: 50px;
        }
        h1 {
            text-align: center;
            margin-bottom: 30px;
            font-size: 2.5em;
            background: linear-gradient(90deg, #00d2ff, #3a7bd5);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }
        .search-box {
            display: flex;
            gap: 10px;
            margin-bottom: 20px;
        }
        input[type="text"] {
            flex: 1;
            padding: 15px 20px;
            font-size: 16px;
            border: 2px solid #3a7bd5;
            border-radius: 10px;
            background: rgba(255,255,255,0.1);
            color: #fff;
            outline: none;
        }
        input[type="text"]::placeholder { color: rgba(255,255,255,0.5); }
        input[type="text"]:focus { border-color: #00d2ff; }
        button {
            padding: 15px 30px;
            font-size: 16px;
            background: linear-gradient(90deg, #00d2ff, #3a7bd5);
            color: #fff;
            border: none;
            border-radius: 10px;
            cursor: pointer;
            font-weight: bold;
        }
        button:hover { opacity: 0.9; }
        .status {
            text-align: center;
            padding: 10px;
            margin-bottom: 20px;
            border-radius: 8px;
            display: none;
        }
        .status.loading { display: block; background: rgba(255,193,7,0.2); color: #ffc107; }
        .status.success { display: block; background: rgba(40,167,69,0.2); color: #28a745; }
        .status.error { display: block; background: rgba(220,53,69,0.2); color: #dc3545; }
        .quick-links {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
            gap: 10px;
            margin-bottom: 30px;
        }
        .quick-links a {
            padding: 15px;
            background: rgba(255,255,255,0.05);
            border: 1px solid rgba(255,255,255,0.1);
            border-radius: 10px;
            color: #fff;
            text-decoration: none;
            text-align: center;
            transition: all 0.3s;
        }
        .quick-links a:hover {
            background: rgba(58,123,213,0.3);
            border-color: #3a7bd5;
        }
        .frame-container {
            width: 100%;
            height: 70vh;
            border: 2px solid #3a7bd5;
            border-radius: 10px;
            overflow: hidden;
            display: none;
        }
        .frame-container iframe {
            width: 100%;
            height: 100%;
            border: none;
        }
        .info {
            margin-top: 20px;
            padding: 15px;
            background: rgba(255,255,255,0.05);
            border-radius: 10px;
            font-size: 14px;
            line-height: 1.8;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>🚀 GoVpn Smart Proxy</h1>
        
        <div class="search-box">
            <input type="text" id="urlInput" placeholder="آدرس سایت رو وارد کنید... (مثال: https://youtube.com)">
            <button onclick="browse()">برو →</button>
        </div>

        <div id="status" class="status"></div>

        <div class="quick-links">
            <a href="#" onclick="quickLink('https://youtube.com')">📺 YouTube</a>
            <a href="#" onclick="quickLink('https://twitter.com')">🐦 Twitter</a>
            <a href="#" onclick="quickLink('https://telegram.org')">✈️ Telegram</a>
            <a href="#" onclick="quickLink('https://github.com')">💻 GitHub</a>
            <a href="#" onclick="quickLink('https://reddit.com')">🔴 Reddit</a>
            <a href="#" onclick="quickLink('https://medium.com')">📝 Medium</a>
        </div>

        <div class="frame-container" id="frameContainer">
            <iframe id="proxyFrame" sandbox="allow-same-origin allow-scripts allow-forms allow-popups"></iframe>
        </div>

        <div class="info">
            <strong>💡 نحوه استفاده:</strong><br>
            • آدرس سایت رو وارد کنید و Enter بزنید<br>
            • روی لینک‌های سریع کلیک کنید<br>
            • ترافیک از طریق Cloudflare عبور می‌کنه (غیرقابل فیلتر)<br>
            • <strong>پورت پروکسی:</strong> <code>localhost:8085</code>
        </div>
    </div>

    <script>
        function browse() {
            const url = document.getElementById('urlInput').value.trim();
            if (!url) return;
            
            let targetUrl = url;
            if (!targetUrl.startsWith('http://') && !targetUrl.startsWith('https://')) {
                targetUrl = 'https://' + targetUrl;
            }

            const status = document.getElementById('status');
            const frame = document.getElementById('frameContainer');
            
            status.className = 'status loading';
            status.textContent = '⏳ در حال بارگذاری...';
            
            // Use proxy endpoint
            const proxyUrl = '/browse?url=' + encodeURIComponent(targetUrl);
            
            document.getElementById('proxyFrame').src = proxyUrl;
            frame.style.display = 'block';
            
            document.getElementById('proxyFrame').onload = function() {
                status.className = 'status success';
                status.textContent = '✅ صفحه بارگذاری شد';
                setTimeout(() => { status.style.display = 'none'; }, 3000);
            };
            
            document.getElementById('proxyFrame').onerror = function() {
                status.className = 'status error';
                status.textContent = '❌ خطا در بارگذاری';
            };
        }

        function quickLink(url) {
            document.getElementById('urlInput').value = url;
            browse();
        }

        document.getElementById('urlInput').addEventListener('keypress', function(e) {
            if (e.key === 'Enter') browse();
        });
    </script>
</body>
</html>`

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
//  Browse Handler (Web Interface Proxy)
// ═══════════════════════════════════════════════════════════════

func handleBrowse(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Error(w, "Missing url parameter", http.StatusBadRequest)
		return
	}

	log.Printf("[BROWSE] 🌐 %s → %s", r.RemoteAddr, targetURL)

	// Forward through CF Worker
	cfURL := fmt.Sprintf("%s/proxy?url=%s", cfWorker, url.QueryEscape(targetURL))
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(cfURL)
	if err != nil {
		http.Error(w, "Failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Read failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Set response headers
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/html"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)

	log.Printf("[BROWSE] ✅ %d (%d bytes)", resp.StatusCode, len(body))
}

// ═══════════════════════════════════════════════════════════════
//  API Handler
// ═══════════════════════════════════════════════════════════════

func handleAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":    "GoVpn Smart Proxy",
		"version": "2.0.0",
		"port":    listenPort,
		"endpoints": map[string]string{
			"/":           "Web interface",
			"/health":     "Health check",
			"/browse?url=":"Browse through CF Worker",
			"/api":        "This info",
		},
		"features": []string{
			"Web Interface",
			"HTTP Forward Proxy",
			"HTTPS CONNECT Tunneling",
			"CF Worker Anti-Censorship",
		},
	})
}

// ═══════════════════════════════════════════════════════════════
//  Health Handler
// ═══════════════════════════════════════════════════════════════

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"message":   "GoVpn Smart Proxy is alive! 🚀",
		"timestamp": time.Now().Format(time.RFC3339),
	})
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
//  Main
// ═══════════════════════════════════════════════════════════════

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║              GoVpn Smart Proxy v2.0                         ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Features:                                                  ║")
	fmt.Println("║    ✅ Web Interface (browser)                               ║")
	fmt.Println("║    ✅ HTTP Forward Proxy                                    ║")
	fmt.Println("║    ✅ HTTPS CONNECT Tunneling                               ║")
	fmt.Println("║    ✅ CF Worker Anti-Censorship                             ║")
	fmt.Printf("║  🌐 Open http://localhost%s in browser              ║\n", listenPort)
	fmt.Printf("║  📡 Proxy address: localhost%s                      ║\n", listenPort)
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodConnect:
			handleConnect(w, r)
		case r.URL.Path == "/health":
			handleHealth(w, r)
		case r.URL.Path == "/api":
			handleAPI(w, r)
		case r.URL.Path == "/browse":
			handleBrowse(w, r)
		case r.URL.Path == "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, webInterface)
		default:
			handleHTTP(w, r)
		}
	})

	if err := http.ListenAndServe(listenPort, handler); err != nil {
		log.Fatalf("❌ Server failed: %v", err)
	}
}

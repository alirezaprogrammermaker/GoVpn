# GoVpn GitHub Actions Relay - Test Report

## Architecture

```
┌─────────────┐     ┌──────────────────┐     ┌─────────────────────┐     ┌──────────────┐
│  VPN Client  │────▶│  Local Proxy     │────▶│  CF Worker (Queue)  │────▶│  GH Actions  │
│  (GoVpn)     │◀────│  :8083           │◀────│  Relay Endpoint     │◀────│  Runner (Go) │
└─────────────┘     └──────────────────┘     └─────────────────────┘     └──────┬───────┘
                                                                                 │
                                                                                 ▼
                                                                          ┌──────────────┐
                                                                          │  Target Site │
                                                                          └──────────────┘
```

## How It Works

### The Problem
GitHub Actions runners **cannot accept incoming connections** - they're CI/CD jobs, not servers. But they **can make outbound HTTP requests**.

### The Solution
Use a Cloudflare Worker as a **bridge/queue** between clients and runners:

1. **Client** submits a request to CF Worker `/enqueue`
2. CF Worker stores the request in a **Durable Object** queue
3. **GitHub Actions Runner** polls CF Worker `/poll` every 500ms
4. Runner picks up the request, executes it against the target
5. Runner posts the response back to CF Worker `/response/:id`
6. **Client** polls CF Worker `/result/:id` for the response

### Why GitHub Actions?

| Benefit | Detail |
|---------|--------|
| **Free compute** | Public repos get 2000 minutes/month |
| **Hard to block** | Runners use Azure IPs (Microsoft's infrastructure) |
| **6-hour runtime** | Enough for a relay session |
| **No server needed** | Just push to a public repo and trigger the workflow |
| **Multiple runners** | Can run multiple instances for higher throughput |

## Components

### 1. CF Worker Relay (`worker-relay/`)
- Uses **Durable Objects** for persistent request queue
- Endpoints: `/enqueue`, `/poll`, `/response/:id`, `/result/:id`, `/health`, `/stats`
- Handles CORS for browser clients

### 2. GitHub Actions Runner (`runner/main.go`)
- Go program that polls the relay
- Configurable: poll delay, request timeout, max runtime
- Identifies itself with a unique runner ID
- Graceful shutdown after max runtime

### 3. GitHub Actions Workflow (`.github/workflows/relay.yml`)
- Manual trigger (`workflow_dispatch`)
- Optional schedule (every 6 hours)
- Configurable relay URL and runtime parameters
- Auto-cleanup after run

### 4. Local Proxy (`proxy-relay/proxy-relay.go`)
- Accepts HTTP requests on `:8083`
- Routes through CF Worker relay
- Polls for response with 60s timeout
- Also supports direct proxy (no relay)

## Deployment Steps

### Step 1: Deploy CF Worker Relay
```bash
cd worker-relay
npm install -g wrangler
wrangler login
wrangler deploy
```

### Step 2: Push to GitHub
```bash
git add .
git commit -m "feat: add GitHub Actions relay"
git remote add origin https://github.com/YOUR_USERNAME/GoVpn.git
git push -u origin main
```

### Step 3: Trigger the Workflow
```bash
# Via GitHub UI:
# Go to Actions → GoVpn Relay Runner → Run workflow

# Via CLI:
gh workflow run relay.yml
```

### Step 4: Run the Proxy
```bash
cd proxy-relay
go run proxy-relay.go
```

### Step 5: Test
```bash
# Direct through relay
curl http://localhost:8083/relay?url=https://httpbin.org/get

# Run test suite
bash tests/github-relay/test_relay.sh
```

## Trade-offs

| Aspect | Value | Notes |
|--------|-------|-------|
| **Latency** | ~1-3s per request | Polling interval + relay hops |
| **Throughput** | Low | One request at a time per runner |
| **Reliability** | Medium | GH Actions can terminate early |
| **Detection risk** | Low | Runner makes normal HTTPS outbound |
| **Cost** | Free | Public repo, 2000 min/month |
| **Duration** | 6 hours max | GH Actions job timeout limit |
| **State** | Ephemeral | Durable Object state persists between requests |

## Limitations

1. **No raw TCP tunneling** - The relay only forwards HTTP request/response pairs. CONNECT tunnels (for HTTPS) go directly, not through the relay.

2. **Single-threaded queue** - One request at a time per runner. Can run multiple runners for parallelism.

3. **Polling overhead** - 500ms poll delay means minimum latency is ~500ms + request time.

4. **GitHub ToS** - Using runners as a persistent proxy may violate GitHub's Terms of Service. Use responsibly and for testing only.

5. **IP reputation** - GitHub Actions runner IPs are well-known and may be blocked by some services.

## Future Improvements

- [ ] WebSocket support for real-time communication (no polling)
- [ ] Multiple concurrent runners with load balancing
- [ ] Request batching for higher throughput
- [ ] Encrypted payloads for additional privacy
- [ ] SOCKS5 proxy support via the relay
- [ ] Fallback to Vercel/Netlify if CF Worker is blocked

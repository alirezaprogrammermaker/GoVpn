# GoVpn

A VPN proxy project built with Go and Cloudflare Workers.

## Project Structure

```
GoVpn/
├── main.go                 # Entry point
├── proxy/
│   └── proxy.go            # HTTP Forward Proxy Server (:8080)
├── client/
│   └── client.go           # Test client (sends requests via proxy)
├── server/
│   └── server.go           # Target server for testing (:9090)
└── worker/
    ├── wrangler.toml        # Cloudflare Worker config
    └── src/
        └── index.js         # Cloudflare Worker source
```

## Components

### Proxy Server (`proxy/proxy.go`)
HTTP forward proxy that listens on `:8080` and forwards requests to target servers.

### Client (`client/client.go`)
Test client that sends HTTP requests through the proxy.

### Target Server (`server/server.go`)
Simple HTTP server on `:9090` for testing the proxy locally.

### Cloudflare Worker (`worker/`)
Deployed at: `https://govpn-worker.social-panel.workers.dev`

## Quick Start

```bash
# Run proxy server
go run proxy/proxy.go

# Run target server (in another terminal)
go run server/server.go

# Run client through proxy
go run client/client.go
```

## License

Private project.

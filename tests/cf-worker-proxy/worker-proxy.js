/**
 * Cloudflare Worker - Forward Proxy
 *
 * This worker acts as a proxy that forwards requests to the actual target.
 * Censors see traffic going to Cloudflare (hard to block), but the actual
 * destination is hidden inside the request.
 *
 * Usage:
 *   GET https://govpn-worker.social-panel.workers.dev/proxy?url=https://target.com/path
 *
 * Or with encoded URL:
 *   GET https://govpn-worker.social-panel.workers.dev/proxy?url=aHR0cHM6Ly90YXJnZXQuY29tL3BhdGg=
 *   (base64 encoded URL)
 */

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);

    // Health check
    if (url.pathname === "/health") {
      return Response.json({
        status: "ok",
        message: "CF Worker Proxy is alive! 🚀",
        timestamp: new Date().toISOString(),
      });
    }

    // Proxy endpoint
    if (url.pathname === "/proxy") {
      let targetUrl = url.searchParams.get("url");

      if (!targetUrl) {
        return Response.json(
          { error: "Missing 'url' parameter" },
          { status: 400 }
        );
      }

      // Try base64 decode if it doesn't look like a URL
      if (!targetUrl.startsWith("http")) {
        try {
          targetUrl = atob(targetUrl);
        } catch (e) {
          // Use as-is
        }
      }

      console.log(`[PROXY] ${request.method} → ${targetUrl}`);

      try {
        // Forward the request to target
        const proxyReq = new Request(targetUrl, {
          method: request.method,
          headers: request.headers,
          body: request.method !== "GET" && request.method !== "HEAD"
            ? request.body
            : undefined,
        });

        // Remove proxy-specific headers
        proxyReq.headers.delete("host");

        const response = await fetch(proxyReq);
        const responseBody = await response.text();

        console.log(`[PROXY] ✅ Response: ${response.status} (${responseBody.length} bytes)`);

        // Return response with CORS headers
        return new Response(responseBody, {
          status: response.status,
          headers: {
            "Content-Type": response.headers.get("Content-Type") || "application/json",
            "Access-Control-Allow-Origin": "*",
            "X-Proxy-By": "GoVpn-CF-Worker",
            "X-Target-Url": targetUrl,
          },
        });
      } catch (err) {
        console.log(`[PROXY] ❌ Error: ${err.message}`);
        return Response.json(
          { error: "Proxy failed", message: err.message },
          { status: 502 }
        );
      }
    }

    // Default response
    return Response.json({
      message: "GoVpn Cloudflare Worker Proxy",
      endpoints: {
        health: "/health",
        proxy: "/proxy?url=<target_url>",
      },
      usage: "GET /proxy?url=https://example.com",
    });
  },
};

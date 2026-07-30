/**
 * GoVpn GitHub Actions Relay - Cloudflare Worker (KV-based)
 *
 * Uses Cloudflare KV for the request queue (free tier compatible).
 *
 * Architecture:
 *   Client → /enqueue → KV Queue ← /poll ← Runner
 *   Client → /result/:id → KV ← /response/:id ← Runner
 *
 * KV keys:
 *   pending:<id>   → request data (TTL 5 min)
 *   result:<id>    → response data (TTL 5 min)
 *   queue:<ts>     → pointer for ordering (TTL 5 min)
 */

// ═══════════════════════════════════════════════════════════════
//  Helper: JSON Response
// ═══════════════════════════════════════════════════════════════

function jsonResponse(data, status = 200) {
  return new Response(JSON.stringify(data, null, 2), {
    status,
    headers: {
      "Content-Type": "application/json",
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Methods": "GET, POST, OPTIONS",
      "Access-Control-Allow-Headers": "Content-Type, X-Runner-Id",
    },
  });
}

// ═══════════════════════════════════════════════════════════════
//  Main Worker
// ═══════════════════════════════════════════════════════════════

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    const path = url.pathname;

    // CORS preflight
    if (request.method === "OPTIONS") {
      return new Response(null, {
        headers: {
          "Access-Control-Allow-Origin": "*",
          "Access-Control-Allow-Methods": "GET, POST, OPTIONS",
          "Access-Control-Allow-Headers": "Content-Type, X-Runner-Id",
        },
      });
    }

    try {
      // ─── Health ───
      if (path === "/health") {
        return jsonResponse({
          status: "ok",
          message: "GoVpn Relay Queue is alive! 🚀",
          storage: "KV",
          timestamp: new Date().toISOString(),
        });
      }

      // ─── Enqueue: Client submits a request ───
      if (path === "/enqueue" && request.method === "POST") {
        const body = await request.json();
        const { method, url: targetUrl, headers, body: reqBody } = body;

        if (!targetUrl) {
          return jsonResponse({ error: "Missing 'url' field" }, 400);
        }

        const id = crypto.randomUUID();
        const entry = {
          id,
          method: method || "GET",
          url: targetUrl,
          headers: headers || {},
          body: reqBody || null,
          timestamp: Date.now(),
          status: "pending",
        };

        // Store in KV with 5 minute TTL
        await env.QUEUE.put(`pending:${id}`, JSON.stringify(entry), {
          expirationTtl: 300,
        });

        // Also store a queue pointer for the runner to find
        await env.QUEUE.put(`queue:${Date.now()}`, id, {
          expirationTtl: 300,
        });

        console.log(`[ENQUEUE] ${id} → ${method} ${targetUrl}`);

        return jsonResponse({ id, status: "pending" });
      }

      // ─── Poll: Runner requests next pending item ───
      if (path === "/poll" && request.method === "GET") {
        const runnerId = request.headers.get("X-Runner-Id") || "anonymous";

        // List all pending keys
        const pendingKeys = await env.QUEUE.list({ prefix: "pending:" });

        if (pendingKeys.keys.length === 0) {
          return jsonResponse({ empty: true });
        }

        // Try to claim the first pending request
        for (const key of pendingKeys.keys) {
          const id = key.name.replace("pending:", "");
          const data = await env.QUEUE.get(key.name, "json");

          if (data && data.status === "pending") {
            // Mark as processing
            data.status = "processing";
            data.runnerId = runnerId;
            data.claimedAt = Date.now();

            await env.QUEUE.put(key.name, JSON.stringify(data), {
              expirationTtl: 300,
            });

            // Remove from queue pointer
            // (We'll find it via pending: prefix anyway)

            console.log(`[POLL] Runner ${runnerId} picked up ${id}`);
            return jsonResponse(data);
          }
        }

        return jsonResponse({ empty: true });
      }

      // ─── Response: Runner posts result ───
      if (path.startsWith("/response/") && request.method === "POST") {
        const id = path.split("/response/")[1];
        const body = await request.json();

        const response = {
          id,
          status: body.status || 200,
          headers: body.headers || {},
          body: body.body || "",
          timestamp: Date.now(),
        };

        // Store result with 5 minute TTL
        await env.QUEUE.put(`result:${id}`, JSON.stringify(response), {
          expirationTtl: 300,
        });

        // Remove from pending
        await env.QUEUE.delete(`pending:${id}`);

        console.log(`[RESPONSE] ${id} → ${response.status} (${(response.body || "").length} bytes)`);

        return jsonResponse({ ok: true });
      }

      // ─── Result: Client polls for response ───
      if (path.startsWith("/result/")) {
        const id = path.split("/result/")[1];

        // Check for result
        const result = await env.QUEUE.get(`result:${id}`, "json");
        if (result) {
          // Clean up
          await env.QUEUE.delete(`result:${id}`);
          return jsonResponse(result);
        }

        // Check if still pending/processing
        const pending = await env.QUEUE.get(`pending:${id}`, "json");
        if (pending) {
          return jsonResponse({
            status: pending.status,
            message: pending.status === "processing"
              ? "Being processed by runner..."
              : "Waiting in queue...",
          });
        }

        return jsonResponse({ error: "Request not found" }, 404);
      }

      // ─── Stats ───
      if (path === "/stats") {
        const pendingKeys = await env.QUEUE.list({ prefix: "pending:" });
        const resultKeys = await env.QUEUE.list({ prefix: "result:" });

        return jsonResponse({
          pending: pendingKeys.keys.length,
          results: resultKeys.keys.length,
          timestamp: new Date().toISOString(),
        });
      }

      // ─── Purge: Clear all data ───
      if (path === "/purge" && request.method === "POST") {
        const allKeys = await env.QUEUE.list();
        let deleted = 0;

        for (const key of allKeys.keys) {
          await env.QUEUE.delete(key.name);
          deleted++;
        }

        return jsonResponse({ deleted, message: "Queue purged" });
      }

      // ─── Default: API info ───
      return jsonResponse({
        name: "GoVpn GitHub Actions Relay",
        version: "1.0.0",
        storage: "Cloudflare KV (free tier)",
        endpoints: {
          "POST /enqueue": "Submit a request to the queue",
          "GET /poll": "Runner polls for pending requests",
          "POST /response/:id": "Runner submits response",
          "GET /result/:id": "Client gets response",
          "GET /health": "Health check",
          "GET /stats": "Queue statistics",
          "POST /purge": "Clear all queue data",
        },
      });
    } catch (err) {
      console.error(`[ERROR] ${err.message}`);
      return jsonResponse({ error: err.message }, 500);
    }
  },
};

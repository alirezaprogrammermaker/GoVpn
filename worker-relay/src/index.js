/**
 * GoVpn GitHub Actions Relay - Cache-Based Queue
 *
 * Uses Cloudflare Cache API instead of KV to avoid rate limits.
 * Cache API has no per-operation limits on free tier!
 *
 * Trick: We cache "fake" HTTP responses that contain our queue data.
 * Each cache key acts as a data slot.
 */

const CACHE_NAME = "govpn-relay-queue";

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

// Cache helpers
async function cachePut(key, data) {
  const cache = await caches.open(CACHE_NAME);
  const response = new Response(JSON.stringify(data), {
    headers: { "Content-Type": "application/json", "Cache-Control": "max-age=300" },
  });
  await cache.put(new Request(`https://cache.local/${key}`), response);
}

async function cacheGet(key) {
  const cache = await caches.open(CACHE_NAME);
  const response = await cache.match(new Request(`https://cache.local/${key}`));
  if (!response) return null;
  return await response.json();
}

async function cacheDelete(key) {
  const cache = await caches.open(CACHE_NAME);
  await cache.delete(new Request(`https://cache.local/${key}`));
}

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    const path = url.pathname;

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
          message: "GoVpn Relay is alive! 🚀",
          storage: "Cache API (no rate limits!)",
          timestamp: new Date().toISOString(),
        });
      }

      // ─── Enqueue ───
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
        };

        // Get current queue
        let queue = (await cacheGet("queue")) || [];
        queue.push(entry);
        await cachePut("queue", queue);

        console.log(`[ENQUEUE] ${id} → ${method} ${targetUrl} (queue: ${queue.length})`);
        return jsonResponse({ id, status: "pending" });
      }

      // ─── Poll ───
      if (path === "/poll" && request.method === "GET") {
        const runnerId = request.headers.get("X-Runner-Id") || "anonymous";

        let queue = (await cacheGet("queue")) || [];

        if (queue.length === 0) {
          return jsonResponse({ empty: true });
        }

        const entry = queue.shift();
        await cachePut("queue", queue);

        console.log(`[POLL] Runner ${runnerId} got ${entry.id} (remaining: ${queue.length})`);
        return jsonResponse(entry);
      }

      // ─── Response ───
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

        await cachePut(`result:${id}`, response);
        console.log(`[RESPONSE] ${id} → ${response.status}`);
        return jsonResponse({ ok: true });
      }

      // ─── Result ───
      if (path.startsWith("/result/")) {
        const id = path.split("/result/")[1];

        const result = await cacheGet(`result:${id}`);
        if (result) {
          await cacheDelete(`result:${id}`);
          return jsonResponse(result);
        }

        return jsonResponse({ status: "processing", message: "Waiting..." });
      }

      // ─── Purge ───
      if (path === "/purge" && request.method === "POST") {
        await cachePut("queue", []);
        return jsonResponse({ message: "Queue cleared" });
      }

      // ─── Default ───
      return jsonResponse({
        name: "GoVpn GitHub Actions Relay",
        version: "5.0.0 (cache-based)",
        storage: "Cloudflare Cache API",
        rateLimits: "None!",
        endpoints: {
          "POST /enqueue": "Submit request",
          "GET /poll": "Runner polls for work",
          "POST /response/:id": "Runner submits response",
          "GET /result/:id": "Client gets response",
        },
      });
    } catch (err) {
      console.error(`[ERROR] ${err.message}`);
      return jsonResponse({ error: err.message }, 500);
    }
  },
};

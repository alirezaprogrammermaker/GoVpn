export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);

    // Health check
    if (url.pathname === "/health") {
      return Response.json({
        status: "ok",
        message: "GoVpn Worker is alive! 🚀",
        timestamp: new Date().toISOString(),
      });
    }

    // Echo request info
    return Response.json({
      message: "Hello from GoVpn Cloudflare Worker!",
      method: request.method,
      url: request.url,
      headers: Object.fromEntries(request.headers.entries()),
      timestamp: new Date().toISOString(),
    });
  },
};

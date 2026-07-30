export default function handler(req, res) {
  const { method, query, headers } = req;

  // Health check
  if (query.action === "health") {
    return res.status(200).json({
      status: "ok",
      message: "GoVpn Vercel API is alive! 🚀",
      timestamp: new Date().toISOString(),
      platform: "Vercel",
    });
  }

  // Default response
  return res.status(200).json({
    message: "Hello from GoVpn Vercel API!",
    method: method,
    query: query,
    headers: {
      "user-agent": headers["user-agent"],
      host: headers["host"],
      "x-forwarded-for": headers["x-forwarded-for"],
    },
    timestamp: new Date().toISOString(),
    platform: "Vercel",
  });
}

export default function handler(req, res) {
  return res.status(200).json({
    status: "ok",
    message: "GoVpn Vercel API is healthy! ✅",
    uptime: process.uptime(),
    timestamp: new Date().toISOString(),
    platform: "Vercel",
  });
}

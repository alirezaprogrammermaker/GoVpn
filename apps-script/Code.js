/**
 * GoVpn API - Google Apps Script Web App
 * Handles incoming HTTP requests and returns JSON responses
 */

function doGet(e) {
  var params = e ? e.parameter : {};

  // Health check endpoint
  if (params.action === "health") {
    return jsonResponse({
      status: "ok",
      message: "GoVpn Apps Script API is alive! 🚀",
      timestamp: new Date().toISOString(),
      platform: "Google Apps Script"
    });
  }

  // Default echo response
  return jsonResponse({
    message: "Hello from GoVpn Apps Script!",
    method: "GET",
    params: params,
    timestamp: new Date().toISOString(),
    platform: "Google Apps Script"
  });
}

function doPost(e) {
  var body = {};
  try {
    body = JSON.parse(e.postData.contents);
  } catch (err) {
    body = { error: "Invalid JSON" };
  }

  return jsonResponse({
    message: "Received POST request!",
    method: "POST",
    body: body,
    timestamp: new Date().toISOString(),
    platform: "Google Apps Script"
  });
}

/**
 * Helper: Create JSON response
 */
function jsonResponse(data) {
  return ContentService
    .createTextOutput(JSON.stringify(data, null, 2))
    .setMimeType(ContentService.MimeType.JSON);
}

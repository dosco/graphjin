import { createServer } from "node:http";

const HOST = "0.0.0.0";
const PORT = 8090;
const MAX_BODY_BYTES = 16 * 1024;

function json(response, status, body) {
  const payload = JSON.stringify(body);
  response.writeHead(status, {
    "content-type": "application/json",
    "content-length": Buffer.byteLength(payload),
  });
  response.end(payload);
}

createServer((request, response) => {
  if (request.method === "GET" && request.url === "/health") {
    return json(response, 200, { ok: true });
  }
  if (request.method !== "POST" || request.url !== "/selections") {
    return json(response, 404, { error: "not_found" });
  }

  let body = "";
  request.setEncoding("utf8");
  request.on("data", (chunk) => {
    body += chunk;
    if (Buffer.byteLength(body) > MAX_BODY_BYTES) request.destroy();
  });
  request.on("end", () => {
    let parsed;
    try {
      parsed = JSON.parse(body);
    } catch {
      return json(response, 400, { error: "invalid_json" });
    }
    if (parsed?.order_id !== "SO-43659" || parsed?.priority !== "expedite") {
      return json(response, 422, { error: "invalid_selection_request" });
    }
    return json(response, 201, {
      receipt: "AW-API-SELECTION-APPROVED",
      route: "northwest-priority",
      order_id: parsed.order_id,
    });
  });
}).listen(PORT, HOST, () => {
  process.stdout.write(`[eval-api-fixture] listening on ${HOST}:${PORT}\n`);
});

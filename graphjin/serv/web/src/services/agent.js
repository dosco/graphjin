import { fetchJSON, RequestError } from "./graphql";

export function agentStatus() {
  return fetchJSON("/api/v1/agent/status");
}

export function askAgent({ instruction, max_steps, return_trace, history }) {
  return fetchJSON("/api/v1/agent", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      instruction,
      max_steps,
      return_trace,
      history,
    }),
  });
}

// askAgentStream posts the same request with `Accept: text/event-stream` and
// parses the SSE frames the agent endpoint emits: one `action` event per
// executed tool call, then a final `result` event with the agent response.
// Falls back to the blocking JSON contract when the server answers with JSON.
export async function askAgentStream({ instruction, max_steps, return_trace, history }, { onAction } = {}) {
  let response;
  try {
    response = await fetch("/api/v1/agent", {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        Accept: "text/event-stream",
      },
      body: JSON.stringify({ instruction, max_steps, return_trace, history }),
    });
  } catch (error) {
    throw new RequestError(`Service unavailable: ${error.message}`, { kind: "unavailable" });
  }

  const contentType = response.headers.get("Content-Type") || "";
  if (!response.ok || !response.body || !contentType.includes("text/event-stream")) {
    const text = await response.text();
    let payload = {};
    if (text) {
      try {
        payload = JSON.parse(text);
      } catch {
        throw new RequestError("GraphJin returned a non-JSON response.", {
          status: response.status,
          kind: "invalid",
          responseText: text.slice(0, 240),
        });
      }
    }
    if (!response.ok) {
      throw new RequestError(payload?.errors?.[0]?.message || payload?.message || `Request failed (${response.status})`, {
        status: response.status,
        errors: payload?.errors || [],
      });
    }
    return payload;
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let result = null;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    buffer += decoder.decode(value, { stream: true });
    let boundary;
    while ((boundary = buffer.indexOf("\n\n")) >= 0) {
      const frame = parseSSEFrame(buffer.slice(0, boundary));
      buffer = buffer.slice(boundary + 2);
      if (!frame) {
        continue;
      }
      if (frame.event === "action" && typeof onAction === "function") {
        onAction(frame.data);
      } else if (frame.event === "result") {
        result = frame.data;
      }
    }
  }
  if (!result) {
    throw new RequestError("The agent stream ended without a result.", { kind: "invalid" });
  }
  return result;
}

function parseSSEFrame(frame) {
  let event = "message";
  const dataLines = [];
  for (const line of frame.split("\n")) {
    if (line.startsWith("event:")) {
      event = line.slice(6).trim();
    } else if (line.startsWith("data:")) {
      dataLines.push(line.slice(5).trim());
    }
  }
  if (dataLines.length === 0) {
    return null;
  }
  try {
    return { event, data: JSON.parse(dataLines.join("\n")) };
  } catch {
    return null;
  }
}

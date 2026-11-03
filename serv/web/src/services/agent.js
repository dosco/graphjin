import { fetchJSON, RequestError } from "./graphql";
import { operatorIdentityHeaders } from "./identity";

export function agentStatus() {
  return fetchJSON("/api/v1/agent/status");
}

export function askAgent({ instruction, max_steps, return_trace, history, task_id }) {
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
      task_id,
    }),
  });
}

// askAgentStream posts the same request with `Accept: text/event-stream` and
// parses the SSE frames the agent endpoint emits: one `action` event per
// executed tool call, then a final `result` event with the agent response.
// Falls back to the blocking JSON contract when the server answers with JSON.
export async function askAgentStream({ instruction, max_steps, return_trace, history, task_id, thread_id }, { onAction, onRun } = {}) {
  let response;
  try {
    response = await fetch("/api/v1/agent", {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        Accept: "text/event-stream",
        ...operatorIdentityHeaders(),
      },
      body: JSON.stringify({ instruction, max_steps, return_trace, history, task_id, thread_id }),
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
      throw new RequestError(payload?.errors?.[0]?.message || payload?.message || payload?.error || `Request failed (${response.status})`, {
        status: response.status,
        errors: payload?.errors || [],
      });
    }
    return payload;
  }
  return readAgentEvents(response, { onAction, onRun });
}

// readAgentEvents consumes an agent SSE stream: `run` names a threaded run,
// `action` reports each tool call, `result` carries the final response.
// Action payloads pass through whole: a lead run's actions carry `delegate`
// (the 1-based delegated question) and `question`, which the trail groups on.
async function readAgentEvents(response, { onAction, onRun } = {}) {
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
      if (frame.event === "run" && typeof onRun === "function") {
        onRun(frame.data);
      } else if (frame.event === "action" && typeof onAction === "function") {
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

// Persistent threads (threads.enabled). A thread's stored messages are the
// conversation history, so threaded requests send thread_id instead of
// history, and a run can be resumed or cancelled after a reload.
export function listThreads() {
  return fetchJSON("/api/v1/agent/threads").then((payload) => payload?.data || []);
}

export function createThread({ title = "", profile = "" } = {}) {
  return fetchJSON("/api/v1/agent/threads", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ title, profile }),
  }).then((payload) => payload?.data);
}

export function fetchThread(threadID) {
  return fetchJSON(`/api/v1/agent/threads/${encodeURIComponent(threadID)}`).then((payload) => payload?.data);
}

export function updateThread(threadID, patch) {
  return fetchJSON(`/api/v1/agent/threads/${encodeURIComponent(threadID)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
  }).then((payload) => payload?.data);
}

// markThreadSeen clears a thread's unread state (threads report unread when an
// assistant message landed after the owner last looked).
export function markThreadSeen(threadID) {
  return updateThread(threadID, { seen: true });
}

export function cancelRun(runID) {
  return fetchJSON(`/api/v1/agent/runs/${encodeURIComponent(runID)}/cancel`, { method: "POST" }).then((payload) => payload?.data);
}

// followRun replays a run's stored events and follows it until it completes;
// any GraphJin replica can serve it.
export async function followRun(runID, { onAction, after = 0 } = {}) {
  let response;
  try {
    response = await fetch(`/api/v1/agent/runs/${encodeURIComponent(runID)}/events?after=${after}`, {
      credentials: "same-origin",
      headers: { Accept: "text/event-stream", ...operatorIdentityHeaders() },
    });
  } catch (error) {
    throw new RequestError(`Service unavailable: ${error.message}`, { kind: "unavailable" });
  }
  if (!response.ok || !response.body) {
    throw new RequestError(`Run stream failed (${response.status})`, { status: response.status });
  }
  return readAgentEvents(response, { onAction });
}

// Memory (memory.enabled): personal facts the agent recalls in every run;
// publishing one shares it with the caller's account as team memory.
export function listMemories() {
  return fetchJSON("/api/v1/agent/memories").then((payload) => payload?.data || []);
}

export function addMemory(content) {
  return fetchJSON("/api/v1/agent/memories", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ content }),
  }).then((payload) => payload?.data);
}

export function updateMemory(id, patch) {
  return fetchJSON(`/api/v1/agent/memories/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
  }).then((payload) => payload?.data);
}

export function deleteMemory(id) {
  return fetchJSON(`/api/v1/agent/memories/${encodeURIComponent(id)}`, { method: "DELETE" });
}

// Agent profiles: named agents with standing instructions. Each has one home
// thread per user, found or created on first open; sharing one (tier
// approved) makes it available to the caller's account.
export function listProfiles() {
  return fetchJSON("/api/v1/agent/profiles").then((payload) => payload?.data || []);
}

export function createProfile({ name, instructions = "", icon = "" }) {
  return fetchJSON("/api/v1/agent/profiles", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, instructions, icon }),
  }).then((payload) => payload?.data);
}

export function updateProfile(id, patch) {
  return fetchJSON(`/api/v1/agent/profiles/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
  }).then((payload) => payload?.data);
}

export function deleteProfile(id) {
  return fetchJSON(`/api/v1/agent/profiles/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export function profileHomeThread(id) {
  return fetchJSON(`/api/v1/agent/profiles/${encodeURIComponent(id)}/thread`).then((payload) => payload?.data);
}

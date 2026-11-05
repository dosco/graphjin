import { fetchJSON, graphqlRequest, RequestError } from "./graphql";
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

// Everything below reads and writes through the graph: threads are the
// gj_thread / gj_thread_message / gj_thread_run roots, and memories, skills
// and agent profiles are gj_artifacts rows of their kind. Only starting a
// turn (POST /api/v1/agent), following or cancelling a run, and finding a
// profile's home thread are REST actions.

async function gql(query, variables = {}) {
  const payload = await graphqlRequest(query, variables);
  if (payload.errors?.length) {
    throw new RequestError(payload.errors.map((error) => error.message).join("; "), { errors: payload.errors });
  }
  return payload.data || {};
}

// The caller's own rows carry the bootstrap's user_ref as owner_ref.
let userRefPromise;
function userRef() {
  userRefPromise ||= fetchJSON("/api/v1/console/bootstrap")
    .then((bootstrap) => bootstrap?.identity?.user_ref || "")
    .catch(() => "");
  return userRefPromise;
}

function scoped(row, ref) {
  return { ...row, scope: row.tier === "approved" ? "team" : "personal", mine: Boolean(ref) && row.owner_ref === ref };
}

const threadFields = "id title profile status channel active_run_id unread last_message_at last_seen_at created_at updated_at";

// Persistent threads (threads.enabled). A thread's stored messages are the
// conversation history, so threaded requests send thread_id instead of
// history, and a run can be resumed or cancelled after a reload.
export async function listThreads() {
  const data = await gql(`query { gj_thread(where: { status: { eq: "open" } }, order_by: { last_message_at: desc }) { ${threadFields} } }`);
  return data.gj_thread || [];
}

export async function createThread({ title = "", profile = "" } = {}) {
  const data = await gql(`mutation ($title: String!, $profile: String!) { gj_thread(insert: { title: $title, profile: $profile }) { ${threadFields} } }`, { title, profile });
  return data.gj_thread;
}

export async function fetchThread(threadID) {
  const data = await gql(
    `query ($id: String!) {
      gj_thread(where: { id: { eq: $id } }) { ${threadFields} }
      gj_thread_message(where: { thread_id: { eq: $id } }, order_by: { seq: asc }) { id thread_id seq role content status run_id catalog_ids response created_at }
    }`,
    { id: threadID },
  );
  const thread = (data.gj_thread || [])[0];
  return thread ? { ...thread, messages: data.gj_thread_message || [] } : undefined;
}

export async function updateThread(threadID, patch) {
  const input = { ...patch };
  if (input.seen) {
    input.last_seen_at = "now";
  }
  delete input.seen;
  const data = await gql(`mutation ($id: String!, $input: json!) { gj_thread(where: { id: { eq: $id } }, update: $input) { ${threadFields} } }`, { id: threadID, input });
  return data.gj_thread;
}

export async function deleteThread(threadID) {
  return gql(`mutation ($id: String!) { gj_thread(where: { id: { eq: $id } }, delete: true) { id } }`, { id: threadID });
}

// markThreadSeen clears a thread's unread state (threads report unread when an
// assistant message landed after the owner last looked).
export function markThreadSeen(threadID) {
  return updateThread(threadID, { seen: true });
}

// runsForThread lists a thread's runs with their cost and the skills and
// memories that shaped each answer.
export async function runsForThread(threadID) {
  const data = await gql(
    `query ($id: String!) { gj_thread_run(where: { thread_id: { eq: $id } }) { id status tokens_in tokens_out llm_calls used_skills used_memories created_at finished_at } }`,
    { id: threadID },
  );
  return data.gj_thread_run || [];
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

const artifactFields = "id name kind content description icon tier owner_ref created_at updated_at";

async function listArtifacts(kind) {
  const [data, ref] = await Promise.all([
    gql(`query ($kind: String!) { gj_artifacts(where: { kind: { eq: $kind } }, order_by: { name: asc }) { ${artifactFields} } }`, { kind }),
    userRef(),
  ]);
  return (data.gj_artifacts || []).map((row) => scoped(row, ref));
}

async function insertArtifact(input) {
  const [data, ref] = await Promise.all([
    gql(`mutation ($input: json!) { gj_artifacts(insert: $input) { ${artifactFields} } }`, { input }),
    userRef(),
  ]);
  return scoped(data.gj_artifacts, ref);
}

async function updateArtifact(id, input) {
  const [data, ref] = await Promise.all([
    gql(`mutation ($id: String!, $input: json!) { gj_artifacts(where: { id: { eq: $id } }, update: $input) { ${artifactFields} } }`, { id, input }),
    userRef(),
  ]);
  return scoped(data.gj_artifacts, ref);
}

function deleteArtifact(id) {
  return gql(`mutation ($id: String!) { gj_artifacts(where: { id: { eq: $id } }, delete: true) { id } }`, { id });
}

// Memory (memory.enabled): personal facts the agent recalls in every run;
// publishing one shares it with the caller's account as team memory.
export function listMemories() {
  return listArtifacts("memory");
}

export function addMemory(content) {
  return insertArtifact({ kind: "memory", content });
}

export function updateMemory(id, patch) {
  return updateArtifact(id, patch);
}

export function deleteMemory(id) {
  return deleteArtifact(id);
}

// Skills: named instructions the lead loads when a request matches their
// description. Sharing one (tier approved) makes it available to the account.
export function listSkills() {
  return listArtifacts("skill");
}

export function addSkill({ name, description, content }) {
  return insertArtifact({ kind: "skill", name, description, content });
}

export function updateSkill(id, patch) {
  return updateArtifact(id, patch);
}

export function deleteSkill(id) {
  return deleteArtifact(id);
}

// Agent profiles: named agents with standing instructions. Each has one home
// thread per user, found or created on first open; sharing one (tier
// approved) makes it available to the caller's account.
function profileRow(row) {
  return row && { ...row, instructions: row.content };
}

function profileInput(patch) {
  const input = { ...patch };
  if ("instructions" in input) {
    input.content = input.instructions;
    delete input.instructions;
  }
  return input;
}

export async function listProfiles() {
  return (await listArtifacts("agent_profile")).map(profileRow);
}

export async function createProfile({ name, instructions = "", icon = "" }) {
  return profileRow(await insertArtifact({ kind: "agent_profile", name, content: instructions, icon }));
}

export async function updateProfile(id, patch) {
  return profileRow(await updateArtifact(id, profileInput(patch)));
}

export function deleteProfile(id) {
  return deleteArtifact(id);
}

export function profileHomeThread(id) {
  return fetchJSON(`/api/v1/agent/profiles/${encodeURIComponent(id)}/thread`).then((payload) => payload?.data);
}

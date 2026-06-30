import { fetchJSON } from "./graphql";

export function agentStatus() {
  return fetchJSON("/api/v1/agent/status");
}

export function askAgent({ instruction, mode, max_steps, return_trace }) {
  return fetchJSON("/api/v1/agent", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      instruction,
      mode,
      max_steps,
      return_trace,
    }),
  });
}

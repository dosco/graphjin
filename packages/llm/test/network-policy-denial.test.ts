import { describe, expect, it } from "vitest";
import type { AgentEvent } from "../src/agent-backend";
import { extractNetworkPolicyDenial } from "../src/work/run-chat-turn";

describe("extractNetworkPolicyDenial", () => {
  it("turns a web_fetch allowlist refusal into a structured capability event", () => {
    expect(
      extractNetworkPolicyDenial({
        type: "tool_end",
        id: "t1",
        name: "web_fetch",
        error: "host api.example.com is not on the web allowlist",
      } as never),
    ).toEqual({
      type: "capability_denied",
      capability: "network_egress",
      reason: "policy_denied",
      host: "api.example.com",
      method: "GET",
    });
  });

  it("turns a legacy policy denial line into a structured capability event", () => {
    const event: AgentEvent = {
      type: "tool_end",
      id: "terminal-1",
      result: JSON.stringify({
        detail:
          "GET wttr.in:80/Igatpuri?format=3 not permitted by policy",
        error: "policy_denied",
      }),
    };

    expect(extractNetworkPolicyDenial(event)).toEqual({
      type: "capability_denied",
      capability: "network_egress",
      reason: "policy_denied",
      host: "wttr.in",
      port: 80,
      method: "GET",
      path: "/Igatpuri?format=3",
    });
  });

  it("ignores ordinary tool errors", () => {
    const event: AgentEvent = {
      type: "tool_end",
      id: "terminal-2",
      error: "command exited with status 1",
    };

    expect(extractNetworkPolicyDenial(event)).toBeNull();
  });
});

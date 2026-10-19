import { describe, expect, it } from "vitest";
import {
  combineAgentTokenUsage,
  normalizeAxProgramUsage,
  graphjinAgentResponseStatus,
  normalizeGraphjinAgentUsage,
} from "../src/usage-normalization";

describe("provider usage normalization", () => {
  it("sums every Ax classifier call and retry", () => {
    const usage = normalizeAxProgramUsage([
      {
        ai: "google-gemini",
        model: "gemini-test",
        tokens: { promptTokens: 10, completionTokens: 4, totalTokens: 14 },
      },
      {
        ai: "google-gemini",
        model: "gemini-test",
        tokens: {
          promptTokens: 12,
          completionTokens: 3,
          totalTokens: 15,
          thoughtsTokens: 2,
        },
      },
    ]);
    expect(usage).toEqual({
      inputTokens: 22,
      outputTokens: 7,
      reasoningTokens: 2,
      totalTokens: 29,
      coverage: "complete",
    });
  });

  it("combines classifier and agent tokens without treating a missing scope as zero", () => {
    expect(
      combineAgentTokenUsage(
        { inputTokens: 10, outputTokens: 2, totalTokens: 12, coverage: "complete" },
        {
          coverage: "unavailable",
          missingReasons: ["metric-agent usage unavailable"],
        },
      ),
    ).toEqual({
      inputTokens: 10,
      outputTokens: 2,
      totalTokens: 12,
      coverage: "partial",
      missingReasons: ["metric-agent usage unavailable"],
    });
  });

  it("finds inner-model usage inside an MCP-encoded GraphJin response", () => {
    expect(
      normalizeGraphjinAgentUsage({
        content: [
          {
            type: "text",
            text: JSON.stringify({
              agentStatus: { provider: "gemini", model: "gemini-3.6-flash" },
              response: {
                usage: {
                  prompt_tokens: 20,
                  completion_tokens: 6,
                  total_tokens: 26,
                },
              },
            }),
          },
        ],
      }),
    ).toEqual({
      provider: "gemini",
      model: "gemini-3.6-flash",
      usage: {
        inputTokens: 20,
        outputTokens: 6,
        totalTokens: 26,
        coverage: "complete",
      },
    });
  });

  it("sums every GraphJin agent model call and reads the response status", () => {
    const result = {
      content: [
        {
          type: "text",
          text: JSON.stringify({
            agentStatus: { status: "ready", provider: "google-gemini", model: "gemini-3.8-flash" },
            response: {
              status: "answered",
              usage: {
                actor: [
                  { prompt_tokens: 100, completion_tokens: 10, total_tokens: 110 },
                  { prompt_tokens: 200, completion_tokens: 20, total_tokens: 220 },
                ],
                responder: [{ prompt_tokens: 50, completion_tokens: 5, total_tokens: 55 }],
              },
            },
          }),
        },
      ],
    };
    expect(normalizeGraphjinAgentUsage(result)).toMatchObject({
      modelCalls: 3,
      usage: { inputTokens: 350, outputTokens: 35, totalTokens: 385, coverage: "complete" },
    });
    expect(graphjinAgentResponseStatus(result)).toBe("answered");
    expect(graphjinAgentResponseStatus({ error: "GraphJin agent is not ready" })).toBe("error");
  });
});

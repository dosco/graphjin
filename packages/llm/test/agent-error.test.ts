import { describe, expect, it } from "vitest";
import {
  detectAgentExecutionError,
  detectDataSourceAuthorizationError,
  detectUpstreamError,
} from "../src/agent-error";

describe("detectUpstreamError", () => {
  it("classifies explicit provider authentication failures", () => {
    const error = detectUpstreamError("OpenAI HTTP 401: invalid API key");

    expect(error?.kind).toBe("auth");
    expect(error?.message).toContain("Provider authentication failed");
  });

  it("does not relabel GraphJin authorization failures as provider auth", () => {
    const output =
      "GraphJin query failed: unauthorized: table productcategory is blocked for role anon";

    expect(detectUpstreamError(output)).toBeNull();
    expect(detectDataSourceAuthorizationError(output)?.message).toContain(
      "Data source authorization failed",
    );
    expect(detectAgentExecutionError(output)?.name).toBe(
      "DataSourceAuthorizationError",
    );
  });

  it("does not classify an unscoped unauthorized message as provider auth", () => {
    expect(detectUpstreamError("Unauthorized to read the requested table")).toBeNull();
  });
});

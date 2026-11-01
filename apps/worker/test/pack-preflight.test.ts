import { beforeEach, describe, expect, it, vi } from "vitest";
import type { SolutionPackBundle } from "@neko/packs";

const { graphjinQuery } = vi.hoisted(() => ({ graphjinQuery: vi.fn() }));

vi.mock("@neko/llm/graphjin", () => ({
  graphjinQuery,
  mintGraphjinToken: () => "fixture-token",
}));

import { PackPreflightError, runPackReadPreflight } from "../src/packs/preflight.js";

function pack(): SolutionPackBundle {
  return {
    manifest: {
      metadata: { id: "third-party-pack" },
      health: { requiredPreflight: [], postInstall: [], postWriteCanary: [], readiness: {} },
    },
    artifacts: [{
      kind: "saved_query",
      key: "query.account_profile",
      targetRef: "account_profile",
      path: "graphjin/saved-queries/account_profile.gql",
      hash: "fixture",
      content: "query AccountProfile { customer_api_get_profile(accountId: \"current\") { displayName } }",
    }],
  } as SolutionPackBundle;
}

describe("pack query preflight errors", () => {
  beforeEach(() => graphjinQuery.mockReset());

  it("identifies the pack artifact and preserves GraphJin error details", async () => {
    graphjinQuery.mockResolvedValue({
      data: null,
      errors: [{ message: "table not found: customer_api_get_profile", path: ["customer_api_get_profile"] }],
    });

    const failure = await runPackReadPreflight(pack(), "http://graphjin.test", "org-test", {})
      .then(() => null, (error: unknown) => error);

    expect(failure).toBeInstanceOf(PackPreflightError);
    expect(failure).toMatchObject({
      diagnostic: {
        code: "pack_query_preflight_failed",
        phase: "query_preflight",
        artifact: {
          kind: "saved_query",
          name: "account_profile",
          path: "graphjin/saved-queries/account_profile.gql",
        },
        causes: ["table not found: customer_api_get_profile (path: customer_api_get_profile)"],
      },
    });
    expect((failure as Error).message).toContain("graphjin/saved-queries/account_profile.gql");
    expect((failure as Error).message).toContain("table not found: customer_api_get_profile");
  });
});

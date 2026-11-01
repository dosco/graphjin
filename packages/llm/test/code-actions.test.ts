// OL6 — conservative code actions: issue filing hits the forge with the
// configured token (and validates inputs hard); patch drafting only
// writes a local artifact and never touches a repo.

import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  draftPatchAdapter,
  makeCreateIssueAdapter,
} from "../src/workflows/adapters/code";

const request = (
  payload: Record<string, unknown>,
  kind = "code_create_issue",
) =>
  ({
    request: {
      id: "11111111-2222-3333-4444-555555555555",
      orgId: "org-1",
      kind,
      payload,
    },
  }) as never;

describe("code_create_issue adapter", () => {
  it("posts to the forge and returns the issue ref", async () => {
    const calls: Array<{ url: string; body: Record<string, unknown> }> = [];
    const adapter = makeCreateIssueAdapter({
      token: "tok",
      fetchImpl: (async (url: string, init: RequestInit) => {
        calls.push({ url: String(url), body: JSON.parse(String(init.body)) });
        return new Response(
          JSON.stringify({ number: 42, html_url: "https://github.com/o/r/issues/42" }),
          { status: 201 },
        );
      }) as never,
    });
    const outcome = await adapter(
      request({
        repo: "o/r",
        title: "Refund spike after deploy",
        body: "evidence...",
        labels: ["ops"],
      }),
    );
    expect(calls[0].url).toContain("/repos/o/r/issues");
    expect(calls[0].body.title).toBe("Refund spike after deploy");
    expect(outcome.result).toMatchObject({ number: 42, repo: "o/r" });
  });

  it("rejects bad repos, missing titles, and missing tokens", async () => {
    const adapter = makeCreateIssueAdapter({ token: "tok" });
    await expect(adapter(request({ repo: "not a repo", title: "x" }))).rejects.toThrow(
      /owner\/name/,
    );
    await expect(adapter(request({ repo: "o/r" }))).rejects.toThrow(/title/);
    const prevA = process.env.OPENNEKO_GITHUB_TOKEN;
    const prevB = process.env.GITHUB_TOKEN;
    delete process.env.OPENNEKO_GITHUB_TOKEN;
    delete process.env.GITHUB_TOKEN;
    try {
      const tokenless = makeCreateIssueAdapter();
      await expect(
        tokenless(request({ repo: "o/r", title: "x" })),
      ).rejects.toThrow(/token/);
    } finally {
      if (prevA !== undefined) process.env.OPENNEKO_GITHUB_TOKEN = prevA;
      if (prevB !== undefined) process.env.GITHUB_TOKEN = prevB;
    }
  });
});

describe("code_draft_patch adapter", () => {
  let xdg: string;
  const prev = process.env.XDG_CONFIG_HOME;

  beforeAll(async () => {
    xdg = await mkdtemp(join(tmpdir(), "ol6-"));
    process.env.XDG_CONFIG_HOME = xdg;
  });

  afterAll(async () => {
    if (prev === undefined) delete process.env.XDG_CONFIG_HOME;
    else process.env.XDG_CONFIG_HOME = prev;
    await rm(xdg, { recursive: true, force: true });
  });

  it("writes the patch artifact into the org workspace and never applies it", async () => {
    const outcome = await draftPatchAdapter(
      request(
        {
          title: "Fix renewal handler",
          patch: "--- a/handler.ts\n+++ b/handler.ts\n@@ -1 +1 @@\n-old\n+new",
          summary: "guards the retry loop",
        },
        "code_draft_patch",
      ),
    );
    const path = (outcome.result as { artifactPath: string }).artifactPath;
    expect(path).toContain(join("artifacts", "patches"));
    const written = await readFile(path, "utf8");
    expect(written).toContain("+++ b/handler.ts");
    expect(written).toContain("never applies patches");
  });

  it("refuses an empty patch", async () => {
    await expect(
      draftPatchAdapter(request({ patch: "   " }, "code_draft_patch")),
    ).rejects.toThrow(/unified diff/);
  });
});

import type { ActionAdapter } from "../action-executor";

/**
 * OL6 — conservative code actions. Per the standing decision, no
 * autonomous code writes: `code_create_issue` files an issue on the
 * forge after operator approval, and `code_draft_patch` only persists a
 * patch ARTIFACT into the org workspace — a human applies it.
 */

const GITHUB_API_BASE = "https://api.github.com";
const REPO_RE = /^[\w.-]+\/[\w.-]+$/;

export class CodeActionError extends Error {
  constructor(
    message: string,
    public readonly status: number | null = null,
  ) {
    super(message);
    this.name = "CodeActionError";
  }
}

export type CreateIssuePayload = {
  /** "owner/name" on the configured forge (GitHub v1). */
  repo?: string;
  title?: string;
  body?: string;
  labels?: string[];
};

export type CreateIssueAdapterOptions = {
  fetchImpl?: typeof fetch;
  token?: string;
  baseUrl?: string;
};

export function makeCreateIssueAdapter(
  options: CreateIssueAdapterOptions = {},
): ActionAdapter {
  return async ({ request }) => {
    const payload = (request.payload ?? {}) as CreateIssuePayload;
    const repo = (payload.repo ?? "").trim();
    const title = (payload.title ?? "").trim();
    if (!REPO_RE.test(repo)) {
      throw new CodeActionError(
        'code_create_issue: payload.repo must be "owner/name"',
      );
    }
    if (!title) throw new CodeActionError("code_create_issue: payload.title required");
    const token =
      options.token ??
      process.env.OPENNEKO_GITHUB_TOKEN ??
      process.env.GITHUB_TOKEN;
    if (!token) {
      throw new CodeActionError(
        "code_create_issue: no forge token configured (set OPENNEKO_GITHUB_TOKEN)",
      );
    }
    const doFetch = options.fetchImpl ?? fetch;
    const res = await doFetch(
      `${options.baseUrl ?? GITHUB_API_BASE}/repos/${repo}/issues`,
      {
        method: "POST",
        headers: {
          authorization: `Bearer ${token}`,
          accept: "application/vnd.github+json",
          "content-type": "application/json",
          "user-agent": "openneko",
        },
        body: JSON.stringify({
          title,
          body: payload.body ?? "",
          ...(payload.labels?.length ? { labels: payload.labels } : {}),
        }),
      },
    );
    const data = (await res.json().catch(() => ({}))) as {
      number?: number;
      html_url?: string;
      message?: string;
    };
    if (!res.ok) {
      throw new CodeActionError(
        `code_create_issue: forge returned ${res.status}${data.message ? ` — ${data.message}` : ""}`,
        res.status,
      );
    }
    return {
      commandOrOperation: `create issue ${repo}#${data.number ?? "?"}: ${title}`,
      result: { repo, number: data.number ?? null, url: data.html_url ?? null },
    };
  };
}

export type DraftPatchPayload = {
  title?: string;
  /** Unified diff text the agent drafted. Never applied by OpenNeko. */
  patch?: string;
  summary?: string;
};

/**
 * Persist the agent's drafted patch as a reviewable artifact under the
 * org workspace (artifacts/patches/<requestId>.patch). A human applies
 * it; OpenNeko never writes code anywhere.
 */
export const draftPatchAdapter: ActionAdapter = async ({ request }) => {
  const payload = (request.payload ?? {}) as DraftPatchPayload;
  const patch = payload.patch ?? "";
  if (!patch.trim()) {
    throw new CodeActionError("code_draft_patch: payload.patch (unified diff) required");
  }
  const { getOrgAgentRoot } = await import("../../work/workspace");
  const { mkdir, writeFile } = await import("node:fs/promises");
  const { join } = await import("node:path");
  const dir = join(getOrgAgentRoot(request.orgId), "artifacts", "patches");
  await mkdir(dir, { recursive: true });
  const file = join(dir, `${request.id}.patch`);
  const header = [
    `# ${payload.title ?? "Drafted patch"}`,
    ...(payload.summary ? [`# ${payload.summary}`] : []),
    `# action_request ${request.id} — review and apply manually; OpenNeko never applies patches.`,
    "",
  ].join("\n");
  await writeFile(file, `${header}${patch}\n`, "utf8");
  return {
    commandOrOperation: `draft patch → ${file}`,
    result: { artifactPath: file, bytes: patch.length },
  };
};

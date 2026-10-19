import { randomUUID } from "node:crypto";
import { graphjinQuery } from "./client";
import { graphjinInputWithJsonVariables } from "./config-change";

/** A gj_config rejection because the expected catalog revision is stale. */
export function isCatalogRevisionConflict(messages: Array<string | null | undefined>): boolean {
  return messages.some((message) =>
    Boolean(message?.includes("expected_catalog_revision") && message.includes("does not match")),
  );
}

/** The current catalog revision that a stale-revision rejection names. */
export function currentRevisionFromConflict(messages: Array<string | null | undefined>): string | null {
  for (const message of messages) {
    const match = /current catalog_revision[^a-f0-9]*([a-f0-9]{64})/i.exec(message ?? "");
    if (match?.[1]) return match[1];
  }
  return null;
}

/** gj_config answers with a list of rows; older GraphJins answered with one row. */
function firstRow<T>(value: T | T[] | undefined | null): T | undefined {
  return Array.isArray(value) ? value[0] : (value ?? undefined);
}

type ConfigRow = {
  valid?: boolean;
  applied?: boolean;
  preview_id?: string;
  catalog_revision?: string;
  errors_json?: string;
};

/**
 * Previews then applies one gj_config update, live, on a source-mode GraphJin,
 * and returns the new catalog revision. It retries with the revision that a
 * stale-revision rejection names. Throws with GraphJin's messages when the
 * update is invalid or rejected.
 */
export async function applyGraphjinConfigUpdate(input: {
  endpoint: string;
  headers: Record<string, string>;
  update: Record<string, unknown>;
  attempts?: number;
}): Promise<string | null> {
  const attempts = input.attempts ?? 8;
  let revision: string | null = null;
  for (let attempt = 1; attempt <= attempts; attempt++) {
    if (!revision) {
      // A unique operation name keeps GraphJin's response cache out of the read.
      const name = `ConfigRevision_${randomUUID().replaceAll("-", "")}`;
      const current = await graphjinQuery<{ gj_config?: { catalog_revision?: string } | Array<{ catalog_revision?: string }> }>({
        baseUrl: input.endpoint,
        configurationOnly: true,
        headers: input.headers,
        query: `query ${name} { gj_config(id: "current") { catalog_revision } }`,
      });
      revision = firstRow(current.data?.gj_config)?.catalog_revision ?? null;
      if (!revision) {
        throw new Error(`GraphJin config revision unavailable: ${current.errors?.map((e) => e.message).join("; ") ?? "no revision"}`);
      }
    }
    const run = async (mode: "preview" | "apply", previewId?: string) => {
      const request = graphjinInputWithJsonVariables({
        mode,
        ...(previewId ? { preview_id: previewId } : {}),
        expected_catalog_revision: revision,
        ...input.update,
      });
      const result = await graphjinQuery<{ gj_config?: ConfigRow | ConfigRow[] }>({
        baseUrl: input.endpoint,
        configurationOnly: true,
        longRunning: true,
        headers: input.headers,
        query: `mutation${request.variableDefinitions} { gj_config(id: "current", update: ${request.literal}) { valid applied preview_id catalog_revision errors_json } }`,
        variables: request.variables,
      });
      const row = firstRow(result.data?.gj_config);
      const messages = [...(result.errors?.map((error) => error.message) ?? []), row?.errors_json];
      return { row, messages, failed: Boolean(result.errors?.length) };
    };
    const preview = await run("preview");
    // A valid preview without a preview_id means the update changes nothing.
    if (!preview.failed && preview.row?.valid && !preview.row.preview_id) return preview.row.catalog_revision ?? revision;
    if (preview.failed || !preview.row?.valid || !preview.row.preview_id) {
      if (attempt < attempts && isCatalogRevisionConflict(preview.messages)) {
        revision = currentRevisionFromConflict(preview.messages);
        continue;
      }
      throw new Error(`GraphJin config preview failed: ${preview.messages.filter(Boolean).join("; ") || "invalid preview"}`);
    }
    const applied = await run("apply", preview.row.preview_id);
    if (applied.failed || !applied.row?.applied) {
      if (attempt < attempts && isCatalogRevisionConflict(applied.messages)) {
        revision = currentRevisionFromConflict(applied.messages);
        continue;
      }
      throw new Error(`GraphJin config apply failed: ${applied.messages.filter(Boolean).join("; ") || "apply rejected"}`);
    }
    return applied.row.catalog_revision ?? null;
  }
  throw new Error("GraphJin config revision did not stabilize");
}

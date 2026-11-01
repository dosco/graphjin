import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { loadSolutionPack, type SolutionPackBundle } from "../src/bundle.js";
import { planPack } from "../src/planner.js";

// The packs that replace the Resend, Shopify, Slack and Parallel plugins.
const here = dirname(fileURLToPath(import.meta.url));
const load = (id: string) => loadSolutionPack(resolve(here, "../../../packs", id));

type Spec = { paths: Record<string, Record<string, { operationId: string }>> };
type Source = { name: string; base_url: string; openapi: string; operations?: Record<string, unknown>; auth: Record<string, string> };
type Action = { kind: string; targets?: { fields: string[]; as?: string }; adapter: { source: string; operations: Record<string, { operationId: string; mutationRoot: string }>; lockedBody?: Record<string, { input: string }> } };

const packs = [
  {
    id: "resend", host: "https://api.resend.com", counts: { action: 1, policy: 2, saved_query: 1, skill: 1 },
    reads: ["getEmail", "getTemplate", "listTemplates"], writes: ["sendEmail"],
  },
  {
    id: "shopify", host: "{{shopify.store_url}}", counts: { action: 1, policy: 1, saved_query: 1, skill: 1 },
    reads: ["countOrders", "getOrder", "listOrders"], writes: ["updateOrder"],
  },
  {
    id: "slack", host: "https://slack.com", counts: { action: 1, policy: 2, saved_query: 1, skill: 1 },
    reads: ["getConversation", "getUser", "listConversations", "listUsers", "lookupUserByEmail"], writes: ["addReaction", "postMessage"],
  },
  {
    id: "parallel-search", host: "https://api.parallel.ai", counts: { action: 1, policy: 1, saved_query: 0, skill: 1 },
    reads: [], writes: ["webSearch"],
  },
] as const;

function only<T>(bundle: SolutionPackBundle, kind: string): T[] {
  return bundle.artifacts.filter((artifact) => artifact.kind === kind).map((artifact) => artifact.content as T);
}

describe.each(packs)("$id pack", (pack) => {
  it("loads, validates and plans every artifact", async () => {
    const bundle = await load(pack.id);
    expect(bundle.manifest.metadata).toMatchObject({ id: pack.id, publisher: "openneko" });
    for (const [kind, count] of Object.entries(pack.counts)) {
      expect(bundle.artifacts.filter((artifact) => artifact.kind === kind), kind).toHaveLength(count);
    }
    expect(planPack(bundle, []).entries.every((entry) => entry.action === "create")).toBe(true);
    expect(new Set(bundle.artifacts.map((artifact) => `${artifact.kind}:${artifact.key}`)).size).toBe(bundle.artifacts.length);
    const second = await load(pack.id);
    expect(second.bundleHash).toBe(bundle.bundleHash);
  });

  it("pins one API source to the vendor host with a sealed credential", async () => {
    const bundle = await load(pack.id);
    const sources = only<Source>(bundle, "source");
    expect(sources).toHaveLength(1);
    const [source] = sources;
    expect(source!.base_url).toBe(pack.host);
    const credential = source!.auth.token ?? source!.auth.key_value;
    const secret = /^\{\{secret\.([a-z0-9_.-]+)}}$/.exec(credential ?? "")?.[1];
    expect(secret, "credential must be a secret reference").toBeDefined();
    expect(bundle.manifest.secrets).toEqual([{ key: secret, purpose: "graphjin_api_auth", required: true }]);
    if (pack.host.startsWith("https://")) {
      expect(bundle.manifest.permissions.network).toEqual([new URL(pack.host).hostname]);
    }
  });

  it("declares only the operations it uses, and writes only as reviewed actions", async () => {
    const bundle = await load(pack.id);
    const [source] = only<Source>(bundle, "source");
    const spec = bundle.artifacts.find((artifact) => artifact.kind === "spec" && artifact.path === source!.openapi)!.content as Spec;
    const methods = new Map<string, string>();
    for (const operations of Object.values(spec.paths)) {
      for (const [method, operation] of Object.entries(operations)) methods.set(operation.operationId, method);
    }
    const reads = [...methods].filter(([, method]) => method === "get").map(([id]) => id).sort();
    const writes = [...methods].filter(([, method]) => method !== "get").map(([id]) => id).sort();
    expect(reads).toEqual([...pack.reads]);
    expect(writes).toEqual([...pack.writes]);
    expect(Object.keys(source!.operations ?? {}).sort()).toEqual([...pack.reads]);
    const actions = only<Action>(bundle, "action");
    const actionOperations = actions.flatMap((action) => Object.values(action.adapter.operations).map((operation) => operation.operationId)).sort();
    expect(actionOperations).toEqual([...pack.writes]);
    for (const action of actions) expect(action.adapter.source).toBe(source!.name);
    const policies = only<{ appliesToKinds: string[] }>(bundle, "policy");
    for (const action of actions) expect(policies.some((policy) => policy.appliesToKinds.includes(action.kind))).toBe(true);
  });

  it("locks body fields only to declared inputs", async () => {
    const bundle = await load(pack.id);
    const inputs = new Set(bundle.manifest.inputs.map((input) => input.key));
    for (const action of only<Action>(bundle, "action")) {
      for (const { input } of Object.values(action.adapter.lockedBody ?? {})) expect(inputs.has(input)).toBe(true);
    }
  });
});

describe("Resend pack safety fields", () => {
  it("targets recipient domains, locks the sender and sends an idempotency key", async () => {
    const bundle = await load("resend");
    const [action] = only<Action & { adapter: { operations: Record<string, { idempotencyHeader?: string }> } }>(bundle, "action");
    expect(action!.targets).toEqual({ fields: ["body.to", "body.cc", "body.bcc"], as: "email_domain" });
    expect(action!.adapter.lockedBody).toEqual({ from: { input: "resend.from" } });
    expect(action!.adapter.operations.send_email?.idempotencyHeader).toBe("Idempotency-Key");
    const spec = bundle.artifacts.find((artifact) => artifact.kind === "spec")!.content as {
      paths: Record<string, Record<string, { parameters?: Array<{ name: string; in: string }> }>>;
      components: { schemas: Record<string, { pattern?: string; maxItems?: number }> };
    };
    expect(spec.paths["/emails"]!.post!.parameters).toEqual([expect.objectContaining({ name: "Idempotency-Key", in: "header" })]);
    // One address per item: a comma would hide a second recipient domain.
    const address = new RegExp(spec.components.schemas.Address!.pattern!);
    expect(address.test("Ops <ops@acme.com>")).toBe(true);
    expect(address.test("ops@acme.com")).toBe(true);
    expect(address.test("ops@acme.com, ceo@evil.test")).toBe(false);
    expect(address.test("Ops <ops@acme.com> <ceo@evil.test>")).toBe(false);
    expect(spec.components.schemas.AddressList!.maxItems).toBe(50);
  });

  it("targets Slack destinations by id only", async () => {
    const bundle = await load("slack");
    const [action] = only<Action>(bundle, "action");
    expect(action!.targets).toEqual({ fields: ["body.channel"], as: "value" });
    const spec = bundle.artifacts.find((artifact) => artifact.kind === "spec")!.content as { components: { schemas: Record<string, { pattern?: string }> } };
    const id = new RegExp(spec.components.schemas.ConversationId!.pattern!);
    expect(["C0123ABCD", "U0123ABCD", "D0123ABCD"].every((value) => id.test(value))).toBe(true);
    expect(["#growth", "general", "c0123abcd"].some((value) => id.test(value))).toBe(false);
  });
});

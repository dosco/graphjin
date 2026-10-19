import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { loadSolutionPack } from "@neko/packs";
import { deriveActionTargets, type ActionTargetSpec } from "@neko/llm/workflows";
import { declarativeGraphjinUpdate } from "../src/packs/declarative.js";

const here = dirname(fileURLToPath(import.meta.url));
const load = (id: string) => loadSolutionPack(resolve(here, "../../../packs", id));

// The GraphJin source config the installer generates for the packs that
// replace the Slack, Shopify and Parallel plugins.
describe("integration pack GraphJin config", () => {
  it("Slack: bearer token, id-only targets, two governed mutations", async () => {
    const bundle = await load("slack");
    const update = declarativeGraphjinUpdate(bundle, {}, { "slack.bot_token": "xoxb-test" });
    const [source] = update.update_sources as Array<Record<string, any>>;
    expect(source.specs.slack).toMatchObject({ base_url: "https://slack.com", auth: { scheme: "bearer", token: "xoxb-test" } });
    expect(source.specs.slack.operations.postMessage).toEqual({ expose_mutation: true, allowed_roles: ["pack_api_executor"], expose_as: "slack_post_message" });
    expect(source.specs.slack.operations.addReaction).toEqual({ expose_mutation: true, allowed_roles: ["pack_api_executor"], expose_as: "slack_add_reaction" });
    const action = bundle.artifacts.find((artifact) => artifact.key === "action.slack_message")!.content as { targets: ActionTargetSpec };
    expect(deriveActionTargets(action.targets, { operation: "post_message", body: { channel: "C0123ABCD", text: "hi" } })).toEqual(["C0123ABCD"]);
  });

  it("Shopify: the configured store, an access-token header, and the order update", async () => {
    const bundle = await load("shopify");
    const update = declarativeGraphjinUpdate(bundle, { "shopify.store_url": "https://acme.myshopify.com" }, { "shopify.access_token": "shpat_test" });
    const [source] = update.update_sources as Array<Record<string, any>>;
    expect(source.specs.shopify).toMatchObject({
      base_url: "https://acme.myshopify.com",
      auth: { scheme: "api_key", key_name: "X-Shopify-Access-Token", key_value: "shpat_test", key_in: "header" },
    });
    expect(source.specs.shopify.operations.updateOrder).toEqual({ expose_mutation: true, allowed_roles: ["pack_api_executor"], expose_as: "shopify_update_order" });
    expect(() => declarativeGraphjinUpdate(bundle, {}, { "shopify.access_token": "shpat_test" })).toThrow("shopify.store_url");
    const action = bundle.artifacts.find((artifact) => artifact.key === "action.shopify_order")!.content as { targets: ActionTargetSpec };
    expect(deriveActionTargets(action.targets, { operation: "update_order_note", path: { order_id: 1234567890 }, body: {} })).toEqual(["1234567890"]);
  });

  it("Parallel: an x-api-key header and the search mutation, with no saved queries", async () => {
    const bundle = await load("parallel-search");
    const update = declarativeGraphjinUpdate(bundle, {}, { "parallel-search.api_key": "pk_test" });
    const [source] = update.update_sources as Array<Record<string, any>>;
    expect(source.specs.parallel).toMatchObject({
      base_url: "https://api.parallel.ai",
      auth: { scheme: "api_key", key_name: "x-api-key", key_value: "pk_test", key_in: "header" },
      operations: { webSearch: { expose_mutation: true, allowed_roles: ["pack_api_executor"], expose_as: "parallel_web_search" } },
    });
    expect(update.update_saved_queries).toBeUndefined();
  });
});

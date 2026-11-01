import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { loadSolutionPack } from "@neko/packs";
import { resolveInputs } from "../src/packs/service.js";

const here = dirname(fileURLToPath(import.meta.url));
const packRoot = resolve(here, "../../../packs/magento");

describe("pack API input validation", () => {
  it("validates enum values on the server, not only in the CLI", async () => {
    const bundle = await loadSolutionPack(packRoot);
    expect(() => resolveInputs(bundle, {
      "magento.base_url": "https://store.example.com",
      "database.host": "db.example.com",
      "database.name": "magento",
      "database.port": 3306,
      "database.connectivity_mode": "anything-goes",
    })).toThrow(/database\.connectivity_mode must be one of/);
  });

  it("normalizes valid URL/string inputs and retains an empty table prefix", async () => {
    const bundle = await loadSolutionPack(packRoot);
    expect(resolveInputs(bundle, {
      "magento.base_url": "https://store.example.com/",
      "database.host": " db.example.com ",
      "database.name": "magento",
    })).toMatchObject({
      "magento.base_url": "https://store.example.com",
      "magento.table_prefix": "",
      "database.host": "db.example.com",
      "database.port": 3306,
    });
  });
});

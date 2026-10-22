/**
 * local-config.ts: ~/.config/openneko/config.json reader/writer.
 * Pure FS, no DB — runs in any environment. Uses a temp $HOME so we
 * don't touch the developer's real file.
 */

import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  hasCustomPassword,
  localConfigPath,
  readLocalConfig,
  writeLocalConfig,
} from "../src/local-config";

describe("local-config (~/.config/openneko/config.json)", () => {
  let tempHome: string;
  const ORIGINAL_HOME = process.env.HOME;
  const ORIGINAL_XDG = process.env.XDG_CONFIG_HOME;

  beforeEach(async () => {
    tempHome = await mkdtemp(join(tmpdir(), "neko-localcfg-test-"));
    process.env.HOME = tempHome;
    delete process.env.XDG_CONFIG_HOME;
  });

  afterEach(async () => {
    if (ORIGINAL_HOME) process.env.HOME = ORIGINAL_HOME;
    if (ORIGINAL_XDG === undefined) delete process.env.XDG_CONFIG_HOME;
    else process.env.XDG_CONFIG_HOME = ORIGINAL_XDG;
    await rm(tempHome, { recursive: true, force: true });
  });

  it("path is rooted at ~/.config/openneko (XDG default)", () => {
    expect(localConfigPath()).toBe(join(tempHome, ".config", "openneko", "config.json"));
  });

  it("respects XDG_CONFIG_HOME when set", () => {
    process.env.XDG_CONFIG_HOME = join(tempHome, "custom-xdg");
    expect(localConfigPath()).toBe(join(tempHome, "custom-xdg", "openneko", "config.json"));
  });

  it("readLocalConfig returns empty object when the file doesn't exist", () => {
    expect(readLocalConfig()).toEqual({});
  });

  it("writeLocalConfig + readLocalConfig round-trip", () => {
    writeLocalConfig({ pg: { password: "new-pw", host: "db.example.com" } });
    const cfg = readLocalConfig();
    expect(cfg.pg?.password).toBe("new-pw");
    expect(cfg.pg?.host).toBe("db.example.com");
  });

  it("writeLocalConfig deep-merges into existing pg config", () => {
    writeLocalConfig({ pg: { password: "first", host: "h1" } });
    writeLocalConfig({ pg: { password: "second" } });
    const cfg = readLocalConfig();
    // host preserved from first write; password updated by second.
    expect(cfg.pg).toEqual({ password: "second", host: "h1" });
  });

  it("writeLocalConfig produces a JSON file with the password encrypted at rest", async () => {
    writeLocalConfig({ pg: { password: "p" } });
    const raw = await readFile(localConfigPath(), "utf8");
    const parsed = JSON.parse(raw);
    expect(parsed.pg.password).toMatch(/^enc:v1:/);
    expect(readLocalConfig().pg?.password).toBe("p");
  });

  it("readLocalConfig tolerates malformed JSON (returns empty)", async () => {
    writeLocalConfig({ pg: { password: "p" } });
    const { writeFileSync } = await import("node:fs");
    writeFileSync(localConfigPath(), "not-valid-json", "utf8");
    expect(readLocalConfig()).toEqual({});
  });

  it("hasCustomPassword ignores the bootstrap password", () => {
    expect(hasCustomPassword()).toBe(false);
    writeLocalConfig({ pg: { password: "secret" } });
    expect(hasCustomPassword()).toBe(false);
    writeLocalConfig({ pg: { password: "p" } });
    expect(hasCustomPassword()).toBe(true);
  });
});

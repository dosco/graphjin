import { mkdtemp, readFile, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  applyEditedGraphjinConfig,
  configHash,
  GraphjinConfigEditorError,
  HIDDEN_VALUE,
  isSecretKey,
  maskGraphjinConfig,
  unmaskGraphjinConfig,
} from "../src/graphjin/config-editor";

const RAW = `# OpenNeko GraphJin config
auth:
  type: jwt
  jwt:
    secret: "jwt-signing-secret-value"
secrets:
  keystore:
    key: keystore-key-value-123
    previous_keys:
      - old-key-one-123
agent:
  api_key_env: GRAPHJIN_AGENT_API_KEY
sources:
  - name: shop
    password: shop-password-123
    access:
      read: authenticated
  - name: crm
    password: gjsecret://crm/password
`;

describe("GraphJin config editor", () => {
  it("hides secret values and keeps references, env names and comments", () => {
    const { text, hidden } = maskGraphjinConfig(RAW);
    expect(hidden).toBe(4);
    for (const secret of ["jwt-signing-secret-value", "keystore-key-value-123", "old-key-one-123", "shop-password-123"]) {
      expect(text).not.toContain(secret);
    }
    expect(text).toContain("gjsecret://crm/password");
    expect(text).toContain("api_key_env: GRAPHJIN_AGENT_API_KEY");
    expect(text).toContain("# OpenNeko GraphJin config");
    expect(isSecretKey("api_key_env")).toBe(false);
    expect(isSecretKey("webhook_secret")).toBe(true);
  });

  it("puts hidden values back, following named list entries when they move", () => {
    const masked = maskGraphjinConfig(RAW).text;
    // Reorder the sources and change an unrelated setting.
    const edited = masked
      .replace("  - name: shop", "  - name: crm\n    password: gjsecret://crm/password\n  - name: shop")
      .replace(/\n  - name: crm\n    password: gjsecret:\/\/crm\/password\n$/, "\n")
      .replace("read: authenticated", "read: admin");
    const restored = unmaskGraphjinConfig(RAW, edited);
    expect(restored).toContain("password: shop-password-123");
    expect(restored).toContain("read: admin");
    expect(restored).toContain('secret: "jwt-signing-secret-value"');
    expect(restored).not.toContain(HIDDEN_VALUE);
    expect(restored.indexOf("name: crm")).toBeLessThan(restored.indexOf("name: shop"));
  });

  it("keeps a new value typed over a hidden one", () => {
    const edited = maskGraphjinConfig(RAW).text.replace(`password: ${HIDDEN_VALUE}`, "password: new-shop-password");
    expect(unmaskGraphjinConfig(RAW, edited)).toContain("password: new-shop-password");
  });

  it("refuses a hidden value with no saved value behind it", () => {
    const edited = `${maskGraphjinConfig(RAW).text}  - name: billing\n    password: ${HIDDEN_VALUE}\n`;
    expect(() => unmaskGraphjinConfig(RAW, edited)).toThrow(/sources\.\[name=billing\]\.password/);
    const misplaced = maskGraphjinConfig(RAW).text.replace("type: jwt", `type: ${HIDDEN_VALUE}`);
    expect(() => unmaskGraphjinConfig(RAW, misplaced)).toThrow(GraphjinConfigEditorError);
  });

  it("refuses YAML that is not a mapping", () => {
    expect(() => unmaskGraphjinConfig(RAW, "sources: [")).toThrow(/not valid YAML/);
    expect(() => unmaskGraphjinConfig(RAW, "- a\n- b\n")).toThrow(/mapping/);
  });

  it("applies live before writing the file, refuses stale edits and keeps secrets out of errors", async () => {
    const dir = await mkdtemp(join(tmpdir(), "gj-editor-"));
    const configFile = join(dir, "agentic.yml");
    await writeFile(configFile, RAW, { mode: 0o640 });
    const applied: string[] = [];
    const connection = { endpoint: "http://graphjin.invalid", headers: {} };
    const edited = maskGraphjinConfig(RAW).text.replace("read: authenticated", "read: admin");

    await expect(
      applyEditedGraphjinConfig({ configFile, edited, baseHash: "stale", connection, apply: async () => {} }),
    ).rejects.toMatchObject({ status: 409 });

    const { hash } = await applyEditedGraphjinConfig({
      configFile,
      edited,
      baseHash: configHash(RAW),
      connection,
      apply: async (update) => {
        applied.push(String(update.config_yaml));
        expect(await readFile(configFile, "utf8")).toBe(RAW); // file written only after GraphJin took it
      },
    });
    const written = await readFile(configFile, "utf8");
    expect(applied).toEqual([written]);
    expect(written).toContain("read: admin");
    expect(written).toContain("password: shop-password-123");
    expect(hash).toBe(configHash(written));
    expect((await stat(configFile)).mode & 0o777).toBe(0o640);

    const rejected = applyEditedGraphjinConfig({
      configFile,
      edited: maskGraphjinConfig(written).text.replace("read: admin", "read: nobody"),
      baseHash: hash,
      connection,
      apply: async () => {
        throw new Error("source shop with password shop-password-123 is invalid");
      },
    });
    await expect(rejected).rejects.toMatchObject({ status: 422 });
    await expect(rejected).rejects.toThrow(`source shop with password ${HIDDEN_VALUE} is invalid`);
    expect(await readFile(configFile, "utf8")).toBe(written);
  });
});

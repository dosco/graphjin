import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  allSecretValues,
  allSecretValuesFull,
  defaultSecretsPath,
  getOperatorCredential,
  isValidEnvKey,
  isValidOperatorId,
  listConnectorsForOperator,
  listKeysForSection,
  listOperatorsForConnector,
  OPERATORS_KEY,
  readFullSecretsFile,
  readFullSecretsFileSoft,
  readSecretsStore,
  readSecretsStoreSoft,
  setOperatorCredential,
  setSecret,
  unsetOperatorCredential,
  unsetSecret,
  writeFullSecretsFile,
  writeSecretsStore,
  type ConnectorCredential,
  type FullSecretsFile,
  type SecretsStore,
} from "../src/secrets-store";

describe("isValidEnvKey", () => {
  it("accepts UPPER_SNAKE_CASE", () => {
    expect(isValidEnvKey("SLACK_BOT_TOKEN")).toBe(true);
    expect(isValidEnvKey("X")).toBe(true);
  });
  it("rejects bad inputs", () => {
    expect(isValidEnvKey("slack")).toBe(false);
    expect(isValidEnvKey("1KEY")).toBe(false);
    expect(isValidEnvKey("FOO; rm")).toBe(false);
  });
});

describe("setSecret / unsetSecret / listKeysForSection", () => {
  it("setSecret writes a new value", () => {
    expect(setSecret({}, "@x/y", "K", "v")["@x/y"]?.K).toBe("v");
  });
  it("setSecret refuses bad keys", () => {
    expect(() => setSecret({}, "@x/y", "bad-key", "v")).toThrow(/UPPER_SNAKE_CASE/);
  });
  it("setSecret overwrites existing keys", () => {
    let s: SecretsStore = setSecret({}, "@x/y", "K", "v1");
    s = setSecret(s, "@x/y", "K", "v2");
    expect(s["@x/y"]?.K).toBe("v2");
  });
  it("unsetSecret removes a key + reports removal", () => {
    const s = setSecret({}, "@x/y", "K", "v");
    const { store, removed } = unsetSecret(s, "@x/y", "K");
    expect(removed).toBe(true);
    expect(store).toEqual({});
  });
  it("unsetSecret drops the section when no keys remain", () => {
    let s: SecretsStore = setSecret({}, "@x/y", "A", "1");
    s = setSecret(s, "@x/y", "B", "2");
    const { store } = unsetSecret(s, "@x/y", "A");
    expect(store["@x/y"]).toEqual({ B: "2" });
    const after = unsetSecret(store, "@x/y", "B");
    expect(after.store).toEqual({});
  });
  it("unsetSecret no-ops for missing keys", () => {
    expect(unsetSecret({}, "@x/y", "K").removed).toBe(false);
  });
  it("listKeysForSection returns sorted keys", () => {
    let s: SecretsStore = {};
    s = setSecret(s, "@x/y", "BANANA", "1");
    s = setSecret(s, "@x/y", "APPLE", "2");
    expect(listKeysForSection(s, "@x/y")).toEqual(["APPLE", "BANANA"]);
  });
});

describe("read/writeSecretsStore", () => {
  let dir: string;
  beforeEach(async () => {
    dir = await mkdtemp(path.join(tmpdir(), "pi-secrets-"));
  });
  afterEach(async () => {
    await rm(dir, { recursive: true, force: true });
  });

  it("readSecretsStore returns empty when file missing", async () => {
    expect(await readSecretsStore(dir)).toEqual({});
  });

  it("writeSecretsStore creates the file with 0600 perms", async () => {
    await writeSecretsStore({ "@x/y": { K: "v" } }, dir);
    const file = defaultSecretsPath(dir);
    const s = await stat(file);
    if (process.platform !== "win32") {
      expect(s.mode & 0o777).toBe(0o600);
    }
    expect((await readSecretsStore(dir))["@x/y"]?.K).toBe("v");
  });

  it("writeSecretsStore sorts sections + keys deterministically", async () => {
    await writeSecretsStore(
      {
        "@b/p": { Z: "1", A: "2" },
        "@a/p": { B: "1", A: "2" },
      },
      dir,
    );
    const raw = await readFile(defaultSecretsPath(dir), "utf8");
    expect(raw.indexOf("@a/p")).toBeLessThan(raw.indexOf("@b/p"));
    const aBlock = raw.slice(raw.indexOf("@a/p"), raw.indexOf("@b/p"));
    expect(aBlock.indexOf("\"A\"")).toBeLessThan(aBlock.indexOf("\"B\""));
  });

  it("readSecretsStore throws on invalid JSON", async () => {
    await writeFile(defaultSecretsPath(dir), "not json", "utf8");
    await expect(readSecretsStore(dir)).rejects.toThrow(/invalid JSON/);
  });

  it("readSecretsStoreSoft returns empty + warns on invalid JSON", async () => {
    await writeFile(defaultSecretsPath(dir), "not json", "utf8");
    const warnings: string[] = [];
    const out = await readSecretsStoreSoft(dir, (m) => warnings.push(m));
    expect(out).toEqual({});
    expect(warnings.join("\n")).toMatch(/invalid JSON/);
  });

  it("readSecretsStore tolerates malformed sections", async () => {
    await writeFile(
      defaultSecretsPath(dir),
      JSON.stringify({
        "@good/p": { K: "v" },
        "@bad/p": 42,
        "@other/p": { K: 42, GOOD: "yes" },
      }),
      "utf8",
    );
    const back = await readSecretsStore(dir);
    expect(back["@good/p"]?.K).toBe("v");
    expect(back["@bad/p"]).toBeUndefined();
    expect(back["@other/p"]).toEqual({ GOOD: "yes" });
  });
});

describe("allSecretValues", () => {
  it("returns the union of every value, deduplicated", () => {
    const store = {
      "@x/a": { K: "alpha", L: "beta" },
      "@x/b": { K: "alpha", M: "gamma" },
    };
    const values = allSecretValues(store);
    expect(values.sort()).toEqual(["alpha", "beta", "gamma"]);
  });
  it("ignores empty strings", () => {
    expect(allSecretValues({ "@x/y": { K: "" } })).toEqual([]);
  });
});

describe("defaultSecretsPath", () => {
  it("returns the secrets.json under the provided dir", () => {
    expect(defaultSecretsPath("/tmp/x")).toBe("/tmp/x/secrets.json");
  });
});

// ─── Per-operator credentials ─────────────────────────────────────────

const sampleCredential = (overrides: Partial<ConnectorCredential> = {}): ConnectorCredential => ({
  tokens: { access_token: "at-123", refresh_token: "rt-456" },
  scopes: ["gmail.readonly", "calendar.readonly"],
  providerLabel: "Google Workspace",
  connectedAt: "2026-05-21T10:00:00Z",
  ...overrides,
});

describe("isValidOperatorId", () => {
  it("accepts alphanumeric with _ and -", () => {
    expect(isValidOperatorId("op1")).toBe(true);
    expect(isValidOperatorId("op-1_x")).toBe(true);
    expect(isValidOperatorId("a")).toBe(true);
  });
  it("rejects bad inputs", () => {
    expect(isValidOperatorId("")).toBe(false);
    expect(isValidOperatorId("-leading-dash")).toBe(false);
    expect(isValidOperatorId("contains spaces")).toBe(false);
    expect(isValidOperatorId("a".repeat(129))).toBe(false);
  });
});

describe("setOperatorCredential / get / unset", () => {
  const empty: FullSecretsFile = { env: {}, operators: {} };
  it("setOperatorCredential writes a new credential", () => {
    const next = setOperatorCredential(empty, "op1", "@x/y", sampleCredential());
    expect(getOperatorCredential(next, "op1", "@x/y")?.connectedAt).toBe(
      "2026-05-21T10:00:00Z",
    );
  });
  it("setOperatorCredential refuses bad operator ids", () => {
    expect(() =>
      setOperatorCredential(empty, "bad id", "@x/y", sampleCredential()),
    ).toThrow(/operator id/);
  });
  it("setOperatorCredential overwrites prior credentials (refresh path)", () => {
    let s = setOperatorCredential(empty, "op1", "@x/y", sampleCredential());
    s = setOperatorCredential(
      s,
      "op1",
      "@x/y",
      sampleCredential({
        tokens: { access_token: "at-rotated" },
        refreshedAt: "2026-05-21T11:00:00Z",
      }),
    );
    const cred = getOperatorCredential(s, "op1", "@x/y");
    expect(cred?.tokens.access_token).toBe("at-rotated");
    expect(cred?.refreshedAt).toBe("2026-05-21T11:00:00Z");
  });
  it("unsetOperatorCredential removes a credential + reports removal", () => {
    const s = setOperatorCredential(empty, "op1", "@x/y", sampleCredential());
    const { store, removed } = unsetOperatorCredential(s, "op1", "@x/y");
    expect(removed).toBe(true);
    expect(getOperatorCredential(store, "op1", "@x/y")).toBeNull();
  });
  it("unsetOperatorCredential drops the operator entry when empty", () => {
    let s = setOperatorCredential(empty, "op1", "@x/a", sampleCredential());
    s = setOperatorCredential(s, "op1", "@x/b", sampleCredential());
    let after = unsetOperatorCredential(s, "op1", "@x/a");
    expect(after.store.operators.op1).toEqual({ "@x/b": sampleCredential() });
    after = unsetOperatorCredential(after.store, "op1", "@x/b");
    expect(after.store.operators).toEqual({});
  });
  it("operators are independent", () => {
    let s = setOperatorCredential(empty, "op1", "@x/y", sampleCredential());
    s = setOperatorCredential(s, "op2", "@x/y", sampleCredential({ connectedAt: "2026-06-01T00:00:00Z" }));
    expect(getOperatorCredential(s, "op1", "@x/y")?.connectedAt).toBe("2026-05-21T10:00:00Z");
    expect(getOperatorCredential(s, "op2", "@x/y")?.connectedAt).toBe("2026-06-01T00:00:00Z");
    const after = unsetOperatorCredential(s, "op1", "@x/y");
    expect(getOperatorCredential(after.store, "op2", "@x/y")).not.toBeNull();
  });
});

describe("listConnectorsForOperator / listOperatorsForConnector", () => {
  let full: FullSecretsFile = { env: {}, operators: {} };
  full = setOperatorCredential(full, "op1", "@x/a", sampleCredential());
  full = setOperatorCredential(full, "op1", "@x/b", sampleCredential());
  full = setOperatorCredential(full, "op2", "@x/a", sampleCredential());
  it("listConnectorsForOperator returns sorted connector names", () => {
    expect(listConnectorsForOperator(full, "op1")).toEqual(["@x/a", "@x/b"]);
  });
  it("listConnectorsForOperator returns [] for unknown operator", () => {
    expect(listConnectorsForOperator(full, "nobody")).toEqual([]);
  });
  it("listOperatorsForConnector returns sorted operator ids", () => {
    expect(listOperatorsForConnector(full, "@x/a")).toEqual(["op1", "op2"]);
    expect(listOperatorsForConnector(full, "@x/b")).toEqual(["op1"]);
    expect(listOperatorsForConnector(full, "@x/missing")).toEqual([]);
  });
});

describe("readFullSecretsFile / writeFullSecretsFile", () => {
  let dir: string;
  beforeEach(async () => {
    dir = await mkdtemp(path.join(tmpdir(), "pi-secrets-full-"));
  });
  afterEach(async () => {
    await rm(dir, { recursive: true, force: true });
  });

  it("readFullSecretsFile returns empty when file missing", async () => {
    expect(await readFullSecretsFile(dir)).toEqual({ env: {}, operators: {} });
  });

  it("round-trips an env-only file (backwards compatibility)", async () => {
    await writeSecretsStore({ "@x/y": { K: "v" } }, dir);
    const back = await readFullSecretsFile(dir);
    expect(back.env["@x/y"]?.K).toBe("v");
    expect(back.operators).toEqual({});
    const raw = await readFile(defaultSecretsPath(dir), "utf8");
    expect(raw).not.toContain(OPERATORS_KEY);
  });

  it("round-trips both env and operator sections", async () => {
    const full: FullSecretsFile = {
      env: { "@x/y": { K: "v" } },
      operators: { op1: { "@x/y": sampleCredential() } },
    };
    await writeFullSecretsFile(full, dir);
    const back = await readFullSecretsFile(dir);
    expect(back.env["@x/y"]?.K).toBe("v");
    expect(back.operators.op1?.["@x/y"]?.tokens.access_token).toBe("at-123");
  });

  it("writeSecretsStore preserves the operator section already on disk", async () => {
    const full: FullSecretsFile = {
      env: {},
      operators: { op1: { "@x/y": sampleCredential() } },
    };
    await writeFullSecretsFile(full, dir);
    // Now mutate env via the legacy entry point.
    await writeSecretsStore({ "@x/y": { K: "v" } }, dir);
    const back = await readFullSecretsFile(dir);
    expect(back.env["@x/y"]?.K).toBe("v");
    expect(back.operators.op1?.["@x/y"]).toBeTruthy();
  });

  it("writeFullSecretsFile creates the file with 0600 perms", async () => {
    await writeFullSecretsFile(
      { env: {}, operators: { op1: { "@x/y": sampleCredential() } } },
      dir,
    );
    const s = await stat(defaultSecretsPath(dir));
    if (process.platform !== "win32") {
      expect(s.mode & 0o777).toBe(0o600);
    }
  });

  it("sorts operators + connectors deterministically", async () => {
    await writeFullSecretsFile(
      {
        env: {},
        operators: {
          opZ: { "@b/y": sampleCredential(), "@a/y": sampleCredential() },
          opA: { "@a/y": sampleCredential() },
        },
      },
      dir,
    );
    const raw = await readFile(defaultSecretsPath(dir), "utf8");
    expect(raw.indexOf("\"opA\"")).toBeLessThan(raw.indexOf("\"opZ\""));
    const zBlock = raw.slice(raw.indexOf("opZ"));
    expect(zBlock.indexOf("@a/y")).toBeLessThan(zBlock.indexOf("@b/y"));
  });

  it("tolerates malformed operator entries", async () => {
    await writeFile(
      defaultSecretsPath(dir),
      JSON.stringify({
        "@good/p": { K: "v" },
        _operators: {
          good: { "@x/y": sampleCredential() },
          notObject: 42,
          missingFields: { "@x/y": { tokens: "not-an-object" } },
          partial: { "@x/y": { tokens: { a: 1 } } /* no connectedAt */ },
        },
      }),
      "utf8",
    );
    const back = await readFullSecretsFile(dir);
    expect(back.env["@good/p"]?.K).toBe("v");
    expect(back.operators.good?.["@x/y"]).toBeTruthy();
    expect(back.operators.notObject).toBeUndefined();
    expect(back.operators.missingFields).toBeUndefined();
    expect(back.operators.partial).toBeUndefined();
  });

  it("readSecretsStore is unchanged by the operator section", async () => {
    await writeFullSecretsFile(
      {
        env: { "@x/y": { K: "v" } },
        operators: { op1: { "@x/y": sampleCredential() } },
      },
      dir,
    );
    const env = await readSecretsStore(dir);
    expect(env).toEqual({ "@x/y": { K: "v" } });
  });

  it("readFullSecretsFileSoft returns empty + warns on invalid JSON", async () => {
    await writeFile(defaultSecretsPath(dir), "not json", "utf8");
    const warnings: string[] = [];
    const out = await readFullSecretsFileSoft(dir, (m) => warnings.push(m));
    expect(out).toEqual({ env: {}, operators: {} });
    expect(warnings.join("\n")).toMatch(/invalid JSON/);
  });
});

describe("allSecretValuesFull", () => {
  it("collects values from both env and operator credentials", () => {
    const full: FullSecretsFile = {
      env: { "@x/y": { K: "envval" } },
      operators: {
        op1: {
          "@x/y": {
            tokens: { access_token: "at-xyz", refresh_token: "rt-xyz" },
            connectedAt: "2026-05-21T10:00:00Z",
            scopes: ["gmail.readonly"],
          },
        },
      },
    };
    const values = allSecretValuesFull(full);
    expect(values.sort()).toEqual(["at-xyz", "envval", "rt-xyz"]);
  });
  it("walks nested objects + arrays inside token blobs", () => {
    const full: FullSecretsFile = {
      env: {},
      operators: {
        op1: {
          "@x/y": {
            tokens: {
              nested: { deep: "secret-deep" },
              arr: ["s1", "s2"],
              n: 42,
            },
            connectedAt: "2026-05-21T10:00:00Z",
          },
        },
      },
    };
    const values = allSecretValuesFull(full);
    expect(values.sort()).toEqual(["s1", "s2", "secret-deep"]);
  });
});

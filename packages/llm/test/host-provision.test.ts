import {
  mkdtemp,
  readFile,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir, platform } from "node:os";
import { join } from "node:path";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import {
  clearProvider,
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  seedDataSource,
  seedProvider,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import {
  and,
  data_source,
  db,
  eq,
  llm_provider_config,
  pool,
} from "@neko/db";
import { createServer } from "node:net";
import {
  ensureHostConfigProvisioned,
  loadAgentModelKeys,
  patchGraphjinSourcesJwtSecret,
  provisionHostConfig,
  shouldReconcileDemoSourceAuthMode,
  verifyAgentRuntimeReady,
} from "../src/host-provision";
import { graphjinSigningSecretB64 } from "../src/graphjin/token";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn(
    "[host-provision] skipping: metadata Postgres unreachable. Run `docker compose up -d`.",
  );
}

function graphjinPath(home: string): string {
  const xdg = process.env.XDG_CONFIG_HOME?.trim();
  if (xdg) {
    return join(xdg, "graphjin", "client.json");
  }
  if (platform() === "darwin") {
    return join(home, "Library", "Application Support", "graphjin", "client.json");
  }
  return join(home, ".config", "graphjin", "client.json");
}

const ORIGINAL_HOME = process.env.HOME;
const ORIGINAL_XDG_CONFIG_HOME = process.env.XDG_CONFIG_HOME;

describe("patchGraphjinSourcesJwtSecret", () => {
  it("replaces a source-mode placeholder", () => {
    const raw = `auth:
  type: jwt
  jwt:
    secret: "REPLACE_WITH_PER_ORG_SECRET_B64"
`;

    expect(patchGraphjinSourcesJwtSecret(raw, "new-secret")).toEqual({
      changed: true,
      content: `auth:
  type: jwt
  jwt:
    secret: "new-secret"
`,
    });
  });

  it("repairs a stale concrete source-mode secret", () => {
    const raw = `auth:
  type: jwt
  jwt:
    # base64 of graphjinSigningSecretB64(orgId)
    secret: "old-secret"
    audience: ""
`;

    expect(patchGraphjinSourcesJwtSecret(raw, "new-secret")).toEqual({
      changed: true,
      content: `auth:
  type: jwt
  jwt:
    # base64 of graphjinSigningSecretB64(orgId)
    secret: "new-secret"
    audience: ""
`,
    });
  });

  it("leaves matching source-mode secrets untouched", () => {
    const raw = `auth:
  type: jwt
  jwt:
    secret: "same-secret"
`;

    expect(patchGraphjinSourcesJwtSecret(raw, "same-secret")).toEqual({
      changed: false,
      content: raw,
    });
  });
});

describe("shouldReconcileDemoSourceAuthMode", () => {
  const jwtConfig = `auth:
  type: jwt
  jwt:
    secret: "demo-secret"
`;

  it("recognizes the mount used by older packaged demo compose files", () => {
    expect(
      shouldReconcileDemoSourceAuthMode(
        "/graphjin-config/agentic.yml",
        jwtConfig,
        "",
      ),
    ).toBe(true);
  });

  it("recognizes an explicitly marked demo with a custom config path", () => {
    expect(
      shouldReconcileDemoSourceAuthMode("/custom/agentic.yml", jwtConfig, "demo"),
    ).toBe(true);
  });

  it("does not reconcile the production GraphJin config", () => {
    expect(
      shouldReconcileDemoSourceAuthMode(
        "/config/graphjin/agentic.yml",
        jwtConfig,
        "",
      ),
    ).toBe(false);
  });

  it("does not trust a demo config that is not JWT-enabled", () => {
    expect(
      shouldReconcileDemoSourceAuthMode(
        "/graphjin-config/agentic.yml",
        "auth:\n  type: none\n",
        "",
      ),
    ).toBe(false);
  });
});

describeIfDb("provisionHostConfig", () => {
  let orgId: string;
  let tempHome: string;

  beforeAll(async () => {
    orgId = uniqueOrgId("provision");
    await createTestOrg(orgId);
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
  });

  beforeEach(async () => {
    tempHome = await mkdtemp(join(tmpdir(), "neko-test-home-"));
    process.env.HOME = tempHome;
    delete process.env.XDG_CONFIG_HOME;
  });

  afterEach(async () => {
    await rm(tempHome, { recursive: true, force: true });
    if (ORIGINAL_HOME) process.env.HOME = ORIGINAL_HOME;
    if (ORIGINAL_XDG_CONFIG_HOME) process.env.XDG_CONFIG_HOME = ORIGINAL_XDG_CONFIG_HOME;
    else delete process.env.XDG_CONFIG_HOME;
    await clearProvider(orgId, "agent");
    await clearProvider(orgId, "primary");
    await db().delete(data_source).where(eq(data_source.org_id, orgId));
  });

  it("writes graphjin client.json with the server base derived from graphql_url", async () => {
    await seedDataSource(orgId, {
      graphqlUrl: "http://localhost:8080/api/v1/graphql",
    });
    await provisionHostConfig(orgId);

    const path = graphjinPath(tempHome);
    const content = JSON.parse(await readFile(path, "utf8"));
    expect(content.server).toBe("http://localhost:8080");
    expect(content.token).toBe("");
    expect(content.expires_at).toBe("0001-01-01T00:00:00Z");
  });

  it("writes graphjin client.json under XDG_CONFIG_HOME when it is set", async () => {
    process.env.XDG_CONFIG_HOME = join(tempHome, "xdg-config");
    await seedDataSource(orgId, {
      graphqlUrl: "http://openneko-serve:8080/api/v1/graphql",
    });

    await provisionHostConfig(orgId);

    const path = graphjinPath(tempHome);
    const content = JSON.parse(await readFile(path, "utf8"));
    expect(path).toBe(join(tempHome, "xdg-config", "graphjin", "client.json"));
    expect(content.server).toBe("http://openneko-serve:8080");
  });

  it("falls back to URL origin when graphql path doesn't match the conventional suffix", async () => {
    await seedDataSource(orgId, {
      graphqlUrl: "https://api.example.com/custom-path",
    });
    await provisionHostConfig(orgId);

    const path = graphjinPath(tempHome);
    const content = JSON.parse(await readFile(path, "utf8"));
    expect(content.server).toBe("https://api.example.com");
  });

  it("loads the model key for a run, never into a host file or the launch config", async () => {
    await seedDataSource(orgId);
    await seedProvider(orgId, {
      scope: "primary",
      provider: "anthropic",
      model: "claude-opus-4-7",
      secrets: { apiKey: "sk-ant-test" },
    });

    await expect(provisionHostConfig(orgId)).resolves.toEqual({});
    await expect(loadAgentModelKeys(orgId)).resolves.toEqual({
      MODEL_API_KEY: "sk-ant-test",
      ANTHROPIC_API_KEY: "sk-ant-test",
    });

    const gj = await readFile(graphjinPath(tempHome), "utf8");
    expect(gj).toContain("server");
    expect(gj).not.toContain("sk-ant-test");
  });

  it("does not throw when primary row is missing (best-effort)", async () => {
    await seedDataSource(orgId);
    await seedProvider(orgId, {
      scope: "agent",
      provider: "ax",
      config: {},
    });
    await expect(provisionHostConfig(orgId)).resolves.toEqual({});
    await expect(loadAgentModelKeys(orgId)).resolves.toEqual({});
    await readFile(graphjinPath(tempHome), "utf8");
  });

  it("does not throw when no data source exists (graphjin write skipped)", async () => {
    await expect(provisionHostConfig(orgId)).resolves.toEqual({});
  });

  it("preserves an explicitly configured development-header auth mode", async () => {
    await seedDataSource(orgId);
    await db()
      .update(data_source)
      .set({ auth_mode: "development" })
      .where(eq(data_source.org_id, orgId));

    await provisionHostConfig(orgId);

    const [source] = await db()
      .select({ authMode: data_source.auth_mode })
      .from(data_source)
      .where(eq(data_source.org_id, orgId));
    expect(source?.authMode).toBe("development");
  });

  it("repairs an existing packaged-demo AdventureWorks source deterministically", async () => {
    const configPath = join(tempHome, "agentic.yml");
    await writeFile(
      configPath,
      `auth:
  type: jwt
  jwt:
    secret: "${graphjinSigningSecretB64(orgId)}"
`,
      "utf8",
    );
    await seedDataSource(orgId, { label: "AdventureWorks" });

    const previousPath = process.env.OPENNEKO_GRAPHJIN_CONFIG;
    const previousMode = process.env.OPENNEKO_STACK_MODE;
    process.env.OPENNEKO_GRAPHJIN_CONFIG = configPath;
    process.env.OPENNEKO_STACK_MODE = "demo";
    try {
      await provisionHostConfig(orgId);
    } finally {
      if (previousPath === undefined) delete process.env.OPENNEKO_GRAPHJIN_CONFIG;
      else process.env.OPENNEKO_GRAPHJIN_CONFIG = previousPath;
      if (previousMode === undefined) delete process.env.OPENNEKO_STACK_MODE;
      else process.env.OPENNEKO_STACK_MODE = previousMode;
    }

    const [source] = await db()
      .select({ authMode: data_source.auth_mode })
      .from(data_source)
      .where(eq(data_source.org_id, orgId));
    expect(source?.authMode).toBe("jwt");
  });

  it("still reconciles the demo source after the wizard renamed its label", async () => {
    const configPath = join(tempHome, "agentic.yml");
    await writeFile(
      configPath,
      `auth:
  type: jwt
  jwt:
    secret: "${graphjinSigningSecretB64(orgId)}"
`,
      "utf8",
    );
    // The setup wizard saves the data source with label "primary", renaming
    // the seeded AdventureWorks row in place. The in-stack GraphJin URL is
    // the stable identity the reconcile must key on.
    await seedDataSource(orgId, {
      label: "primary",
      graphqlUrl: "http://openneko-serve:8080/api/v1/graphql",
    });

    const previousPath = process.env.OPENNEKO_GRAPHJIN_CONFIG;
    const previousMode = process.env.OPENNEKO_STACK_MODE;
    process.env.OPENNEKO_GRAPHJIN_CONFIG = configPath;
    process.env.OPENNEKO_STACK_MODE = "demo";
    try {
      await provisionHostConfig(orgId);
    } finally {
      if (previousPath === undefined) delete process.env.OPENNEKO_GRAPHJIN_CONFIG;
      else process.env.OPENNEKO_GRAPHJIN_CONFIG = previousPath;
      if (previousMode === undefined) delete process.env.OPENNEKO_STACK_MODE;
      else process.env.OPENNEKO_STACK_MODE = previousMode;
    }

    const [source] = await db()
      .select({ authMode: data_source.auth_mode })
      .from(data_source)
      .where(eq(data_source.org_id, orgId));
    expect(source?.authMode).toBe("jwt");
  });

  it("loads the current key under the current provider's variable after a switch or rotation", async () => {
    await seedDataSource(orgId);
    await seedProvider(orgId, {
      scope: "primary",
      provider: "google-gemini",
      model: "gemini-pro-latest",
      secrets: { apiKey: "key-a" },
    });
    await expect(loadAgentModelKeys(orgId)).resolves.toEqual({ MODEL_API_KEY: "key-a", GEMINI_API_KEY: "key-a" });

    await db()
      .update(llm_provider_config)
      .set({ provider: "anthropic", model: "claude-opus-4-7", secrets: { apiKey: "key-b" }, updated_at: new Date() })
      .where(and(eq(llm_provider_config.org_id, orgId), eq(llm_provider_config.scope, "primary")));
    await expect(loadAgentModelKeys(orgId)).resolves.toEqual({ MODEL_API_KEY: "key-b", ANTHROPIC_API_KEY: "key-b" });

    await db()
      .update(llm_provider_config)
      .set({ enabled: false, updated_at: new Date() })
      .where(and(eq(llm_provider_config.org_id, orgId), eq(llm_provider_config.scope, "primary")));
    await expect(loadAgentModelKeys(orgId)).resolves.toEqual({});
    // Persisted org configuration never leaks into process.env.
    expect(process.env.OPENNEKO_AGENT_MODEL_KEY_ENV).toBeUndefined();
  });

  it("is ready when openneko serve answers, and not when it is gone", async () => {
    const socket = join(tempHome, "serve.sock");
    const server = createServer((conn) => conn.end());
    await new Promise<void>((done) => server.listen(socket, done));
    vi.stubEnv("OPENNEKO_SERVE_SOCKET", socket);
    try {
      await expect(verifyAgentRuntimeReady(orgId)).resolves.toBeUndefined();
      await new Promise<void>((done) => server.close(() => done()));
      await expect(verifyAgentRuntimeReady(orgId)).rejects.toThrow(/openneko serve is unavailable/);
    } finally {
      vi.unstubAllEnvs();
    }
  });

  it("memoizes provisioning per configuration revision and redoes it after a change", async () => {
    const refreshOrgId = uniqueOrgId("provision-refresh");
    await createTestOrg(refreshOrgId);
    try {
      await seedDataSource(refreshOrgId, { graphqlUrl: "http://first:8080/api/v1/graphql" });
      await expect(ensureHostConfigProvisioned(refreshOrgId)).resolves.toEqual({});
      expect(JSON.parse(await readFile(graphjinPath(tempHome), "utf8")).server).toBe("http://first:8080");

      await db()
        .update(data_source)
        .set({ graphql_url: "http://second:8080/api/v1/graphql", updated_at: new Date() })
        .where(eq(data_source.org_id, refreshOrgId));
      await ensureHostConfigProvisioned(refreshOrgId);
      expect(JSON.parse(await readFile(graphjinPath(tempHome), "utf8")).server).toBe("http://second:8080");
    } finally {
      await deleteTestOrg(refreshOrgId);
    }
  });
});

if (reachable) {
  afterAll(async () => {
    await pool().end();
  });
}

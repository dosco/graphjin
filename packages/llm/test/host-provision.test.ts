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
import {
  ensureHostConfigProvisioned,
  gatewayProviderName,
  patchGraphjinSourcesJwtSecret,
  provisionHostConfig,
  shouldReconcileDemoSourceAuthMode,
  verifyAgentRuntimeReady,
} from "../src/host-provision";
import {
  ensureOpenShellProvider,
  verifyOpenShellGateway,
} from "../src/work/sandbox-launcher";
import { graphjinSigningSecretB64 } from "../src/graphjin/token";

vi.mock("../src/work/sandbox-launcher", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../src/work/sandbox-launcher")>();
  return {
    ...actual,
    ensureOpenShellProvider: vi.fn().mockResolvedValue(undefined),
    verifyOpenShellGateway: vi.fn().mockResolvedValue(undefined),
  };
});

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
const ensureOpenShellProviderMock = vi.mocked(ensureOpenShellProvider);
const verifyOpenShellGatewayMock = vi.mocked(verifyOpenShellGateway);

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

describe("gatewayProviderName", () => {
  it("isolates gateway credentials by organization without exposing the org id", () => {
    const first = gatewayProviderName("customer-a");
    const second = gatewayProviderName("customer-b");
    expect(first).toMatch(/^openneko-agent-[a-f0-9]{16}$/);
    expect(second).toMatch(/^openneko-agent-[a-f0-9]{16}$/);
    expect(first).not.toBe(second);
    expect(first).not.toContain("customer-a");
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
    ensureOpenShellProviderMock.mockReset();
    ensureOpenShellProviderMock.mockResolvedValue(undefined);
    verifyOpenShellGatewayMock.mockReset();
    verifyOpenShellGatewayMock.mockResolvedValue(undefined);
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
      graphqlUrl: "http://graphjin:8080/api/v1/graphql",
    });

    await provisionHostConfig(orgId);

    const path = graphjinPath(tempHome);
    const content = JSON.parse(await readFile(path, "utf8"));
    expect(path).toBe(join(tempHome, "xdg-config", "graphjin", "client.json"));
    expect(content.server).toBe("http://graphjin:8080");
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

  it("syncs the model key to the gateway, never to a host file", async () => {
    await seedDataSource(orgId);
    await seedProvider(orgId, {
      scope: "agent",
      provider: "obsolete-runtime",
      config: { backend: "obsolete-runtime" },
    });
    await seedProvider(orgId, {
      scope: "primary",
      provider: "anthropic",
      model: "claude-opus-4-7",
      secrets: { apiKey: "sk-ant-test" },
    });

    await expect(provisionHostConfig(orgId)).resolves.toMatchObject({
      modelHosts: [{ host: "api.anthropic.com" }],
      keyAliases: [{ from: "MODEL_API_KEY", to: "ANTHROPIC_API_KEY" }],
    });
    expect(ensureOpenShellProviderMock).toHaveBeenCalledWith(
      expect.objectContaining({ apiKey: "sk-ant-test" }),
    );

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
    await expect(provisionHostConfig(orgId)).resolves.toEqual({
      modelHosts: [],
    });
    await readFile(graphjinPath(tempHome), "utf8");
  });

  it("does not throw when no data source exists (graphjin write skipped)", async () => {
    await expect(provisionHostConfig(orgId)).resolves.toEqual({
      modelHosts: [],
    });
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
      graphqlUrl: "http://graphjin:8080/api/v1/graphql",
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

  it("re-derives the sandbox egress env on a provider switch", async () => {
    await seedDataSource(orgId);
    await seedProvider(orgId, {
      scope: "agent",
      provider: "ax",
      config: {},
    });
    await seedProvider(orgId, {
      scope: "primary",
      provider: "google-gemini",
      model: "gemini-pro-latest",
      secrets: { apiKey: "key-a" },
    });
    const firstLaunch = await provisionHostConfig(orgId);
    expect(firstLaunch).toMatchObject({
      modelProvider: expect.stringMatching(/^openneko-agent-[a-f0-9]{16}-[a-f0-9]{8}$/),
      modelHosts: [
        { host: "generativelanguage.googleapis.com" },
      ],
      keyAliases: [{ from: "MODEL_API_KEY", to: "GEMINI_API_KEY" }],
    });

    // Operator switches provider; the old `||=` writes pinned the first
    // pass's derived values for the process lifetime, leaving the egress
    // allowlist and key env var on the previous provider.
    await clearProvider(orgId, "primary");
    await seedProvider(orgId, {
      scope: "primary",
      provider: "anthropic",
      model: "claude-opus-4-7",
      secrets: { apiKey: "key-b" },
    });
    const secondLaunch = await provisionHostConfig(orgId);
    expect(secondLaunch).toMatchObject({
      modelProvider: expect.stringMatching(/^openneko-agent-[a-f0-9]{16}-[a-f0-9]{8}$/),
      modelHosts: [
        { host: "api.anthropic.com" },
      ],
      keyAliases: [{ from: "MODEL_API_KEY", to: "ANTHROPIC_API_KEY" }],
    });

    // Persisted org configuration is returned as a run-local snapshot. It
    // must not contaminate process.env, where a separately bundled Next.js
    // route could later mistake it for an operator override.
    expect(process.env.OPENNEKO_AGENT_MODEL_HOST).toBeUndefined();
    expect(process.env.OPENNEKO_AGENT_MODEL_KEY_ENV).toBeUndefined();
  });

  it("keeps direct provisioning best-effort when OpenShell provider sync fails", async () => {
    // Persistent failure: the sync now retries in place (3 attempts) before
    // giving up, so a single rejection would be healed by the retry.
    process.env.OPENNEKO_PROVISION_RETRY_BASE_MS = "10";
    ensureOpenShellProviderMock.mockRejectedValue(new Error("gateway unavailable"));
    await seedDataSource(orgId);
    await seedProvider(orgId, {
      scope: "agent",
      provider: "ax",
      config: {},
    });
    await seedProvider(orgId, {
      scope: "primary",
      provider: "google-gemini",
      model: "gemini-pro-latest",
      secrets: { apiKey: "test-gemini-key" },
    });

    await expect(provisionHostConfig(orgId)).resolves.toEqual({
      modelHosts: [],
    });
    expect(ensureOpenShellProviderMock).toHaveBeenCalledTimes(3);
    await readFile(graphjinPath(tempHome), "utf8");
    delete process.env.OPENNEKO_PROVISION_RETRY_BASE_MS;
  });

  it("verifies the gateway and syncs the provider before an agent job", async () => {
    await seedProvider(orgId, {
      scope: "agent",
      provider: "ax",
      config: {},
    });
    await seedProvider(orgId, {
      scope: "primary",
      provider: "google-gemini",
      model: "gemini-pro-latest",
      secrets: { apiKey: "test-gemini-key" },
    });

    await verifyAgentRuntimeReady(orgId);

    expect(verifyOpenShellGatewayMock).toHaveBeenCalledWith({
      gatewayName: process.env.OPENSHELL_GATEWAY || undefined,
      gatewayEndpoint: process.env.OPENSHELL_GATEWAY_ENDPOINT || undefined,
    });
    expect(ensureOpenShellProviderMock).toHaveBeenCalledTimes(1);
  });

  it("fails readiness when the gateway RPC fails", async () => {
    verifyOpenShellGatewayMock.mockRejectedValueOnce(
      new Error("No active gateway"),
    );

    await expect(verifyAgentRuntimeReady(orgId)).rejects.toThrow(
      "No active gateway",
    );
    expect(ensureOpenShellProviderMock).not.toHaveBeenCalled();
  });

  it("refreshes worker-owned provisioning after first setup, key rotation, and provider switch", async () => {
    const refreshOrgId = uniqueOrgId("provision-refresh");
    await createTestOrg(refreshOrgId);
    try {
      await seedDataSource(refreshOrgId);
      await seedProvider(refreshOrgId, {
        scope: "agent",
        provider: "ax",
        config: {},
      });

      // The worker can become healthy before System Setup creates a primary
      // provider. That empty pass must not be memoized for the process lifetime.
      await ensureHostConfigProvisioned(refreshOrgId);
      expect(ensureOpenShellProviderMock).not.toHaveBeenCalled();

      await seedProvider(refreshOrgId, {
        scope: "primary",
        provider: "google-gemini",
        model: "gemini-pro-latest",
        secrets: { apiKey: "key-a" },
      });
      await ensureHostConfigProvisioned(refreshOrgId);
      expect(ensureOpenShellProviderMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ apiKey: "key-a" }),
      );

      await db()
        .update(llm_provider_config)
        .set({ secrets: { apiKey: "key-b" }, updated_at: new Date() })
        .where(
          and(
            eq(llm_provider_config.org_id, refreshOrgId),
            eq(llm_provider_config.scope, "primary"),
          ),
        );
      await ensureHostConfigProvisioned(refreshOrgId);
      expect(ensureOpenShellProviderMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ apiKey: "key-b" }),
      );
      const synced = ensureOpenShellProviderMock.mock.calls.map(([opts]) => opts as { providerName: string; family?: string });
      // A rotated key gets a new provider in the same family; runs in flight keep the old one.
      expect(synced.at(-1)!.providerName).not.toBe(synced.at(-2)!.providerName);
      expect(synced.at(-1)!.providerName.startsWith(`${synced.at(-1)!.family}-`)).toBe(true);

      await db()
        .update(llm_provider_config)
        .set({
          provider: "anthropic",
          model: "claude-opus-4-7",
          secrets: { apiKey: "key-c" },
          updated_at: new Date(),
        })
        .where(
          and(
            eq(llm_provider_config.org_id, refreshOrgId),
            eq(llm_provider_config.scope, "primary"),
          ),
        );
      const anthropicLaunch = await ensureHostConfigProvisioned(refreshOrgId);
      expect(ensureOpenShellProviderMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ apiKey: "key-c" }),
      );
      expect(anthropicLaunch).toMatchObject({
        modelHosts: [
          { host: "api.anthropic.com" },
        ],
        keyAliases: [{ from: "MODEL_API_KEY", to: "ANTHROPIC_API_KEY" }],
      });

      // Removing the saved key must not leave the previous gateway provider
      // attached to future sandboxes in this long-lived worker process.
      await db()
        .update(llm_provider_config)
        .set({ secrets: {}, updated_at: new Date() })
        .where(
          and(
            eq(llm_provider_config.org_id, refreshOrgId),
            eq(llm_provider_config.scope, "primary"),
          ),
        );
      const keylessLaunch = await ensureHostConfigProvisioned(refreshOrgId);
      expect(keylessLaunch.modelProvider).toBeUndefined();

      await db()
        .update(llm_provider_config)
        .set({ enabled: false, updated_at: new Date() })
        .where(
          and(
            eq(llm_provider_config.org_id, refreshOrgId),
            eq(llm_provider_config.scope, "primary"),
          ),
        );
      await expect(ensureHostConfigProvisioned(refreshOrgId)).resolves.toEqual({
        modelHosts: [],
      });
    } finally {
      await deleteTestOrg(refreshOrgId);
    }
  });

  it("retries memoized provisioning after an OpenShell provider sync failure", async () => {
    const retryOrgId = uniqueOrgId("provision-retry");
    await createTestOrg(retryOrgId);
    try {
      await seedDataSource(retryOrgId);
      await seedProvider(retryOrgId, {
        scope: "agent",
        provider: "ax",
        config: {},
      });
      await seedProvider(retryOrgId, {
        scope: "primary",
        provider: "google-gemini",
        model: "gemini-pro-latest",
        secrets: { apiKey: "test-gemini-key" },
      });
      // Persistently down first (all 3 in-place retries fail), then back.
      process.env.OPENNEKO_PROVISION_RETRY_BASE_MS = "10";
      ensureOpenShellProviderMock.mockRejectedValue(new Error("gateway unavailable"));

      await expect(ensureHostConfigProvisioned(retryOrgId)).rejects.toThrow(
        "gateway unavailable",
      );
      expect(ensureOpenShellProviderMock).toHaveBeenCalledTimes(3);

      ensureOpenShellProviderMock.mockResolvedValue(undefined);
      await expect(ensureHostConfigProvisioned(retryOrgId)).resolves.toMatchObject({
        modelProvider: expect.stringMatching(/^openneko-agent-[a-f0-9]{16}-[a-f0-9]{8}$/),
        keyAliases: [{ from: "MODEL_API_KEY", to: "GEMINI_API_KEY" }],
      });
      expect(ensureOpenShellProviderMock).toHaveBeenCalledTimes(4);
      delete process.env.OPENNEKO_PROVISION_RETRY_BASE_MS;
    } finally {
      await deleteTestOrg(retryOrgId);
    }
  });
});

if (reachable) {
  afterAll(async () => {
    await pool().end();
  });
}

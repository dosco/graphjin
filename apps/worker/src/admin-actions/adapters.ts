import { createHash, randomUUID } from "node:crypto";
import { mkdir, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import { basename, dirname, join } from "node:path";
import { registerActionAdapter } from "@neko/llm/workflows";
import {
  assertDatabaseSourcesStayReadOnly,
  buildGraphjinConfigUpdate,
  acquireGraphjinConfigLock,
  acquireManagedFileSourceLock,
  assertManagedFileSourceMutable,
  graphjinConfigPatchHash,
  graphjinInputWithJsonVariables,
  graphjinInputValue,
  managedFileSourceLocalDirectory,
  managedFileSourceRuntimeRoot,
  managedOpenApiSpecFilename,
  markManagedFileSourceApproved,
  parseManagedFileSourceManifest,
  persistGraphjinSourceConfigUpdate,
  verifyManagedFileSourceManifest,
} from "@neko/llm/graphjin";

/**
 * Executes approved group_admin action requests: groups, members, IdP
 * rules, item grants and data access rules. Changes that can alter
 * GraphJin roles schedule a batched GraphJin apply.
 */
export function registerGroupAdminAdapter(onGraphjinChange: (orgId: string) => void): void {
  registerActionAdapter("group_admin", async ({ request }) => {
    const db = await import("@neko/db");
    const { parseRowFilter } = await import("@neko/llm/graphjin");
    const payload = request.payload as Record<string, unknown>;
    const orgId = request.orgId;
    const text = (key: string) => {
      const value = payload[key];
      if (typeof value !== "string" || !value.trim()) throw new Error(`group_admin ${String(payload.action)}: ${key} required`);
      return value.trim();
    };
    const action = String(payload.action ?? "");
    const done = (result: Record<string, unknown>, graphjin = false) => {
      if (graphjin) onGraphjinChange(orgId);
      return { commandOrOperation: action, result };
    };
    switch (action) {
      case "create_group": {
        const group = await db.createUserGroup(orgId, { name: text("name"), description: typeof payload.description === "string" ? payload.description : null });
        return done({ groupId: group.id, slug: group.slug });
      }
      case "delete_group":
        await db.deleteUserGroup(orgId, text("groupId"));
        return done({ groupId: text("groupId") }, true);
      case "add_member":
        await db.addLocalGroupMember(orgId, text("groupId"), text("userId"));
        return done({ groupId: text("groupId"), userId: text("userId") });
      case "remove_member":
        return done(await db.removeLocalGroupMember(orgId, text("groupId"), text("userId")));
      case "add_idp_rule":
        return done(await db.createIdpGroupRule(orgId, { ssoGroupId: text("ssoGroupId"), userGroupId: text("groupId"), createdByUserId: request.actorUserId ?? null }));
      case "remove_idp_rule":
        await db.deleteIdpGroupRule(orgId, text("ruleId"));
        return done({ ruleId: text("ruleId") });
      case "grant_item":
      case "revoke_item": {
        const itemType = text("itemType");
        if (!db.isItemType(itemType)) throw new Error(`group_admin: unknown item type ${itemType}`);
        const input = { groupId: text("groupId"), itemType, itemId: text("itemId"), actorUserId: request.actorUserId ?? null, actionRequestId: request.id };
        const result = action === "grant_item" ? await db.grantItem(orgId, input) : await db.revokeItem(orgId, input);
        return done(result, itemType === "data_source" || itemType === "api_operation");
      }
      case "set_table_access": {
        const table = text("table");
        const [schema, name] = table.includes(".") ? [table.slice(0, table.lastIndexOf(".")), table.slice(table.lastIndexOf(".") + 1)] : ["", table];
        const columns = Array.isArray(payload.columns) ? payload.columns.map(String) : [];
        const rule = await db.upsertDataAccessRule(orgId, {
          groupId: text("groupId"),
          source: text("source"),
          tableSchema: schema,
          tableName: name,
          columns,
          rowFilter: payload.rowFilter == null ? null : parseRowFilter(payload.rowFilter),
          actorUserId: request.actorUserId ?? null,
        });
        return done({ ruleId: rule.id }, true);
      }
      case "remove_table_access":
        return done({ removed: await db.deleteDataAccessRule(orgId, text("ruleId")) }, true);
      default:
        throw new Error(`group_admin: unknown action "${action}"`);
    }
  });
}

/**
 * ADM1 — executes approved user_admin action requests (admin-approved
 * per the user_management_default policy; K2 enforces the approver).
 * invite pre-creates the app_user row so SSO links by email on first
 * sign-in; deactivate sets disabled_at — sign-in and live sessions both
 * die on it.
 */
export function registerUserAdminAdapter(): void {
  registerActionAdapter("user_admin", async ({ request }) => {
    const { app_user, and, db, eq } = await import("@neko/db");
    const payload = request.payload as Record<string, unknown>;
    const action = String(payload.action ?? "");
    const orgId = request.orgId;

    if (action === "invite") {
      const email = String(payload.email ?? "").trim().toLowerCase();
      const role = payload.role === "admin" ? "admin" : "member";
      if (!email) throw new Error("user_admin invite: email required");
      const [existing] = await db()
        .select({ id: app_user.id })
        .from(app_user)
        .where(and(eq(app_user.org_id, orgId), eq(app_user.email, email)))
        .limit(1);
      if (existing) {
        return {
          commandOrOperation: `invite ${email}`,
          result: { userId: existing.id, alreadyExisted: true },
        };
      }
      const id = randomUUID();
      await db().insert(app_user).values({ id, email, org_id: orgId });
      if (role === "admin") {
        const { setLocalAdministrator } = await import("@neko/db");
        await setLocalAdministrator(orgId, id, true);
      }
      return {
        commandOrOperation: `invite ${email} as ${role}`,
        result: { userId: id, role },
      };
    }

    const userId = String(payload.userId ?? "");
    if (!userId) throw new Error(`user_admin ${action}: userId required`);
    const where = and(eq(app_user.org_id, orgId), eq(app_user.id, userId));

    if (action === "set_role") {
      const role = payload.role === "admin" ? "admin" : "member";
      const { setLocalAdministrator } = await import("@neko/db");
      const [row] = await db().select({ id: app_user.id }).from(app_user).where(where).limit(1);
      if (!row) throw new Error(`user ${userId} not found`);
      await setLocalAdministrator(orgId, userId, role === "admin");
      return { commandOrOperation: `set_role ${userId} ${role}`, result: { userId, role } };
    }
    if (action === "deactivate" || action === "reactivate") {
      const disabled_at = action === "deactivate" ? new Date() : null;
      const rows = await db()
        .update(app_user)
        .set({ disabled_at, updated_at: new Date() })
        .where(where)
        .returning({ id: app_user.id });
      if (rows.length === 0) throw new Error(`user ${userId} not found`);
      return {
        commandOrOperation: `${action} ${userId}`,
        result: { userId, disabled: action === "deactivate" },
      };
    }
    throw new Error(`user_admin: unknown action "${action}"`);
  });
}

/**
 * ADM5 — executes approved channel_admin action requests (admin-approved
 * per the channel_management_default policy). Same verbs as the
 * admin-map API: link an identity to an app_user, unlink it back to
 * anonymous, block/unblock it.
 */
export function registerChannelAdminAdapter(): void {
  registerActionAdapter("channel_admin", async ({ request }) => {
    const { and, app_user, channel_identity, db, eq, isNull } = await import(
      "@neko/db"
    );
    const payload = request.payload as Record<string, unknown>;
    const action = String(payload.action ?? "");
    const identityId = String(payload.identityId ?? "");
    const orgId = request.orgId;
    if (!identityId) throw new Error(`channel_admin ${action}: identityId required`);
    const where = and(
      eq(channel_identity.org_id, orgId),
      eq(channel_identity.id, identityId),
    );
    const now = new Date();

    if (action === "link") {
      const appUserId = String(payload.appUserId ?? "");
      const [user] = await db()
        .select({ id: app_user.id })
        .from(app_user)
        .where(
          and(
            eq(app_user.org_id, orgId),
            eq(app_user.id, appUserId),
            isNull(app_user.disabled_at),
          ),
        )
        .limit(1);
      if (!user) throw new Error(`channel_admin link: unknown or disabled user ${appUserId}`);
      const rows = await db()
        .update(channel_identity)
        .set({
          app_user_id: user.id,
          status: "linked",
          verified_at: now,
          updated_at: now,
        })
        .where(where)
        .returning({ id: channel_identity.id });
      if (rows.length === 0) throw new Error(`channel identity ${identityId} not found`);
      return {
        commandOrOperation: `link ${identityId} → ${appUserId}`,
        result: { identityId, appUserId, status: "linked" },
      };
    }

    if (action === "unlink" || action === "block" || action === "unblock") {
      const rows = await db()
        .update(channel_identity)
        .set({
          ...(action === "block"
            ? { status: "blocked" }
            : { app_user_id: null, status: "unverified", verified_at: null }),
          updated_at: now,
        })
        .where(where)
        .returning({ id: channel_identity.id, status: channel_identity.status });
      if (rows.length === 0) throw new Error(`channel identity ${identityId} not found`);
      return {
        commandOrOperation: `${action} ${identityId}`,
        result: { identityId, status: rows[0].status },
      };
    }

    throw new Error(`channel_admin: unknown action "${action}"`);
  });
}

/**
 * ADM2 — executes approved data_source_admin action requests. register
 * creates a DISABLED placeholder row (no connection details — the admin
 * fills those in Settings; credentials never pass through chat);
 * enable/disable/set_default/remove manage the registry.
 */
export function registerDataSourceAdminAdapter(): void {
  registerActionAdapter("data_source_admin", async ({ request }) => {
    const { and, data_source, db, eq, ne } = await import("@neko/db");
    const payload = request.payload as Record<string, unknown>;
    const action = String(payload.action ?? "");
    const name = String(payload.name ?? "").trim();
    const orgId = request.orgId;
    if (!name) throw new Error(`data_source_admin ${action}: name required`);
    const byName = and(eq(data_source.org_id, orgId), eq(data_source.name, name));
    const now = new Date();

    if (action === "register") {
      const [existing] = await db()
        .select({ id: data_source.id })
        .from(data_source)
        .where(byName)
        .limit(1);
      if (existing) {
        return {
          commandOrOperation: `register ${name}`,
          result: { name, alreadyExisted: true },
        };
      }
      const SOURCE_KINDS = ["graphjin", "database", "api", "files", "code"];
      const sourceKind =
        typeof payload.sourceKind === "string" && SOURCE_KINDS.includes(payload.sourceKind)
          ? payload.sourceKind
          : "graphjin";
      const [row] = await db()
        .insert(data_source)
        .values({
          org_id: orgId,
          kind: sourceKind,
          graphql_url: "",
          name,
          label: typeof payload.label === "string" ? payload.label : null,
          enabled: false,
        })
        .returning({ id: data_source.id });
      return {
        commandOrOperation: `register ${name} (placeholder — complete in Settings)`,
        result: { id: row.id, name, enabled: false },
      };
    }

    if (action === "enable" || action === "disable") {
      const rows = await db()
        .update(data_source)
        .set({ enabled: action === "enable", updated_at: now })
        .where(byName)
        .returning({ id: data_source.id });
      if (rows.length === 0) throw new Error(`data source ${name} not found`);
      return {
        commandOrOperation: `${action} ${name}`,
        result: { name, enabled: action === "enable" },
      };
    }

    if (action === "set_default") {
      const rows = await db()
        .update(data_source)
        .set({ is_default: true, updated_at: now })
        .where(byName)
        .returning({ id: data_source.id });
      if (rows.length === 0) throw new Error(`data source ${name} not found`);
      await db()
        .update(data_source)
        .set({ is_default: false, updated_at: now })
        .where(and(eq(data_source.org_id, orgId), ne(data_source.name, name)));
      return { commandOrOperation: `set_default ${name}`, result: { name } };
    }

    if (action === "remove") {
      const [row] = await db()
        .select({ id: data_source.id, isDefault: data_source.is_default })
        .from(data_source)
        .where(byName)
        .limit(1);
      if (!row) throw new Error(`data source ${name} not found`);
      if (row.isDefault) {
        throw new Error("cannot remove the default data source — set another default first");
      }
      await db().delete(data_source).where(byName);
      return { commandOrOperation: `remove ${name}`, result: { name, removed: true } };
    }

    throw new Error(`data_source_admin: unknown action "${action}"`);
  });
}

/** OL5: the internal GraphJin endpoint the customer config path must never touch. */
function internalGraphjinHost(): string | null {
  try {
    return new URL(
      process.env.OPENNEKO_GRAPHJIN_URL ?? "http://127.0.0.1:8089",
    ).host;
  } catch {
    return null;
  }
}

function graphqlEndpointFrom(url: string): string {
  const trimmed = url.replace(/\/+$/, "");
  return trimmed.endsWith("/api/v1/graphql")
    ? trimmed
    : `${trimmed}/api/v1/graphql`;
}

/**
 * OL5 — executes approved source_config_admin actions: configure the
 * CUSTOMER GraphJin (roles, per-source access, source registration) via
 * its admin-only gj_config two-phase preview→apply. Three guarantees:
 *   1. Target is resolved ONLY from data_source (the customer engine);
 *      a host matching OPENNEKO_GRAPHJIN_URL is refused outright.
 *   2. The agent never holds config-write power — it only proposes; this
 *      adapter (post-approval, outside the sandbox) applies with a minted
 *      admin token.
 *   3. Connection secrets stay value-blind to the agent: a payload carries
 *      a secretRef NAME; the real value is resolved + decrypted here and
 *      injected into the gj_config source — never in the agent, payload, or
 *      audit row.
 */
export function registerSourceConfigAdminAdapter(): void {
  registerActionAdapter("source_config_admin", async ({ request }) => {
    const {
      data_source,
      data_source_secret,
      openapi_spec_asset,
      app_user,
      and,
      db,
      desc,
      eq,
      getGraphjinConfigSettingsForOrg,
      ne,
    } = await import("@neko/db");
    const { graphjinQuery, mintGraphjinToken } = await import("@neko/llm/graphjin");
    const payload = request.payload as Record<string, unknown>;
    const action = String(payload.action ?? "");
    const orgId = request.orgId;

    const settings = await getGraphjinConfigSettingsForOrg(orgId);
    if (!settings.sourceConfigEnabled) {
      throw new Error(
        "source_config_admin: GraphJin config capability is disabled",
      );
    }

    const dataSourceId =
      typeof payload.dataSourceId === "string" ? payload.dataSourceId : null;
    const sources = await db()
      .select({ id: data_source.id, graphqlUrl: data_source.graphql_url })
      .from(data_source)
      .where(
        and(
          eq(data_source.org_id, orgId),
          eq(data_source.enabled, true),
          ...(dataSourceId ? [eq(data_source.id, dataSourceId)] : []),
        ),
      )
      .orderBy(desc(data_source.is_default), data_source.created_at)
      .limit(dataSourceId ? 1 : 3);
    if (!dataSourceId && sources.length > 1) {
      throw new Error(
        "source_config_admin: multiple enabled data sources exist; an explicit dataSourceId is required",
      );
    }
    const [src] = sources;
    if (!src?.graphqlUrl) {
      throw new Error("source_config_admin: no data source configured");
    }
    const endpoint = graphqlEndpointFrom(src.graphqlUrl);
    // BOUNDARY: never configure the OpenNeko internal GraphJin.
    const internal = internalGraphjinHost();
    let endpointHost: string;
    try {
      endpointHost = new URL(endpoint).host;
    } catch {
      throw new Error(`source_config_admin: invalid data source URL ${endpoint}`);
    }
    if (internal && endpointHost === internal) {
      throw new Error(
        "source_config_admin: refusing to configure the OpenNeko internal GraphJin",
      );
    }

    const selectedKind = Array.isArray(payload.kind)
      ? payload.kind[0]
      : payload.kind;
    const selectedBackend = Array.isArray(payload.backend)
      ? payload.backend[0]
      : payload.backend;
    const isManagedOpenApi =
      action === "register_source" && selectedKind === "api";
    const isManagedLocalFile =
      action === "register_source" &&
      selectedKind === "file" &&
      selectedBackend === "local";
    const needsDurableSourceConfig =
      action === "register_source" ||
      action === "set_source_access" ||
      action === "set_source_capabilities" ||
      action === "expose_api_operation" ||
      action === "enable_api_writes";
    const sourceName = typeof payload.name === "string" ? payload.name.trim() : "";
    let specAsset: typeof openapi_spec_asset.$inferSelect | null = null;
    let managedRuntimeDir: string | undefined;
    let managedLocalFile: string | null = null;
    let managedLocalFilesRoot: string | undefined;
    let managedLocalFilesDirectory: string | undefined;
    let localFileManifest:
      | ReturnType<typeof parseManagedFileSourceManifest>
      | undefined;
    const durableConfigFile = needsDurableSourceConfig
      ? process.env.OPENNEKO_GRAPHJIN_CONFIG?.trim() || null
      : null;
    if (needsDurableSourceConfig && !durableConfigFile) {
      throw new Error(
        "source_config_admin: source changes require the worker to share GraphJin's config volume",
      );
    }
    if (isManagedOpenApi) {
      const specAssetId =
        typeof payload.specAssetId === "string" ? payload.specAssetId : "";
      if (!specAssetId) {
        throw new Error("source_config_admin: API source has no managed OpenAPI asset");
      }
      [specAsset] = await db()
        .select()
        .from(openapi_spec_asset)
        .where(
          and(
            eq(openapi_spec_asset.org_id, orgId),
            eq(openapi_spec_asset.id, specAssetId),
          ),
        )
        .limit(1);
      if (!specAsset) {
        throw new Error("source_config_admin: managed OpenAPI asset is unavailable");
      }
      const actualChecksum = createHash("sha256")
        .update(specAsset.content)
        .digest("hex");
      if (actualChecksum !== specAsset.checksum_sha256) {
        throw new Error("source_config_admin: managed OpenAPI asset failed its integrity check");
      }
      const runtimeRoot =
        process.env.OPENNEKO_GRAPHJIN_SPECS_DIR?.replace(/\/+$/, "") ||
        "/config/specs";
      managedRuntimeDir = runtimeRoot;
      managedLocalFile = join(
        dirname(durableConfigFile!),
        "specs",
        managedOpenApiSpecFilename(orgId, sourceName),
      );
    }
    if (isManagedLocalFile) {
      localFileManifest = parseManagedFileSourceManifest(
        payload.localFiles,
        sourceName,
      );
      managedLocalFilesRoot = managedFileSourceRuntimeRoot({
        orgId,
        sourceName,
        runtimeBase:
          process.env.OPENNEKO_GRAPHJIN_FILES_DIR?.trim() || "/config/files",
      });
      managedLocalFilesDirectory = managedFileSourceLocalDirectory({
        configFile: durableConfigFile!,
        orgId,
        sourceName,
        localBase:
          process.env.OPENNEKO_GRAPHJIN_FILES_LOCAL_DIR?.trim() || undefined,
      });
      if (
        basename(managedLocalFilesDirectory) !== basename(managedLocalFilesRoot)
      ) {
        throw new Error(
          "source_config_admin: managed file storage paths are inconsistent",
        );
      }
    }

    const { update, secretName } = await buildGraphjinConfigUpdate(
      payload,
      async (ref) => {
        const [row] = await db()
          .select({ valueEnc: data_source_secret.value_enc })
          .from(data_source_secret)
          .where(
            and(
              eq(data_source_secret.org_id, orgId),
              eq(data_source_secret.name, ref),
            ),
          )
          .limit(1);
        if (!row) {
          throw new Error(
            `register_source: no stored secret named "${ref}" — add it in GraphJin Config first`,
          );
        }
        const { maybeDecryptSecret } = await import("@neko/llm/secrets");
        return maybeDecryptSecret(row.valueEnc);
      },
      managedRuntimeDir || managedLocalFilesRoot
        ? {
            ...(managedRuntimeDir
              ? { managedOpenApiSpecsDir: managedRuntimeDir }
              : {}),
            ...(managedLocalFilesRoot
              ? { managedLocalFilesRoot }
              : {}),
          }
        : undefined,
    );
    const approvedPreview =
      payload.preview && typeof payload.preview === "object"
        ? (payload.preview as Record<string, unknown>)
        : null;
    if (!approvedPreview) {
      throw new Error(
        "source_config_admin: approved request is missing a validated preview",
      );
    }
    const credentialFreePayload = { ...payload };
    delete credentialFreePayload.preview;
    delete credentialFreePayload.specSummary;
    const patchHash = graphjinConfigPatchHash(credentialFreePayload);
    if (approvedPreview.patchHash !== patchHash) {
      throw new Error(
        "source_config_admin: approved payload no longer matches its preview",
      );
    }

    if (request.approvedByUserId) {
      const [approver] = await db()
        .select({ disabledAt: app_user.disabled_at })
        .from(app_user)
        .where(
          and(
            eq(app_user.id, request.approvedByUserId),
            eq(app_user.org_id, orgId),
          ),
        )
        .limit(1);
      const { resolveUserGroups } = await import("@neko/db");
      const approverGroups = approver ? await resolveUserGroups(orgId, request.approvedByUserId) : null;
      if (!approver || approver.disabledAt || !approverGroups?.administrator) {
        throw new Error(
          "source_config_admin: approving user is no longer an active admin",
        );
      }
    } else if (request.actorRole !== "admin") {
      throw new Error(
        "source_config_admin: missing trusted admin approval provenance",
      );
    }
    const adminToken = mintGraphjinToken({
      orgId,
      userId: request.approvedByUserId ?? request.actorUserId ?? null,
      role: "admin",
      ttlSeconds: 120,
    });
    const headers = { authorization: `Bearer ${adminToken}` };

    let restoreManagedSpec: (() => Promise<void>) | null = null;
    let restoreManagedConfig: (() => Promise<void>) | null = null;
    let appliedCatalogRevision: string | null = null;
    let liveRegisteredSource = false;
    let releaseGraphjinConfigLock: (() => Promise<void>) | null = null;
    let releaseManagedFileLock: (() => Promise<void>) | null = null;
    try {
      if (durableConfigFile) {
        releaseGraphjinConfigLock = await acquireGraphjinConfigLock({
          configFile: durableConfigFile,
        });
      }
      if (localFileManifest) {
        const localBase =
          process.env.OPENNEKO_GRAPHJIN_FILES_LOCAL_DIR?.trim() || undefined;
        releaseManagedFileLock = await acquireManagedFileSourceLock({
          configFile: durableConfigFile!,
          orgId,
          sourceName,
          localBase,
        });
      }
      if (specAsset && managedLocalFile) {
        await mkdir(dirname(managedLocalFile), { recursive: true });
        let previousContent: Buffer | null = null;
        try {
          previousContent = await readFile(managedLocalFile);
        } catch (error) {
          if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
        }
        const temporary = `${managedLocalFile}.${randomUUID()}.tmp`;
        // The worker and GraphJin images run as different non-root users. The
        // shared Docker volume is private to the stack, while the document must
        // remain readable by the GraphJin process.
        await writeFile(temporary, specAsset.content, { mode: 0o644 });
        await rename(temporary, managedLocalFile);
        restoreManagedSpec = async () => {
          if (previousContent) {
            const rollback = `${managedLocalFile}.${randomUUID()}.rollback`;
            await writeFile(rollback, previousContent, { mode: 0o644 });
            await rename(rollback, managedLocalFile);
          } else {
            await rm(managedLocalFile, { force: true });
          }
        };
      }
      if (localFileManifest) {
        await assertManagedFileSourceMutable({
          configFile: durableConfigFile!,
          orgId,
          sourceName,
          localBase:
            process.env.OPENNEKO_GRAPHJIN_FILES_LOCAL_DIR?.trim() || undefined,
        });
        await verifyManagedFileSourceManifest({
          configFile: durableConfigFile!,
          orgId,
          sourceName,
          localBase:
            process.env.OPENNEKO_GRAPHJIN_FILES_LOCAL_DIR?.trim() || undefined,
          files: localFileManifest.files,
          totalSize: localFileManifest.totalSize,
        });
      }
      // Phase 0: read the current catalog_revision — the two-phase apply needs
      // it for optimistic concurrency (a stale value rejects the apply).
      const revRes = await graphjinQuery<{
        gj_config?: { catalog_revision?: string; sources?: unknown };
      }>({
        baseUrl: endpoint,
        headers,
        query: 'query { gj_config(id: "current") { catalog_revision sources } }',
      });
      const rev = revRes.data?.gj_config?.catalog_revision;
      if (!rev) {
        throw new Error(
          `source_config_admin: could not read catalog_revision: ${revRes.errors?.map((e) => e.message).join("; ") ?? "no revision"}`,
        );
      }
      if (approvedPreview.catalogRevision !== rev) {
        throw new Error(
          "source_config_admin: GraphJin configuration changed after approval; create and approve a fresh preview",
        );
      }
      let currentSources = revRes.data?.gj_config?.sources;
      if (typeof currentSources === "string") {
        try {
          currentSources = JSON.parse(currentSources);
        } catch {
          currentSources = [];
        }
      }
      assertDatabaseSourcesStayReadOnly(update, currentSources);
      if (
        action === "register_source" &&
        Array.isArray(currentSources) &&
        currentSources.some(
          (source) =>
            source &&
            typeof source === "object" &&
            !Array.isArray(source) &&
            (source as Record<string, unknown>).name === sourceName,
        )
      ) {
        throw new Error(
          `source_config_admin: a GraphJin source named "${sourceName}" already exists`,
        );
      }

      // Phase 1: preview (validates without applying). Inline object syntax —
      // introspection is off, so no typed `$update` variable.
      const previewInput = graphjinInputWithJsonVariables({
        mode: "preview",
        expected_catalog_revision: rev,
        ...update,
      });
      const preview = await graphjinQuery<{
        gj_config?: {
          valid?: boolean;
          preview_id?: string;
          change_summary_json?: string;
          errors_json?: string;
        };
      }>({
        baseUrl: endpoint,
        headers,
        query: `mutation${previewInput.variableDefinitions} { gj_config(id: "current", update: ${previewInput.literal}) { valid preview_id change_summary_json errors_json } }`,
        variables: previewInput.variables,
      });
      if (preview.errors?.length) {
        throw new Error(
          `source_config_admin preview failed: ${preview.errors.map((e) => e.message).join("; ")}`,
        );
      }
      const pv = preview.data?.gj_config;
      if (!pv?.valid || !pv.preview_id) {
        throw new Error(
          `source_config_admin preview invalid: ${pv?.errors_json ?? "no preview_id returned"}`,
        );
      }

      // Phase 2: apply with the same preview_id + identical payload.
      const applyInput = graphjinInputWithJsonVariables({
        mode: "apply",
        preview_id: pv.preview_id,
        expected_catalog_revision: rev,
        ...update,
      });
      const applied = await graphjinQuery<{
        gj_config?: {
          applied?: boolean;
          catalog_revision?: string;
          errors_json?: string;
        };
      }>({
        baseUrl: endpoint,
        headers,
        query: `mutation${applyInput.variableDefinitions} { gj_config(id: "current", update: ${applyInput.literal}) { applied catalog_revision errors_json } }`,
        variables: applyInput.variables,
      });
      if (applied.errors?.length) {
        throw new Error(
          `source_config_admin apply failed: ${applied.errors.map((e) => e.message).join("; ")}`,
        );
      }
      const av = applied.data?.gj_config;
      if (!av?.applied) {
        throw new Error(
          `source_config_admin apply rejected: ${av?.errors_json ?? "unknown"}`,
        );
      }
      appliedCatalogRevision = av.catalog_revision ?? null;
      liveRegisteredSource = action === "register_source";

      if (durableConfigFile) {
        const previousConfig = await readFile(durableConfigFile);
        const previousMode = (await stat(durableConfigFile)).mode & 0o777;
        await persistGraphjinSourceConfigUpdate({
          configFile: durableConfigFile,
          update,
        });
        restoreManagedConfig = async () => {
          const rollback = `${durableConfigFile}.${randomUUID()}.rollback`;
          await writeFile(rollback, previousConfig, { mode: previousMode });
          await rename(rollback, durableConfigFile);
        };
      }

      if (localFileManifest) {
        await verifyManagedFileSourceManifest({
          configFile: durableConfigFile!,
          orgId,
          sourceName,
          localBase:
            process.env.OPENNEKO_GRAPHJIN_FILES_LOCAL_DIR?.trim() || undefined,
          files: localFileManifest.files,
          totalSize: localFileManifest.totalSize,
        });
      }

      let verifiedOperations: Array<{ id: string; name: string; summary: string }> = [];
      if (specAsset) {
        const operationPrefix = managedOpenApiSpecFilename(orgId, sourceName)
          .replace(/\.yaml$/i, "_");
        for (let attempt = 0; attempt < 20; attempt += 1) {
          const catalog = await graphjinQuery<{
            gj_catalog?: Array<{ id: string; name: string; summary: string }>;
          }>({
            baseUrl: endpoint,
            headers,
            query:
              'query { gj_catalog(where: { kind: { eq: "table" } }, limit: 500) { id name summary } }',
          });
          if (catalog.errors?.length) {
            throw new Error(
              `source_config_admin: catalog verification failed: ${catalog.errors.map((item) => item.message).join("; ")}`,
            );
          }
          verifiedOperations = (catalog.data?.gj_catalog ?? []).filter(
            (item) =>
              item.id.startsWith(operationPrefix) ||
              item.name.startsWith(operationPrefix),
          );
          if (verifiedOperations.length > 0) break;
          await new Promise((resolve) => setTimeout(resolve, 500));
        }
        if (verifiedOperations.length === 0) {
          throw new Error(
            "source_config_admin: GraphJin applied the source but exposed no operations from its OpenAPI document",
          );
        }
        const database = db();
        await database.transaction(async (tx) => {
          await tx
            .update(openapi_spec_asset)
            .set({ status: "superseded", updated_at: new Date() })
            .where(
              and(
                eq(openapi_spec_asset.org_id, orgId),
                eq(openapi_spec_asset.source_name, sourceName),
                eq(openapi_spec_asset.status, "active"),
                ne(openapi_spec_asset.id, specAsset!.id),
              ),
            );
          await tx
            .update(openapi_spec_asset)
            .set({
              status: "active",
              source_name: sourceName,
              activated_at: new Date(),
              updated_at: new Date(),
            })
            .where(
              and(
                eq(openapi_spec_asset.org_id, orgId),
                eq(openapi_spec_asset.id, specAsset!.id),
              ),
            );
        });
      }

      let verifiedFileSource = false;
      if (localFileManifest) {
        for (let attempt = 0; attempt < 20; attempt += 1) {
          const catalog = await graphjinQuery<{
            gj_catalog?: Array<{ id: string; name: string; summary: string }>;
          }>({
            baseUrl: endpoint,
            headers,
            query:
              'query { gj_catalog(where: { kind: { eq: "table" } }, limit: 500) { id name summary } }',
          });
          if (catalog.errors?.length) {
            throw new Error(
              `source_config_admin: file catalog verification failed: ${catalog.errors.map((item) => item.message).join("; ")}`,
            );
          }
          verifiedFileSource = (catalog.data?.gj_catalog ?? []).some(
            (item) => item.id === sourceName || item.name === sourceName,
          );
          if (verifiedFileSource) break;
          await new Promise((resolve) => setTimeout(resolve, 500));
        }
        if (!verifiedFileSource) {
          throw new Error(
            "source_config_admin: GraphJin applied the local file source but did not expose its virtual table",
          );
        }
        await markManagedFileSourceApproved({
          configFile: durableConfigFile!,
          orgId,
          sourceName,
          localBase:
            process.env.OPENNEKO_GRAPHJIN_FILES_LOCAL_DIR?.trim() || undefined,
        });
      }

      return {
        commandOrOperation: `${action} on customer GraphJin (${endpointHost})`,
        result: {
          action,
          host: endpointHost,
          changeSummary: pv.change_summary_json ?? null,
          catalogRevision: av.catalog_revision ?? null,
          ...(specAsset
            ? {
                openApi: {
                  assetId: specAsset.id,
                  title: specAsset.title,
                  version: specAsset.version,
                  operationCount: verifiedOperations.length,
                  checksumSha256: specAsset.checksum_sha256,
                },
              }
            : {}),
          ...(localFileManifest
            ? {
                fileSource: {
                  backend: "local",
                  fileCount: localFileManifest.files.length,
                  totalSize: localFileManifest.totalSize,
                  readOnly: true,
                },
              }
            : {}),
          // Name only — never the value.
          ...(secretName ? { secretRef: secretName } : {}),
        },
      };
    } catch (error) {
      const rollbackFailures: string[] = [];
      let liveRollbackFailed = false;
      if (liveRegisteredSource && appliedCatalogRevision && sourceName) {
        try {
          const rollbackUpdate = { remove_sources: [sourceName] };
          const rollbackPreviewInline = graphjinInputValue({
            mode: "preview",
            expected_catalog_revision: appliedCatalogRevision,
            ...rollbackUpdate,
          });
          const rollbackPreview = await graphjinQuery<{
            gj_config?: { valid?: boolean; preview_id?: string; errors_json?: string };
          }>({
            baseUrl: endpoint,
            headers,
            query: `mutation { gj_config(id: "current", update: ${rollbackPreviewInline}) { valid preview_id errors_json } }`,
          });
          const rollbackRow = rollbackPreview.data?.gj_config;
          if (
            rollbackPreview.errors?.length ||
            !rollbackRow?.valid ||
            !rollbackRow.preview_id
          ) {
            throw new Error(
              rollbackPreview.errors?.map((item) => item.message).join("; ") ||
                rollbackRow?.errors_json ||
                "rollback preview was rejected",
            );
          }
          const rollbackApplyInline = graphjinInputValue({
            mode: "apply",
            preview_id: rollbackRow.preview_id,
            expected_catalog_revision: appliedCatalogRevision,
            ...rollbackUpdate,
          });
          const rollbackApply = await graphjinQuery<{
            gj_config?: { applied?: boolean; errors_json?: string };
          }>({
            baseUrl: endpoint,
            headers,
            query: `mutation { gj_config(id: "current", update: ${rollbackApplyInline}) { applied errors_json } }`,
          });
          if (
            rollbackApply.errors?.length ||
            rollbackApply.data?.gj_config?.applied !== true
          ) {
            throw new Error(
              rollbackApply.errors?.map((item) => item.message).join("; ") ||
                rollbackApply.data?.gj_config?.errors_json ||
                "rollback apply was rejected",
            );
          }
        } catch (rollbackError) {
          liveRollbackFailed = true;
          rollbackFailures.push(
            `GraphJin live-source rollback failed: ${rollbackError instanceof Error ? rollbackError.message : String(rollbackError)}`,
          );
        }
      }
      if (restoreManagedSpec) {
        try {
          await restoreManagedSpec();
        } catch (rollbackError) {
          rollbackFailures.push(
            `OpenAPI file rollback failed: ${rollbackError instanceof Error ? rollbackError.message : String(rollbackError)}`,
          );
        }
      }
      if (restoreManagedConfig) {
        try {
          await restoreManagedConfig();
        } catch (rollbackError) {
          rollbackFailures.push(
            `GraphJin config rollback failed: ${rollbackError instanceof Error ? rollbackError.message : String(rollbackError)}`,
          );
        }
      }
      if (liveRollbackFailed && localFileManifest) {
        try {
          await markManagedFileSourceApproved({
            configFile: durableConfigFile!,
            orgId,
            sourceName,
            localBase:
              process.env.OPENNEKO_GRAPHJIN_FILES_LOCAL_DIR?.trim() || undefined,
          });
        } catch (freezeError) {
          rollbackFailures.push(
            `managed file source freeze failed: ${freezeError instanceof Error ? freezeError.message : String(freezeError)}`,
          );
        }
      }
      if (rollbackFailures.length > 0) {
        throw new Error(
          `${error instanceof Error ? error.message : String(error)}; ${rollbackFailures.join("; ")}`,
        );
      }
      throw error;
    } finally {
      await releaseManagedFileLock?.();
      await releaseGraphjinConfigLock?.();
    }
  });
}

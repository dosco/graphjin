import { packConnectionHeader } from "@neko/packs/oauth-client";
import { resolveWatcherVariables } from "@neko/llm/workflows";
import { Kind, parse, print, visit, type FieldNode, type SelectionSetNode } from "graphql";
import { basename, extname } from "node:path";
import { sha256, type SolutionPackBundle } from "@neko/packs";

/** Resolve values, never interpolate secrets into serialized YAML or JSON. */
export function packValue(value: unknown, inputs: Record<string, unknown>, secrets: Record<string, string> = {}): unknown {
  if (typeof value === "string") {
    const match = /^\{\{([^}]+)}}$/.exec(value);
    if (!match) {
      if (value.includes("{{")) throw new Error("pack templates must occupy the complete value");
      return value;
    }
    const key = match[1]!.trim();
    const resolved = key.startsWith("secret.") ? secrets[key.slice(7)] : inputs[key];
    if (resolved === undefined) throw new Error(`missing pack template input ${key}`);
    return resolved;
  }
  if (Array.isArray(value)) return value.map(item => packValue(item, inputs, secrets));
  if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, packValue(item, inputs, secrets)]));
  return value;
}

const HTTP_TOKEN = /^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,128}$/;

/** Map a pack API auth declaration to its GraphJin spec auth. */
function packApiAuth(auth: Record<string, unknown>, requestHeader?: string): Record<string, unknown> {
  const only = (keys: string[]) => Object.keys(auth).every(key => keys.includes(key));
  if (auth.type === "bearer" && typeof auth.token === "string" && only(["type", "token"])) {
    return { scheme: "bearer", ...(requestHeader ? { token_from_request: { header: requestHeader } } : { token: auth.token }) };
  }
  if (auth.type === "api_key" && typeof auth.key_value === "string" && typeof auth.key_name === "string" && HTTP_TOKEN.test(auth.key_name)
    && [undefined, "header", "query"].includes(auth.key_in as string | undefined) && only(["type", "key_name", "key_value", "key_in"])) {
    return { scheme: "api_key", key_name: auth.key_name, key_value: auth.key_value, key_in: auth.key_in ?? "header" };
  }
  throw new Error("custom API sources support bearer or api_key authentication only");
}

/** Custom connectors use the existing GraphJin source/spec contract. */
export function declarativeGraphjinUpdate(
  bundle: SolutionPackBundle,
  inputs: Record<string, unknown>,
  secrets: Record<string, string>,
  retiredSources: string[] = [],
  bindings: Record<string, string> = {},
  api?: { specDocuments: Map<string, string>; savedQueries: Array<{ name: string; query: string }> },
  callerAccess: "authenticated" | "public" = "authenticated",
): Record<string, unknown> {
  const knownChecks = new Set(["db-connect", "db-read-only", "graphjin-reload", "analytics-smoke", "queries"]);
  for (const check of [...bundle.manifest.health.requiredPreflight, ...bundle.manifest.health.postInstall, ...bundle.manifest.health.postWriteCanary, ...Object.values(bundle.manifest.health.readiness).flat()]) {
    if (["db-connect", "analytics-smoke", "queries"].includes(check) && !bundle.artifacts.some(artifact => artifact.kind === "saved_query")) throw new Error(`${check} requires a saved query`);
    if (!bundle.manifest.artifacts.graphjin) throw new Error(`${check} requires GraphJin artifacts`);
    if (!knownChecks.has(check)) throw new Error(`unsupported pack readiness check ${check}`);
    if (check.startsWith("db-") && !bundle.artifacts.some(artifact => artifact.kind === "source" && (artifact.content as Record<string, unknown>).kind === "database")) throw new Error(`${check} requires a database source`);
  }
  for (const artifact of bundle.artifacts) {
    if (artifact.kind === "metric" || artifact.kind === "watcher") {
      const value = artifact.content as Record<string, unknown>;
      packVariables(artifact.kind === "metric" ? (value.execution as Record<string, unknown>).variables : value.variables, inputs);
      for (const signal of (value.readinessSignals ?? []) as string[]) {
        if (!Object.hasOwn(bundle.manifest.health.readiness, signal)) throw new Error(`unknown pack readiness signal ${signal}`);
      }
    }
    if (artifact.kind === "workflow") {
      const schedule = (artifact.content as Record<string, unknown>).schedule as Record<string, unknown> | null;
      if (schedule) {
        const timezone = inputs[String(schedule.timezoneInput)] ?? schedule.timezoneInput;
        try { new Intl.DateTimeFormat("en", { timeZone: String(timezone) }).format(); }
        catch { throw new Error(`workflow ${artifact.key} timezone must resolve to an IANA timezone`); }
      }
    }
    if (artifact.kind === "saved_query") {
      readQuery(String(artifact.content));
    }
    if (artifact.kind === "spec") {
      const inspect = (value: unknown): void => {
        if (!value || typeof value !== "object") return;
        for (const [key, child] of Object.entries(value)) {
          if (key === "$ref" && (typeof child !== "string" || !child.startsWith("#/"))) throw new Error("pack OpenAPI references must be local");
          inspect(child);
        }
      };
      inspect(artifact.content);
    }
  }
  const operationExposures = new Map<string, Map<string, Record<string, unknown>>>();
  for (const artifact of bundle.artifacts.filter((value) => value.kind === "source")) {
    const source = artifact.content as Record<string, unknown>;
    if (source.kind !== "api" || source.operations === undefined) continue;
    if (!source.operations || typeof source.operations !== "object" || Array.isArray(source.operations)) {
      throw new Error(`API source ${source.name} operations must be an object`);
    }
    const spec = bundle.artifacts.find(
      (value) => value.kind === "spec" && value.path === String(source.openapi),
    );
    if (!spec) throw new Error(`API source ${source.name} must reference a bundled OpenAPI spec`);
    const methods = new Map<string, string>();
    for (const path of Object.values((spec.content as { paths?: Record<string, Record<string, unknown>> }).paths ?? {})) {
      for (const [method, operation] of Object.entries(path)) {
        if (operation && typeof operation === "object" && !Array.isArray(operation)) {
          const operationId = (operation as Record<string, unknown>).operationId;
          if (typeof operationId === "string") methods.set(operationId, method.toLowerCase());
        }
      }
    }
    const exposures = new Map<string, Record<string, unknown>>();
    for (const [operationId, raw] of Object.entries(source.operations as Record<string, unknown>)) {
      if (!raw || typeof raw !== "object" || Array.isArray(raw)) throw new Error(`API operation ${operationId} override must be an object`);
      if (methods.get(operationId) !== "get") throw new Error(`API read override ${operationId} must reference a GET operation`);
      const override = raw as Record<string, unknown>;
      if (Object.keys(override).some((key) => key !== "expose_top_level") || override.expose_top_level !== true) {
        throw new Error(`API read override ${operationId} may only set expose_top_level: true`);
      }
      exposures.set(operationId, { expose_top_level: true });
    }
    operationExposures.set(String(source.name), exposures);
  }
  let exposesMutations = false;
  for (const artifact of bundle.artifacts.filter((value) => value.kind === "action")) {
    const action = artifact.content as Record<string, unknown>;
    const adapter = action.adapter as Record<string, unknown>;
    if (adapter.kind !== "graphjin_api_operation") continue;
    const sourceName = String(adapter.source);
    const source = bundle.artifacts.find(
      (value) => value.kind === "source" && String((value.content as Record<string, unknown>).name) === sourceName,
    );
    if (!source) throw new Error(`action ${artifact.key} references missing API source ${sourceName}`);
    const sourceSpec = basename(String((source.content as Record<string, unknown>).openapi), extname(String((source.content as Record<string, unknown>).openapi)));
    const specName = adapter.spec === undefined ? sourceSpec : String(adapter.spec);
    if (specName !== sourceSpec) throw new Error(`action ${artifact.key} references spec ${specName} outside source ${sourceName}`);
    const spec = bundle.artifacts.find(
      (value) => value.kind === "spec" && value.path === String((source.content as Record<string, unknown>).openapi),
    );
    const operationMethods = new Map<string, string>();
    const paths = (spec?.content as { paths?: Record<string, Record<string, unknown>> } | undefined)?.paths ?? {};
    for (const methods of Object.values(paths)) {
      for (const [method, operation] of Object.entries(methods)) {
        if (operation && typeof operation === "object" && !Array.isArray(operation)) {
          const operationId = (operation as Record<string, unknown>).operationId;
          if (typeof operationId === "string") operationMethods.set(operationId, method.toLowerCase());
        }
      }
    }
    const declared = adapter.operations && typeof adapter.operations === "object"
      ? adapter.operations as Record<string, unknown>
      : { default: adapter };
    const exposures = operationExposures.get(sourceName) ?? new Map<string, Record<string, unknown>>();
    for (const operation of Object.values(declared)) {
      if (!operation || typeof operation !== "object" || Array.isArray(operation)) continue;
      const value = operation as Record<string, unknown>;
      const operationId = String(value.operationId ?? "");
      const mutationRoot = String(value.mutationRoot ?? "");
      if (!operationId || !/^[_A-Za-z][_0-9A-Za-z]*$/.test(mutationRoot)) {
        throw new Error(`action ${artifact.key} has an invalid GraphJin API operation`);
      }
      const method = operationMethods.get(operationId);
      if (!method) throw new Error(`action ${artifact.key} references missing OpenAPI operation ${operationId}`);
      if (method === "get") throw new Error(`action ${artifact.key} cannot expose read operation ${operationId} as a mutation`);
      const exposure = {
        expose_mutation: true,
        allowed_roles: ["pack_api_executor"],
        expose_as: mutationRoot,
      };
      const current = exposures.get(operationId);
      if (current && JSON.stringify(current) !== JSON.stringify(exposure)) {
        throw new Error(`API operation ${operationId} has conflicting action exposure`);
      }
      exposures.set(operationId, exposure);
      exposesMutations = true;
    }
    operationExposures.set(sourceName, exposures);
  }

  const sources = bundle.artifacts.filter(artifact => artifact.kind === "source").flatMap<Record<string, unknown>>(artifact => {
    const authored = artifact.content as Record<string, unknown>;
    const allowed = new Set(["name", "kind", "type", "host", "port", "dbname", "user", "password", "base_url", "openapi", "auth", "read_only", "capabilities", "operations"]);
    for (const key of Object.keys(authored)) {
      if (!allowed.has(key)) throw new Error(`unsupported custom source property ${key}`);
    }
    if (bindings[artifact.key]) {
      if (Object.keys(authored).some(key => !["name", "kind", "read_only"].includes(key))) throw new Error("a source binding cannot also declare connection settings");
      return [];
    }
    const authoredAuth = authored.auth as Record<string, unknown> | undefined;
    for (const secret of [authored.password, authoredAuth?.token, authoredAuth?.key_value]) {
      if (secret !== undefined && (typeof secret !== "string" || !/^\{\{secret\.[a-z][a-z0-9_.-]+}}$/.test(secret))) {
        throw new Error("pack credentials must use declared secret references");
      }
    }
    const personal = bundle.manifest.oauth.find(c => c.scope === "user" && authoredAuth?.token === `{{secret.${c.accessToken}}}`);
    const source = packValue(personal ? { ...authored, auth: { ...authoredAuth, token: "request-bound" } } : authored, inputs, secrets) as Record<string, unknown>;
    if (source.kind === "database") {
      if (source.read_only === false) throw new Error("custom pack database sources must be read-only");
      if (!source.host || !source.dbname || !source.type) throw new Error("database source requires an explicit connection; select an existing-source binding for a source without connection settings");
      if (!["postgres", "mysql", "mariadb"].includes(String(source.type))) throw new Error("unsupported database type");
      return {
        ...source,
        name: source.name,
        kind: source.kind,
        default: false,
        read_only: true,
        access: { read: "authenticated", write: "blocked", delete: "blocked" },
        capabilities: { "data.read": true, "data.write": false, "schema.read": true, "schema.write": false },
      };
    }
    const spec = bundle.artifacts.find(artifact => artifact.kind === "spec" && artifact.path === source.openapi);
    if (!spec) throw new Error(`API source ${source.name} must reference a bundled OpenAPI spec`);
    const url = new URL(String(source.base_url));
    if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) throw new Error("API base URL must be HTTP(S) without credentials");
    const auth = source.auth as Record<string, unknown> | undefined;
    const specAuth = auth ? packApiAuth(auth, personal ? packConnectionHeader(bundle.manifest.metadata.id, personal.key) : undefined) : undefined;
    const requested = source.capabilities as Record<string, unknown> | undefined;
    if (requested && (Object.keys(requested).some(key => !["api.read", "api.write", "api.delete"].includes(key)) || Object.values(requested).some(value => typeof value !== "boolean"))) {
      throw new Error("custom API source capabilities must be boolean api.read, api.write, or api.delete values");
    }
    const write = requested?.["api.write"] === true;
    const remove = requested?.["api.delete"] === true;
    if ((write || remove) && source.read_only !== false) {
      throw new Error("custom API sources requesting write or delete capability must set read_only to false");
    }
    if (source.read_only === false && !write && !remove) {
      throw new Error("custom API sources may set read_only to false only when requesting write or delete capability");
    }
    // A GraphJin without auth sees every caller as anonymous. GraphJin grants
    // no capability and no public write to an anonymous caller, so the source
    // is read-only there and its write operations stay unexposed.
    const anonymous = callerAccess === "public";
    const writeOn = write && !anonymous;
    const removeOn = remove && !anonymous;
    const exposures = [...(operationExposures.get(String(source.name)) ?? new Map<string, Record<string, unknown>>())]
      .filter(([, exposure]) => !anonymous || exposure.expose_mutation !== true);
    return {
      name: source.name,
      kind: source.kind,
      default: false,
      read_only: !(writeOn || removeOn),
      access: {
        read: requested?.["api.read"] === false ? "blocked" : callerAccess,
        write: writeOn ? callerAccess : "blocked",
        delete: removeOn ? callerAccess : "blocked",
      },
      ...(api ? {} : { specs_dir: "/config/specs" }),
      specs: { [basename(spec.path, extname(spec.path))]: {
        ...(api ? { document: api.specDocuments.get(spec.path) } : {}),
        base_url: url.toString().replace(/\/$/, ""),
        ...(specAuth ? { auth: specAuth } : {}),
        ...(exposures.length ? { operations: Object.fromEntries(exposures) } : {}),
      } },
      capabilities: Object.fromEntries(Object.entries({
        "api.read": requested?.["api.read"] !== false,
        "api.write": writeOn,
        "api.delete": removeOn,
      }).filter(([, allowed]) => !anonymous || !allowed)),
    };
  });
  const names = Object.fromEntries(bundle.artifacts.filter(artifact => artifact.kind === "source" && bindings[artifact.key]).map(artifact => [String((artifact.content as Record<string, unknown>).name), bindings[artifact.key]]));
  const relationships = bundle.artifacts.filter(artifact => artifact.kind === "relationships").flatMap(artifact => {
    const value = artifact.content as { source: string; relationships: Array<{ left: string; right: string }> };
    return value.relationships.map(edge => ({ from: `${names[value.source] ?? value.source}:${edge.left}`, to: `${names[value.source] ?? value.source}:${edge.right}` }));
  });
  const exposesSourceMutations = sources.some(source =>
    Object.values((source.specs ?? {}) as Record<string, { operations?: Record<string, { expose_mutation?: boolean }> }>)
      .some(spec => Object.values(spec.operations ?? {}).some(operation => operation.expose_mutation === true)));
  return {
    ...(exposesMutations && exposesSourceMutations
      ? { roles: [{ name: "pack_api_executor", comment: "Short-lived executor for approved pack API actions" }] }
      : {}),
    update_sources: sources,
    ...(relationships.length ? { relationships } : {}),
    ...(api?.savedQueries.length ? { update_saved_queries: api.savedQueries } : {}),
    ...(retiredSources.length ? { source_patches: retiredSources.map(name => ({ name, read_only: true, access: { read: "blocked", write: "blocked", delete: "blocked" } })) } : {}),
  };
}

export function declarativePackPermissions(bundle: SolutionPackBundle): Record<string, string> {
  const oauth = bundle.manifest.oauth ?? [];
  const network = bundle.manifest.permissions?.network ?? [];
  const sources = bundle.artifacts
    .filter(artifact => artifact.kind === "source")
    .map(artifact => artifact.content as Record<string, unknown>);
  const database = sources.some(source => source.kind === "database") ? "read-only" : "none";
  const apiSources = sources.filter(source => source.kind === "api");
  const apiWrite = apiSources.some(source => {
    const capabilities = source.capabilities as Record<string, unknown> | undefined;
    return capabilities?.["api.write"] === true || capabilities?.["api.delete"] === true;
  })
    ? "requested; actions require an enabled policy"
    : "not requested";
  return {
    database,
    apiWrite,
    ...(oauth.length > 0
      ? { oauth: `${oauth.length} account connection; ${new Set(oauth.flatMap((connection) => connection.scopes)).size} consent scopes` }
      : {}),
    ...(network.length > 0
      ? { network: network.join(", ") }
      : {}),
  };
}

export function packPolicyControlsWrite(
  bundle: SolutionPackBundle,
  policy: Record<string, unknown>,
): boolean {
  const kinds = new Set((policy.appliesToKinds as string[] | undefined) ?? []);
  return bundle.artifacts.some(artifact => {
    if (artifact.kind !== "action") return false;
    const action = artifact.content as Record<string, unknown>;
    if (!kinds.has(String(action.kind))) return false;
    const adapter = action.adapter as Record<string, unknown> | undefined;
    return ["graphjin_api_operation", "magento_governed_operation", "magento_changeset"]
      .includes(String(adapter?.kind));
  });
}

/** Write policies start disabled; upgrades retain the administrator's current choice. */
export function installedPackPolicyEnabled(input: {
  declared: boolean;
  controlsWrite: boolean;
  existing?: boolean;
}): boolean {
  return input.existing ?? (input.controlsWrite ? false : input.declared);
}

/** Existing generated packs use these time-window variables; authored declarations override them. */
export function packVariables(value: unknown, inputs: Record<string, unknown>): Record<string, unknown> {
  const definition = packValue(value ?? {
    from: { kind: "seconds_ago", seconds: 30 * 86400 }, to: { kind: "now" }, now: { kind: "now" },
    staleBefore: { kind: "seconds_ago", seconds: 86400 }, olderThan: { kind: "seconds_ago", seconds: 2 * 86400 }, threshold: { kind: "literal", value: 0 },
  }, inputs) as Record<string, unknown>;
  resolveWatcherVariables(definition, new Date());
  return definition;
}

function readQuery(query: string) {
  const document = parse(query);
  const operations = document.definitions.filter(value => value.kind === Kind.OPERATION_DEFINITION);
  if (operations.length !== 1 || operations[0]!.operation !== "query" || document.definitions.some(value => value.kind !== Kind.OPERATION_DEFINITION && value.kind !== Kind.FRAGMENT_DEFINITION)) {
    throw new Error("custom pack saved queries must contain one read-only operation");
  }
  return document;
}

/** GraphJin routes roots by table mappings; @database alone does not select the connection.
 * Pack-owned aliases preserve response names without changing administrator table mappings. */
export function bindPackQueries(bundle: SolutionPackBundle, bindings: Record<string, string>): { bundle: SolutionPackBundle; tables: Array<Record<string, string>> } {
  const tables: Array<Record<string, string>> = [];
  const sources = bundle.artifacts.filter(value => value.kind === "source");
  const databases = sources.filter(value => (value.content as Record<string, unknown>).kind === "database");
  if (!databases.length) return { bundle, tables };
  const names = Object.fromEntries(databases.map(value => {
    const name = String((value.content as Record<string, unknown>).name);
    return [name, bindings[value.key] ?? name];
  }));
  const defaultDatabase = sources.length === 1 ? Object.keys(names)[0] : undefined;
  const resolved = { ...bundle, artifacts: bundle.artifacts.map(artifact => {
    if (artifact.kind !== "saved_query") return artifact;
    const document = readQuery(String(artifact.content));
    const roots = new Set<FieldNode>();
    const collect = (set: SelectionSetNode, seen = new Set<string>()): void => {
      for (const selection of set.selections) {
        if (selection.kind === Kind.FIELD) roots.add(selection);
        else if (selection.kind === Kind.INLINE_FRAGMENT) collect(selection.selectionSet, seen);
        else {
          const name = selection.name.value;
          const fragment = document.definitions.find(value => value.kind === Kind.FRAGMENT_DEFINITION && value.name.value === name);
          if (!fragment || fragment.kind !== Kind.FRAGMENT_DEFINITION || seen.has(name)) throw new Error("invalid saved query fragment");
          collect(fragment.selectionSet, new Set([...seen, name]));
        }
      }
    };
    for (const definition of document.definitions) if (definition.kind === Kind.OPERATION_DEFINITION) collect(definition.selectionSet);
    const bound = visit(document, { Field(node) {
      const directives = node.directives ?? [];
      const database = directives.filter(value => value.name.value === "database");
      if (database.length > 1) throw new Error("duplicate database directive");
      if (!database.length && !roots.has(node)) return;
      const argument = database[0]?.arguments?.find(value => value.name.value === "name")?.value;
      const logical = argument?.kind === Kind.STRING || argument?.kind === Kind.ENUM ? argument.value : !database.length ? defaultDatabase : undefined;
      if (!logical || !names[logical]) throw new Error(`saved query ${artifact.key} must select a declared database with @database(name: "source_name")`);
      const table = node.name.value;
      const name = `pack_${sha256(`${bundle.manifest.metadata.id}:${logical}:${names[logical]}:${table}`).slice(0, 20)}`;
      if (roots.has(node) && !tables.some(value => value.name === name)) tables.push({ name, table, source: names[logical]! });
      return { ...node, ...(roots.has(node) ? { alias: node.alias ?? node.name, name: { kind: Kind.NAME, value: name } } : {}), directives: [...directives.filter(value => value.name.value !== "database"), {
        kind: Kind.DIRECTIVE, name: { kind: Kind.NAME, value: "database" },
        arguments: [{ kind: Kind.ARGUMENT, name: { kind: Kind.NAME, value: "name" }, value: { kind: Kind.STRING, value: names[logical] } }],
      }] };
    } });
    return { ...artifact, content: print(bound) };
  }) };
  return { bundle: resolved, tables };
}

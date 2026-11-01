import { createHash } from "node:crypto";
import { lookup } from "node:dns/promises";
import { request as httpsRequest } from "node:https";
import { BlockList, isIP } from "node:net";
import { basename } from "node:path";
import { parse, stringify } from "yaml";

export const MAX_OPENAPI_SPEC_BYTES = 2 * 1024 * 1024;
const MAX_REDIRECTS = 3;
const FETCH_TIMEOUT_MS = 15_000;
const HTTP_METHODS = new Set([
  "get",
  "put",
  "post",
  "delete",
  "options",
  "head",
  "patch",
  "trace",
]);
const BLOCKED_IPV6 = new BlockList();
for (const [network, prefix] of [
  ["::", 128],
  ["::1", 128],
  ["::ffff:0:0", 96],
  ["fc00::", 7],
  ["fe80::", 10],
  ["ff00::", 8],
  ["2001:db8::", 32],
] as const) {
  BLOCKED_IPV6.addSubnet(network, prefix, "ipv6");
}

export type OpenApiSpecSummary = {
  content: string;
  checksumSha256: string;
  title: string;
  version: string | null;
  baseUrl: string;
  serverUrls: string[];
  authSchemes: string[];
  warnings: string[];
  operationCount: number;
  readOperationCount: number;
  mutatingOperationCount: number;
};

function safeManagedSourceName(sourceName: string): string {
  const source = sourceName
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 64);
  if (!source) throw new Error("API source name must contain letters or digits");
  return source;
}

/** Stable filename inside GraphJin's single managed OpenAPI specs directory. */
export function managedOpenApiSpecFilename(
  orgId: string,
  sourceName: string,
): string {
  const org = createHash("sha256").update(orgId).digest("hex").slice(0, 12);
  const source = safeManagedSourceName(sourceName).replace(/-/g, "_");
  return `org_${org}_${source}.yaml`;
}

type OpenApiDocument = Record<string, unknown> & {
  openapi?: unknown;
  info?: unknown;
  paths?: unknown;
  servers?: unknown;
  components?: unknown;
};

function asRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function ipv4Number(address: string): number | null {
  const parts = address.split(".").map(Number);
  if (parts.length !== 4 || parts.some((part) => !Number.isInteger(part) || part < 0 || part > 255)) {
    return null;
  }
  return (((parts[0]! << 24) >>> 0) + (parts[1]! << 16) + (parts[2]! << 8) + parts[3]!) >>> 0;
}

function inV4Range(value: number, network: string, prefix: number): boolean {
  const base = ipv4Number(network)!;
  const mask = prefix === 0 ? 0 : (0xffffffff << (32 - prefix)) >>> 0;
  return (value & mask) === (base & mask);
}

/** True only for globally routable addresses suitable for server-side import. */
export function isPublicAddress(address: string): boolean {
  const family = isIP(address);
  if (family === 4) {
    const value = ipv4Number(address)!;
    const blocked: Array<[string, number]> = [
      ["0.0.0.0", 8],
      ["10.0.0.0", 8],
      ["100.64.0.0", 10],
      ["127.0.0.0", 8],
      ["169.254.0.0", 16],
      ["172.16.0.0", 12],
      ["192.0.0.0", 24],
      ["192.0.2.0", 24],
      ["192.168.0.0", 16],
      ["198.18.0.0", 15],
      ["198.51.100.0", 24],
      ["203.0.113.0", 24],
      ["224.0.0.0", 4],
      ["240.0.0.0", 4],
    ];
    return !blocked.some(([network, prefix]) => inV4Range(value, network, prefix));
  }
  if (family === 6) {
    const lower = address.toLowerCase().split("%")[0]!;
    return !BLOCKED_IPV6.check(lower, "ipv6");
  }
  return false;
}

function parseImportUrl(value: string): URL {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error("OpenAPI URL must be a valid absolute URL");
  }
  if (url.protocol !== "https:") {
    throw new Error("OpenAPI URL must use HTTPS");
  }
  if (url.username || url.password) {
    throw new Error("OpenAPI URL must not include credentials");
  }
  if (url.search || url.hash) {
    throw new Error(
      "OpenAPI URL must not include a query string or fragment; upload private or signed specifications instead",
    );
  }
  return url;
}

async function resolvePublicHost(hostname: string): Promise<Array<{ address: string; family: 4 | 6 }>> {
  const results = await lookup(hostname, { all: true, verbatim: true });
  if (results.length === 0 || results.some((item) => !isPublicAddress(item.address))) {
    throw new Error("OpenAPI URL resolves to a non-public network address");
  }
  return results.map((item) => ({ address: item.address, family: item.family as 4 | 6 }));
}

async function readHttpsUrl(url: URL, redirectCount = 0): Promise<{ body: string; finalUrl: URL }> {
  const resolved = await resolvePublicHost(url.hostname);
  const selected = resolved[0]!;
  return await new Promise((resolve, reject) => {
    const req = httpsRequest(
      url,
      {
        headers: {
          accept: "application/yaml, application/json, text/yaml, text/plain;q=0.8",
          "accept-encoding": "identity",
          "user-agent": "OpenNeko-OpenAPI-Importer/1.0",
        },
        lookup: (_hostname, _options, callback) => {
          if (
            typeof _options === "object" &&
            _options !== null &&
            "all" in _options &&
            _options.all
          ) {
            (callback as unknown as (
              error: NodeJS.ErrnoException | null,
              addresses: Array<{ address: string; family: number }>,
            ) => void)(null, resolved);
          } else {
            (callback as unknown as (
              error: NodeJS.ErrnoException | null,
              address: string,
              family: number,
            ) => void)(null, selected.address, selected.family);
          }
        },
        servername: url.hostname,
      },
      (response) => {
        const status = response.statusCode ?? 0;
        if (status >= 300 && status < 400 && response.headers.location) {
          response.resume();
          if (redirectCount >= MAX_REDIRECTS) {
            reject(new Error("OpenAPI URL redirected too many times"));
            return;
          }
          let next: URL;
          try {
            next = parseImportUrl(new URL(response.headers.location, url).toString());
          } catch (error) {
            reject(error);
            return;
          }
          void readHttpsUrl(next, redirectCount + 1).then(resolve, reject);
          return;
        }
        if (status < 200 || status >= 300) {
          response.resume();
          reject(new Error(`OpenAPI URL returned HTTP ${status}`));
          return;
        }
        const declaredLength = Number(response.headers["content-length"] ?? 0);
        if (declaredLength > MAX_OPENAPI_SPEC_BYTES) {
          response.destroy();
          reject(new Error("OpenAPI document exceeds the 2 MB limit"));
          return;
        }
        const chunks: Buffer[] = [];
        let size = 0;
        response.on("data", (chunk: Buffer) => {
          size += chunk.length;
          if (size > MAX_OPENAPI_SPEC_BYTES) {
            response.destroy(new Error("OpenAPI document exceeds the 2 MB limit"));
            return;
          }
          chunks.push(chunk);
        });
        response.on("end", () => {
          resolve({ body: Buffer.concat(chunks).toString("utf8"), finalUrl: url });
        });
        response.on("error", reject);
      },
    );
    req.setTimeout(FETCH_TIMEOUT_MS, () => {
      req.destroy(new Error("OpenAPI URL fetch timed out"));
    });
    req.on("error", reject);
    req.end();
  });
}

function assertNoExternalReferences(value: unknown, seen = new Set<object>()): void {
  if (!value || typeof value !== "object") return;
  if (seen.has(value as object)) return;
  seen.add(value as object);
  if (!Array.isArray(value)) {
    const ref = (value as Record<string, unknown>).$ref;
    if (typeof ref === "string" && !ref.startsWith("#/")) {
      throw new Error(
        `external OpenAPI references are not supported (${ref.slice(0, 120)})`,
      );
    }
  }
  for (const child of Array.isArray(value) ? value : Object.values(value)) {
    assertNoExternalReferences(child, seen);
  }
}

function normalizeBaseUrl(value: string): string {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error("OpenAPI server URL must be an absolute HTTP or HTTPS URL");
  }
  if (url.protocol !== "https:" && url.protocol !== "http:") {
    throw new Error("OpenAPI server URL must use HTTP or HTTPS");
  }
  if (url.username || url.password) {
    throw new Error("OpenAPI server URL must not include credentials");
  }
  url.hash = "";
  return url.toString().replace(/\/$/, "");
}

export function inspectOpenApiSpec(input: {
  content: string;
  originalName: string;
  sourceUrl?: string | null;
  baseUrl?: string | null;
}): OpenApiSpecSummary {
  const byteLength = Buffer.byteLength(input.content, "utf8");
  if (byteLength === 0) throw new Error("OpenAPI document is empty");
  if (byteLength > MAX_OPENAPI_SPEC_BYTES) {
    throw new Error("OpenAPI document exceeds the 2 MB limit");
  }

  let parsed: unknown;
  try {
    parsed = parse(input.content, { maxAliasCount: 50, merge: false });
  } catch (error) {
    throw new Error(
      `OpenAPI document is not valid YAML or JSON: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
  const document = asRecord(parsed) as OpenApiDocument | null;
  if (!document) throw new Error("OpenAPI document root must be an object");
  const openapi = typeof document.openapi === "string" ? document.openapi.trim() : "";
  if (!/^3\.\d+(?:\.\d+)?(?:[-+].*)?$/.test(openapi)) {
    throw new Error("only OpenAPI 3.x documents are supported");
  }
  assertNoExternalReferences(document);

  const paths = asRecord(document.paths);
  if (!paths || Object.keys(paths).length === 0) {
    throw new Error("OpenAPI document must define at least one path");
  }
  let operationCount = 0;
  let readOperationCount = 0;
  let mutatingOperationCount = 0;
  for (const pathItem of Object.values(paths)) {
    const record = asRecord(pathItem);
    if (!record) continue;
    for (const method of Object.keys(record)) {
      if (!HTTP_METHODS.has(method.toLowerCase())) continue;
      operationCount += 1;
      if (["get", "head", "options"].includes(method.toLowerCase())) {
        readOperationCount += 1;
      } else {
        mutatingOperationCount += 1;
      }
    }
  }
  if (operationCount === 0) {
    throw new Error("OpenAPI document does not define any operations");
  }

  const info = asRecord(document.info);
  const title =
    (typeof info?.title === "string" && info.title.trim()) ||
    input.originalName.replace(/\.(?:ya?ml|json)$/i, "") ||
    "Imported API";
  const version = typeof info?.version === "string" && info.version.trim()
    ? info.version.trim()
    : null;

  const declaredServers = Array.isArray(document.servers)
    ? document.servers
        .map((item) => asRecord(item)?.url)
        .filter((value): value is string => typeof value === "string" && value.length > 0)
    : [];
  let baseUrl = input.baseUrl?.trim() ?? "";
  if (!baseUrl) {
    const declared = declaredServers[0];
    if (declared) {
      try {
        baseUrl = new URL(declared).toString();
      } catch {
        if (input.sourceUrl) baseUrl = new URL(declared, input.sourceUrl).toString();
      }
    }
  }
  if (!baseUrl) {
    throw new Error(
      "OpenAPI document needs an absolute server URL or a Base URL override",
    );
  }
  baseUrl = normalizeBaseUrl(baseUrl);
  document.servers = [{ url: baseUrl }];

  const components = asRecord(document.components);
  const securitySchemes = asRecord(components?.securitySchemes);
  const authSchemes = securitySchemes
    ? Object.entries(securitySchemes).map(([name, value]) => {
        const type = asRecord(value)?.type;
        return typeof type === "string" ? `${name} (${type})` : name;
      })
    : [];
  const warnings: string[] = [];
  if (mutatingOperationCount > 0) {
    warnings.push(`${mutatingOperationCount} operation${mutatingOperationCount === 1 ? " is" : "s are"} mutating`);
  }
  if (authSchemes.length === 0) warnings.push("No authentication scheme is declared");
  if (declaredServers.length > 1) {
    warnings.push(`The document declares ${declaredServers.length} servers; the selected Base URL will be used`);
  }

  const content = stringify(document, { lineWidth: 0 });
  return {
    content,
    checksumSha256: createHash("sha256").update(content).digest("hex"),
    title,
    version,
    baseUrl,
    serverUrls: declaredServers,
    authSchemes,
    warnings,
    operationCount,
    readOperationCount,
    mutatingOperationCount,
  };
}

export async function importOpenApiSpecFromUrl(input: {
  url: string;
  baseUrl?: string | null;
}): Promise<OpenApiSpecSummary & { sourceUrl: string; originalName: string }> {
  const requestedUrl = parseImportUrl(input.url.trim());
  const fetched = await readHttpsUrl(requestedUrl);
  const originalName = basename(fetched.finalUrl.pathname) || "openapi.yaml";
  return {
    ...inspectOpenApiSpec({
      content: fetched.body,
      originalName,
      sourceUrl: fetched.finalUrl.toString(),
      baseUrl: input.baseUrl,
    }),
    sourceUrl: fetched.finalUrl.toString(),
    originalName,
  };
}

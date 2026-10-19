import { z } from "zod";

const slug = z
  .string()
  .regex(/^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/, "must be a lowercase slug");
const version = z
  .string()
  .regex(/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/, "must be a semantic version");
const relativePath = z.string().min(1).superRefine((value, ctx) => {
  if (value.includes("\\")) {
    ctx.addIssue({ code: "custom", message: "must use forward slashes" });
  }
  if (value.startsWith("/") || value.split("/").some((part) => part === "..")) {
    ctx.addIssue({ code: "custom", message: "must stay within the pack root" });
  }
});

const metadataSchema = z
  .object({
    id: slug,
    name: z.string().min(1),
    version,
    publisher: slug,
    category: slug,
  })
  .strict();

const applicationCompatibilitySchema = z
  .object({
    id: slug,
    editions: z.array(slug).min(1),
    versions: z.string().min(1),
  })
  .strict();

const databaseCompatibilitySchema = z
  .object({
    engine: z.enum(["mariadb", "mysql", "postgres"]),
    versions: z.string().min(1),
  })
  .strict();

const compatibilitySchema = z
  .object({
    openneko: z.string().min(1),
    graphjin: z
      .object({
        analytics: z.string().min(1),
        operator: z.string().min(1),
      })
      .strict().optional(),
    applications: z.array(applicationCompatibilitySchema),
    databases: z.array(databaseCompatibilitySchema),
  })
  .strict();

const inputSchema = z
  .object({
    key: z.string().regex(/^[a-z][a-z0-9_.-]+$/),
    type: z.enum(["string", "url", "integer", "enum", "timezone", "boolean"]),
    required: z.boolean().optional(),
    default: z.union([z.string(), z.number(), z.boolean()]).optional(),
    discover: z.boolean().optional(),
    values: z.array(z.union([z.string(), z.number(), z.boolean()])).optional(),
    description: z.string().optional(),
  })
  .strict()
  .superRefine((input, ctx) => {
    if (input.type === "enum" && (!input.values || input.values.length === 0)) {
      ctx.addIssue({ code: "custom", message: "enum inputs require values", path: ["values"] });
    }
    if (input.type !== "enum" && input.values) {
      ctx.addIssue({ code: "custom", message: "values are valid only for enum inputs", path: ["values"] });
    }
  });

const secretSchema = z
  .object({
    key: z.string().regex(/^[a-z][a-z0-9_.-]+$/),
    purpose: z.enum([
      "graphjin_source",
      "graphjin_api_auth",
      "pack_runtime",
      "pack_oauth_client",
      "pack_oauth_token",
    ]),
    required: z.boolean().optional().default(true),
  })
  .strict();

const oauthConnectionSchema = z
  .object({
    key: slug,
    providerLabel: z.string().min(1),
    scope: z.enum(["deployment", "user"]).default("deployment"),
    experience: z.object({
      description: z.string().min(1).max(500),
      setupInstructions: z.string().min(1).max(4000).optional(),
      helpUrl: z.string().url().refine(value => new URL(value).protocol === "https:", "help URL must use HTTPS").optional(),
    }).strict().optional(),
    authorizationUrl: z.string().url(),
    tokenUrl: z.string().url(),
    userInfoUrl: z.string().url(),
    clientIdInput: z.string().min(1),
    clientSecret: z.string().min(1),
    accessToken: z.string().min(1),
    refreshToken: z.string().min(1),
    scopes: z.array(z.string().min(1)).min(1),
    authorizationParams: z.record(z.string(), z.string()).optional(),
    accountIdField: z.string().min(1).default("sub"),
    accountLabelField: z.string().min(1).default("email"),
  })
  .strict();

/**
 * Built-in channels a pack can turn on for the org while it is installed.
 * Optional rather than defaulted, so packs without it keep their manifest hash.
 */
export const PACK_CHANNELS = ["slack", "telegram", "hud"] as const;

const channelsSchema = z
  .array(z.enum(PACK_CHANNELS))
  .refine((channels) => new Set(channels).size === channels.length, "duplicate channel")
  .optional();

const permissionsSchema = z
  .object({
    network: z
      .array(z.string().regex(/^(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)*[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/i))
      .default([]),
  })
  .strict();

const graphjinArtifactsSchema = z
  .object({
    sources: relativePath,
    relationships: relativePath,
    specs: z.array(relativePath),
    savedQueries: relativePath.optional(),
  })
  .strict();

const artifactsSchema = z
  .object({
    graphjin: graphjinArtifactsSchema.optional(),
    metrics: relativePath.optional(),
    workflows: relativePath.optional(),
    watchers: relativePath.optional(),
    actions: relativePath.optional(),
    policies: relativePath.optional(),
    skills: z.array(relativePath),
  })
  .strict();

const healthSchema = z
  .object({
    requiredPreflight: z.array(slug),
    readiness: z.record(slug, z.array(slug)),
    postInstall: z.array(slug),
    postWriteCanary: z.array(slug),
  })
  .strict();

export const solutionPackManifestSchema = z
  .object({
    apiVersion: z.literal("openneko.app/v1"),
    kind: z.literal("SolutionPack"),
    metadata: metadataSchema,
    management: z.object({ label: z.string().min(1), path: z.string().regex(/^\/(?!\/)[a-zA-Z0-9/_-]*$/, "management path must be a local application path") }).strict().optional(),
    compatibility: compatibilitySchema,
    inputs: z.array(inputSchema),
    secrets: z.array(secretSchema),
    oauth: z.array(oauthConnectionSchema).default([]),
    channels: channelsSchema,
    permissions: permissionsSchema.default({ network: [] }),
    artifacts: artifactsSchema,
    health: healthSchema,
  })
  .strict()
  .superRefine((manifest, ctx) => {
    if (manifest.artifacts.graphjin && !manifest.compatibility.graphjin) {
      ctx.addIssue({ code: "custom", message: "GraphJin artifacts require GraphJin compatibility", path: ["compatibility", "graphjin"] });
    }
    for (const field of ["inputs", "secrets"] as const) {
      const seen = new Set<string>();
      manifest[field].forEach((entry, index) => {
        if (seen.has(entry.key)) {
          ctx.addIssue({ code: "custom", message: `duplicate ${field} key`, path: [field, index, "key"] });
        }
        seen.add(entry.key);
      });
    }
    const inputs = new Set(manifest.inputs.map((input) => input.key));
    const secrets = new Map(manifest.secrets.map((secret) => [secret.key, secret]));
    const hosts = new Set(manifest.permissions.network.map((host) => host.toLowerCase()));
    const connections = new Set<string>();
    manifest.oauth.forEach((connection, index) => {
      if (connections.has(connection.key)) {
        ctx.addIssue({ code: "custom", message: "duplicate OAuth connection key", path: ["oauth", index, "key"] });
      }
      connections.add(connection.key);
      if (!inputs.has(connection.clientIdInput)) {
        ctx.addIssue({ code: "custom", message: "OAuth clientIdInput must reference a declared input", path: ["oauth", index, "clientIdInput"] });
      }
      for (const field of ["clientSecret", "accessToken", "refreshToken"] as const) {
        if (!secrets.has(connection[field])) {
          ctx.addIssue({ code: "custom", message: `OAuth ${field} must reference a declared secret`, path: ["oauth", index, field] });
        }
      }
      for (const field of ["authorizationUrl", "tokenUrl", "userInfoUrl"] as const) {
        const url = new URL(connection[field]);
        if (url.protocol !== "https:") {
          ctx.addIssue({ code: "custom", message: "OAuth endpoints must use HTTPS", path: ["oauth", index, field] });
        }
        if (!hosts.has(url.hostname.toLowerCase())) {
          ctx.addIssue({ code: "custom", message: "OAuth endpoint host must be declared in permissions.network", path: ["oauth", index, field] });
        }
      }
    });
  });

export type SolutionPackManifest = z.infer<typeof solutionPackManifestSchema>;

export function parseManifest(input: unknown): SolutionPackManifest {
  return solutionPackManifestSchema.parse(input);
}

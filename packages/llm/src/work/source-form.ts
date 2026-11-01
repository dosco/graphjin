import { z } from "zod";
import type { AgentSurfaceMessage } from "../agent-backend";
import { A2UI_CATALOG_ID, A2UI_VERSION } from "./a2ui-contract";

const optionalText = (max: number) => z.string().trim().max(max).optional();

/** Optional values the agent already knows. The admin completes the rest. */
export const SOURCE_FORM_PREFILL_SHAPE = {
  name: optionalText(64),
  kind: z.enum(["database", "api", "file"]).optional(),
  type: optionalText(32).describe("Database type, such as postgres or mysql."),
  host: optionalText(255),
  port: z.number().int().min(1).max(65_535).optional(),
  dbname: optionalText(128),
  user: optionalText(128),
  secretRef: optionalText(128).describe("A stored secret name from list_source_secret_names."),
  backend: z.enum(["local", "s3", "gcs"]).optional(),
  bucket: optionalText(255),
  prefix: optionalText(255),
  region: optionalText(64),
  endpoint: optionalText(255),
  publicBaseUrl: optionalText(255),
  presignTtl: optionalText(16),
};

const sourceFormPrefillSchema = z.object(SOURCE_FORM_PREFILL_SHAPE);
export type SourceFormPrefill = z.infer<typeof sourceFormPrefillSchema>;

const SOURCE_FORM_COMPONENTS: Array<Record<string, unknown>> = [
  {"id": "root", "component": "Answer", "eyebrow": "GRAPHJIN CONFIG", "title": "Add a source", "subtitle": "Choose a source type and provide its connection details.", "children": ["note", "common", "databaseFields", "apiFields", "fileFields", "databaseSubmit", "apiSubmit", "fileSubmit"]},
  {"id": "note", "component": "Callout", "mood": "watch", "title": "Approval required", "text": "Submitting creates a configuration proposal for admin review."},
  {"id": "common", "component": "Column", "children": ["name", "kind"]},
  {"id": "name", "component": "TextField", "label": "Source name", "value": {"path": "/form/name"}, "placeholder": "warehouse"},
  {"id": "kind", "component": "ChoicePicker", "label": "Source kind", "options": [{"label": "Database", "value": "database"}, {"label": "API", "value": "api"}, {"label": "Files", "value": "file"}], "value": {"path": "/form/kind"}, "variant": "mutuallyExclusive"},
  {"id": "databaseFields", "component": "Conditional", "when": {"path": "/form/kind"}, "equals": "database", "children": ["type", "network", "database"]},
  {"id": "type", "component": "TextField", "label": "Database type", "value": {"path": "/form/type"}, "placeholder": "postgres"},
  {"id": "network", "component": "Row", "children": ["host", "port"]},
  {"id": "host", "component": "TextField", "label": "Host", "value": {"path": "/form/host"}, "placeholder": "db.internal"},
  {"id": "port", "component": "TextField", "label": "Port", "value": {"path": "/form/port"}, "variant": "number"},
  {"id": "database", "component": "Row", "children": ["dbname", "user", "secretRef"]},
  {"id": "dbname", "component": "TextField", "label": "Database", "value": {"path": "/form/dbname"}},
  {"id": "user", "component": "TextField", "label": "User", "value": {"path": "/form/user"}},
  {"id": "secretRef", "component": "TextField", "label": "Stored secret name", "value": {"path": "/form/secretRef"}, "placeholder": "warehouse-password"},
  {"id": "apiFields", "component": "Conditional", "when": {"path": "/form/kind"}, "equals": "api", "children": ["openApiSpec"]},
  {"id": "openApiSpec", "component": "OpenApiSpecInput", "label": "OpenAPI specification", "value": {"path": "/form/openApiSpec"}},
  {"id": "fileFields", "component": "Conditional", "when": {"path": "/form/kind"}, "equals": "file", "children": ["fileSecurity", "backend", "localFields", "objectFields", "s3Fields"]},
  {"id": "fileSecurity", "component": "Callout", "mood": "watch", "title": "Read-only source", "text": "Authenticated organization members receive list/read access to approved file content."},
  {"id": "backend", "component": "ChoicePicker", "label": "Storage backend", "options": [{"label": "Local", "value": "local"}, {"label": "S3", "value": "s3"}, {"label": "GCS", "value": "gcs"}], "value": {"path": "/form/backend"}, "variant": "mutuallyExclusive"},
  {"id": "localFields", "component": "Conditional", "when": {"path": "/form/backend"}, "equals": "local", "children": ["managedFiles"]},
  {"id": "managedFiles", "component": "ManagedFileSourceInput", "label": "Managed local files", "sourceName": {"path": "/form/name"}, "value": {"path": "/form/localFiles"}},
  {"id": "objectFields", "component": "Conditional", "when": {"path": "/form/backend"}, "oneOf": ["s3", "gcs"], "children": ["cloudCredentials", "bucket", "prefix", "publicBaseUrl", "presignTtl"]},
  {"id": "cloudCredentials", "component": "Callout", "mood": "watch", "title": "Deployment credentials", "text": "GraphJin uses its AWS or Google Cloud runtime identity; chat and approval carry storage metadata."},
  {"id": "bucket", "component": "TextField", "label": "Bucket", "value": {"path": "/form/bucket"}},
  {"id": "prefix", "component": "TextField", "label": "Key prefix", "value": {"path": "/form/prefix"}},
  {"id": "publicBaseUrl", "component": "TextField", "label": "Public HTTPS base URL (optional)", "value": {"path": "/form/publicBaseUrl"}},
  {"id": "presignTtl", "component": "TextField", "label": "Presigned URL TTL", "value": {"path": "/form/presignTtl"}, "placeholder": "15m"},
  {"id": "s3Fields", "component": "Conditional", "when": {"path": "/form/backend"}, "equals": "s3", "children": ["region", "endpoint"]},
  {"id": "region", "component": "TextField", "label": "Region", "value": {"path": "/form/region"}},
  {"id": "endpoint", "component": "TextField", "label": "S3-compatible endpoint", "value": {"path": "/form/endpoint"}},
  {"id": "submitLabel", "component": "Text", "text": "Review proposal"},
  {"id": "databaseSubmit", "component": "Conditional", "when": {"path": "/form/kind"}, "equals": "database", "children": ["submitDatabase"]},
  {"id": "apiSubmit", "component": "Conditional", "when": {"path": "/form/kind"}, "equals": "api", "children": ["submitApi"]},
  {"id": "fileSubmit", "component": "Conditional", "when": {"path": "/form/kind"}, "equals": "file", "children": ["localSubmit", "s3Submit", "gcsSubmit"]},
  {"id": "localSubmit", "component": "Conditional", "when": {"path": "/form/backend"}, "equals": "local", "children": ["submitLocalFile"]},
  {"id": "s3Submit", "component": "Conditional", "when": {"path": "/form/backend"}, "equals": "s3", "children": ["submitCloudFile"]},
  {"id": "gcsSubmit", "component": "Conditional", "when": {"path": "/form/backend"}, "equals": "gcs", "children": ["submitCloudFile"]},
  {"id": "submitDatabase", "component": "Button", "child": "submitLabel", "variant": "primary", "requires": {"path": "/form/name"}, "action": {"event": {"name": "submit_source_config", "context": {"prompt": "Review the selected source kind and its relevant values, then file the register_source configuration proposal.", "values": {"path": "/form"}}}}},
  {"id": "submitApi", "component": "Button", "child": "submitLabel", "variant": "primary", "requires": [{"path": "/form/name"}, {"path": "/form/openApiSpec/id"}], "action": {"event": {"name": "submit_source_config", "context": {"prompt": "Use openApiSpec.id as specAssetId and file the register_source API configuration proposal.", "values": {"path": "/form"}}}}},
  {"id": "submitLocalFile", "component": "Button", "child": "submitLabel", "variant": "primary", "requires": [{"path": "/form/name"}, {"path": "/form/localFiles/sourceName"}], "action": {"event": {"name": "submit_source_config", "context": {"prompt": "Create the read-only local file source proposal using the managed localFiles manifest and selected source name.", "values": {"path": "/form"}}}}},
  {"id": "submitCloudFile", "component": "Button", "child": "submitLabel", "variant": "primary", "requires": [{"path": "/form/name"}, {"path": "/form/bucket"}], "action": {"event": {"name": "submit_source_config", "context": {"prompt": "Create the read-only cloud file source proposal using the selected backend, bucket, prefix, endpoint, public base URL, and bounded presign TTL values.", "values": {"path": "/form"}}}}},
];

/** Build the canonical source configuration form with the agent's prefill. */
export function buildSourceFormSurface(
  prefill: SourceFormPrefill,
  surfaceId: string,
): AgentSurfaceMessage[] {
  const { kind, backend, ...fields } = sourceFormPrefillSchema.parse(prefill);
  const form = {
    name: "",
    kind: [kind ?? "database"],
    type: "postgres",
    host: "",
    port: 5432,
    dbname: "",
    user: "",
    secretRef: "",
    openApiSpec: null,
    backend: [backend ?? "local"],
    localFiles: null,
    bucket: "",
    prefix: "",
    region: "",
    endpoint: "",
    publicBaseUrl: "",
    presignTtl: "15m",
    ...Object.fromEntries(Object.entries(fields).filter(([, value]) => value !== undefined)),
  };
  return [{
    version: A2UI_VERSION,
    createSurface: {
      surfaceId,
      catalogId: A2UI_CATALOG_ID,
      dataModel: { form },
      components: SOURCE_FORM_COMPONENTS,
    },
  } as AgentSurfaceMessage];
}

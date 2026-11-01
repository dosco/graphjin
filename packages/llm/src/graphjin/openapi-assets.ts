import {
  and,
  db,
  desc,
  eq,
  openapi_spec_asset,
} from "@neko/db";
import {
  importOpenApiSpecFromUrl,
  inspectOpenApiSpec,
  type OpenApiSpecSummary,
} from "./openapi-spec";

export type OpenApiSpecAssetView = {
  id: string;
  originalName: string;
  sourceType: "url" | "upload";
  title: string;
  version: string | null;
  baseUrl: string;
  operationCount: number;
  readOperationCount: number;
  mutatingOperationCount: number;
  authSchemes: string[];
  warnings: string[];
  checksumSha256: string;
  status: string;
  sourceName: string | null;
  createdAt: string;
};

type StoredAsset = typeof openapi_spec_asset.$inferSelect;

export function openApiSpecAssetView(row: StoredAsset): OpenApiSpecAssetView {
  return {
    id: row.id,
    originalName: row.original_name,
    sourceType: row.source_type === "url" ? "url" : "upload",
    title: row.title,
    version: row.version,
    baseUrl: row.base_url,
    operationCount: row.operation_count,
    readOperationCount: row.read_operation_count,
    mutatingOperationCount: row.mutating_operation_count,
    authSchemes: row.auth_schemes,
    warnings: row.warnings,
    checksumSha256: row.checksum_sha256,
    status: row.status,
    sourceName: row.source_name,
    createdAt: row.created_at.toISOString(),
  };
}

async function persistAsset(input: {
  orgId: string;
  createdByUserId?: string | null;
  sourceType: "url" | "upload";
  originalName: string;
  sourceUrl?: string | null;
  summary: OpenApiSpecSummary;
}): Promise<OpenApiSpecAssetView> {
  const [row] = await db()
    .insert(openapi_spec_asset)
    .values({
      org_id: input.orgId,
      created_by_user_id: input.createdByUserId ?? null,
      source_type: input.sourceType,
      original_name: input.originalName.slice(0, 500),
      source_url: input.sourceUrl ?? null,
      content: input.summary.content,
      checksum_sha256: input.summary.checksumSha256,
      document_format: "yaml",
      title: input.summary.title.slice(0, 500),
      version: input.summary.version?.slice(0, 200) ?? null,
      base_url: input.summary.baseUrl,
      server_urls: input.summary.serverUrls.slice(0, 20),
      auth_schemes: input.summary.authSchemes.slice(0, 50),
      warnings: input.summary.warnings.slice(0, 20),
      operation_count: input.summary.operationCount,
      read_operation_count: input.summary.readOperationCount,
      mutating_operation_count: input.summary.mutatingOperationCount,
    })
    .returning();
  if (!row) throw new Error("OpenAPI asset could not be stored");
  return openApiSpecAssetView(row);
}

export async function createOpenApiSpecAssetFromUpload(input: {
  orgId: string;
  createdByUserId?: string | null;
  originalName: string;
  content: string;
  baseUrl?: string | null;
}): Promise<OpenApiSpecAssetView> {
  const summary = inspectOpenApiSpec({
    content: input.content,
    originalName: input.originalName,
    baseUrl: input.baseUrl,
  });
  return persistAsset({
    orgId: input.orgId,
    createdByUserId: input.createdByUserId,
    sourceType: "upload",
    originalName: input.originalName,
    summary,
  });
}

export async function createOpenApiSpecAssetFromUrl(input: {
  orgId: string;
  createdByUserId?: string | null;
  url: string;
  baseUrl?: string | null;
}): Promise<OpenApiSpecAssetView> {
  const imported = await importOpenApiSpecFromUrl({
    url: input.url,
    baseUrl: input.baseUrl,
  });
  return persistAsset({
    orgId: input.orgId,
    createdByUserId: input.createdByUserId,
    sourceType: "url",
    originalName: imported.originalName,
    sourceUrl: imported.sourceUrl,
    summary: imported,
  });
}

export async function listOpenApiSpecAssets(input: {
  orgId: string;
  limit?: number;
}): Promise<OpenApiSpecAssetView[]> {
  const rows = await db()
    .select()
    .from(openapi_spec_asset)
    .where(eq(openapi_spec_asset.org_id, input.orgId))
    .orderBy(desc(openapi_spec_asset.created_at))
    .limit(Math.min(Math.max(input.limit ?? 20, 1), 100));
  return rows.map(openApiSpecAssetView);
}

export async function getOpenApiSpecAsset(input: {
  orgId: string;
  assetId: string;
}): Promise<StoredAsset | null> {
  const [row] = await db()
    .select()
    .from(openapi_spec_asset)
    .where(
      and(
        eq(openapi_spec_asset.org_id, input.orgId),
        eq(openapi_spec_asset.id, input.assetId),
      ),
    )
    .limit(1);
  return row ?? null;
}

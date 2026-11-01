import { NextRequest, NextResponse } from "next/server";
import { getGraphjinConfigSettingsForOrg } from "@neko/db";
import {
  createOpenApiSpecAssetFromUpload,
  createOpenApiSpecAssetFromUrl,
  listOpenApiSpecAssets,
} from "@neko/llm/graphjin/openapi-assets";
import { MAX_OPENAPI_SPEC_BYTES } from "@neko/llm/graphjin/openapi-spec";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

function unavailable() {
  // Keep this administrative surface undiscoverable to non-admin members.
  return NextResponse.json({ error: "not found" }, { status: 404 });
}

function cleanBaseUrl(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

export async function GET() {
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return unavailable();
  const orgId = await getOrgId();
  if (!(await getGraphjinConfigSettingsForOrg(orgId)).sourceConfigEnabled) {
    return unavailable();
  }
  const assets = await listOpenApiSpecAssets({ orgId });
  return NextResponse.json({ assets });
}

export async function POST(request: NextRequest) {
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return unavailable();
  const orgId = await getOrgId();
  if (!(await getGraphjinConfigSettingsForOrg(orgId)).sourceConfigEnabled) {
    return unavailable();
  }

  try {
    const contentType = request.headers.get("content-type") ?? "";
    if (contentType.includes("multipart/form-data")) {
      const form = await request.formData();
      const file = form.get("file");
      if (!(file instanceof File)) {
        return NextResponse.json(
          { error: "an OpenAPI YAML or JSON file is required" },
          { status: 400 },
        );
      }
      if (file.size === 0 || file.size > MAX_OPENAPI_SPEC_BYTES) {
        return NextResponse.json(
          { error: "OpenAPI file must be between 1 byte and 2 MB" },
          { status: 400 },
        );
      }
      if (!/\.(?:ya?ml|json)$/i.test(file.name)) {
        return NextResponse.json(
          { error: "OpenAPI file must have a .yaml, .yml, or .json extension" },
          { status: 400 },
        );
      }
      const asset = await createOpenApiSpecAssetFromUpload({
        orgId,
        createdByUserId: actor.userId,
        originalName: file.name,
        content: await file.text(),
        baseUrl: cleanBaseUrl(form.get("baseUrl")),
      });
      return NextResponse.json({ asset }, { status: 201 });
    }

    const body = (await request.json().catch(() => null)) as Record<
      string,
      unknown
    > | null;
    const url = typeof body?.url === "string" ? body.url.trim() : "";
    if (!url) {
      return NextResponse.json(
        { error: "url is required for a hosted OpenAPI import" },
        { status: 400 },
      );
    }
    const asset = await createOpenApiSpecAssetFromUrl({
      orgId,
      createdByUserId: actor.userId,
      url,
      baseUrl: cleanBaseUrl(body?.baseUrl),
    });
    return NextResponse.json({ asset }, { status: 201 });
  } catch (error) {
    return NextResponse.json(
      { error: error instanceof Error ? error.message : "OpenAPI import failed" },
      { status: 400 },
    );
  }
}

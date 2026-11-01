import "server-only";

import { pool } from "@neko/db";
import { getOrgId } from "@/lib/db";

const PUBLIC_URL_SCOPE = "public-url";

export type PublicUrlSetting = {
  saved: string | null;
  environment: string | null;
};

export function normalizePublicUrl(input: string): string | null {
  let url: URL;
  try {
    url = new URL(input.trim());
  } catch {
    return null;
  }
  if (url.protocol !== "https:" && url.protocol !== "http:") return null;
  if (url.search || url.hash || url.username || url.password) return null;
  return `${url.origin}${url.pathname}`.replace(/\/+$/, "");
}

export async function getPublicUrlSetting(orgId: string): Promise<PublicUrlSetting> {
  const { rows } = await pool().query<{ url: string | null }>(
    "select config->>'url' as url from llm_provider_config where org_id = $1 and scope = $2",
    [orgId, PUBLIC_URL_SCOPE],
  );
  const environment = process.env.OPENNEKO_PUBLIC_URL?.trim().replace(/\/+$/, "") || null;
  return { saved: rows[0]?.url || null, environment };
}

export async function getPublicUrl(orgId: string): Promise<string | null> {
  const setting = await getPublicUrlSetting(orgId);
  return setting.saved ?? setting.environment;
}

export async function savePublicUrl(orgId: string, url: string | null): Promise<void> {
  if (!url) {
    await pool().query("delete from llm_provider_config where org_id = $1 and scope = $2", [orgId, PUBLIC_URL_SCOPE]);
    return;
  }
  await pool().query(
    `insert into llm_provider_config (org_id, scope, provider, config)
     values ($1, $2, $2, jsonb_build_object('url', $3::text))
     on conflict (org_id, scope) do update set config = excluded.config, updated_at = now()`,
    [orgId, PUBLIC_URL_SCOPE, url],
  );
}

export async function withSavedPublicUrl(path: string): Promise<string> {
  const { saved } = await getPublicUrlSetting(await getOrgId());
  return saved ? `${saved}${path}` : path;
}

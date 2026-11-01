import { readdir, readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { parseManifest } from "@neko/packs/manifest";
import { parse } from "yaml";

// Provider metadata mirrors ../plugins; account state is synthetic and actions are disabled.
export const pluginConnectionsFixture = {
  workspace: [{ pluginId: "scalekit", pluginName: "scalekit", providerLabel: "Scalekit workspace", scopes: ["wks:read", "wks:write", "env:read", "env:write", "org:read", "org:write"], flow: "mcp-oauth", credentialScope: "deployment", connected: true, connectedAt: "2026-09-01T10:00:00.000Z" }],
  connectors: [{ pluginId: "google-workspace", pluginName: "@open-neko/connector-google-workspace", providerLabel: "Google Workspace", scopes: ["openid", "https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/gmail.send", "https://www.googleapis.com/auth/calendar.events.readonly", "https://www.googleapis.com/auth/spreadsheets", "https://www.googleapis.com/auth/documents"], flow: "oauth2-pkce", credentialScope: "operator", connected: false, connectedAt: null }],
};

// Visual testing uses the actual bundled declarations; only account state is synthetic.
export async function personalConnectionsFixture(state: string) {
  const root = resolve(process.cwd(), "../../packs");
  const entries = await readdir(root, { withFileTypes: true });
  const packs = await Promise.all(entries.filter(entry => entry.isDirectory() && entry.name !== "schema").map(entry => readFile(resolve(root, entry.name, "pack.yaml"), "utf8").then(raw => ({ manifest: parseManifest(parse(raw)) }))));
  return packs.flatMap(pack => pack.manifest.oauth.filter(connection => connection.scope === "user").map(connection => ({
    packId: pack.manifest.metadata.id,
    key: connection.key,
    providerLabel: connection.providerLabel,
    experience: connection.experience,
    scopes: connection.scopes,
    configured: state !== "unconfigured",
    connected: state === "connected",
    accountLabel: state === "connected" ? "synthetic-user@example.test" : null,
  })));
}

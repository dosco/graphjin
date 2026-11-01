// Scalekit directory (SCIM) adapter for OpenNeko's directory sync.
// Endpoints follow the Scalekit REST API:
//   POST {env}/oauth/token                                             — client_credentials
//   GET  {env}/api/v1/organizations/{org}/directories
//   GET  {env}/api/v1/organizations/{org}/directories/{dir}/groups
//   GET  {env}/api/v1/organizations/{org}/directories/{dir}/users
//   POST {env}/api/v1/organizations/{org}/users                        — create user + membership

import type {
  ApplyDirectoryChangeParams,
  ApplyDirectoryChangeResult,
  DirectoryGroup,
  DirectoryMembership,
  DirectoryUser,
  ListDirectoryParams,
  ListDirectoryResult,
} from "../../directory/types.js";
import {
  DEFAULT_TIMEOUT_MS,
  normalizeEnvironmentUrl,
  scalekitRequest,
  ScalekitApiError,
} from "./client.js";

const PAGE_SIZE = 100;

export interface ScalekitDirectoryOptions {
  environmentUrl: string;
  clientId: string;
  clientSecret: string;
  organizationId: string;
  fetchImpl?: typeof fetch;
  timeoutMs?: number;
}

type Json = Record<string, unknown>;

interface Cursor {
  directory: number;
  phase: "groups" | "users";
  pageToken: string;
}

function field(obj: unknown, snake: string, camel: string): unknown {
  if (!obj || typeof obj !== "object") return undefined;
  const record = obj as Json;
  return record[snake] ?? record[camel];
}

function text(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function encodeCursor(cursor: Cursor): string {
  return Buffer.from(JSON.stringify(cursor)).toString("base64url");
}

function decodeCursor(raw: string | null | undefined): Cursor {
  if (!raw) return { directory: 0, phase: "groups", pageToken: "" };
  try {
    const parsed = JSON.parse(Buffer.from(raw, "base64url").toString("utf8")) as Cursor;
    if (Number.isInteger(parsed.directory) && (parsed.phase === "groups" || parsed.phase === "users")) {
      return { directory: parsed.directory, phase: parsed.phase, pageToken: text(parsed.pageToken) };
    }
  } catch {
    // handled below
  }
  throw new ScalekitApiError("directory cursor is not valid", null, null);
}

/** Sign-in claims carry group names, so the name is the group key. */
function groupKey(group: unknown): string {
  return text(field(group, "display_name", "displayName")) || text(field(group, "id", "id"));
}

function toUser(raw: unknown): DirectoryUser | null {
  const emails = field(raw, "emails", "emails");
  const email = (text(field(raw, "email", "email")) || (Array.isArray(emails) ? text(emails[0]) : "")).toLowerCase();
  const id = text(field(raw, "id", "id"));
  if (!id || !email) return null;
  const name =
    [text(field(raw, "given_name", "givenName")), text(field(raw, "family_name", "familyName"))].filter(Boolean).join(" ") ||
    text(field(raw, "preferred_username", "preferredUsername")) ||
    null;
  const active = field(field(raw, "user_detail", "userDetail"), "active", "active");
  return { externalId: id, sub: null, email, name, active: active !== false };
}

export function createScalekitDirectory(options: ScalekitDirectoryOptions) {
  const envUrl = normalizeEnvironmentUrl(options.environmentUrl);
  const organizationId = options.organizationId.trim();
  if (!organizationId) throw new ScalekitApiError("SCALEKIT_ORGANIZATION_ID is empty", null, null);
  const fetchImpl = options.fetchImpl ?? fetch;
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const orgPath = `/api/v1/organizations/${encodeURIComponent(organizationId)}`;
  let token: Promise<string> | null = null;

  function accessToken(): Promise<string> {
    token ??= (async () => {
      const body = new URLSearchParams({
        grant_type: "client_credentials",
        client_id: options.clientId,
        client_secret: options.clientSecret,
      });
      const result = await scalekitRequest<Json>(
        fetchImpl,
        timeoutMs,
        new URL("/oauth/token", envUrl).toString(),
        {
          method: "POST",
          headers: { "Content-Type": "application/x-www-form-urlencoded", Accept: "application/json" },
          body: body.toString(),
        },
        "client credentials token",
      );
      const value = text(result.access_token);
      if (!value) throw new ScalekitApiError("Scalekit client credentials token returned no access_token", null, null);
      return value;
    })();
    token.catch(() => {
      token = null;
    });
    return token;
  }

  async function api<T>(path: string, description: string, init: RequestInit = {}, query: Record<string, string> = {}): Promise<T> {
    const url = new URL(path, envUrl);
    for (const [key, value] of Object.entries(query)) if (value) url.searchParams.set(key, value);
    return scalekitRequest<T>(
      fetchImpl,
      timeoutMs,
      url.toString(),
      {
        ...init,
        headers: {
          Authorization: `Bearer ${await accessToken()}`,
          Accept: "application/json",
          ...(init.body ? { "Content-Type": "application/json" } : {}),
        },
      },
      description,
    );
  }

  async function directoryIds(): Promise<string[]> {
    const result = await api<Json>(`${orgPath}/directories`, "list directories");
    const directories = Array.isArray(result.directories) ? result.directories : [];
    return directories
      .filter((d) => field(d, "enabled", "enabled") !== false)
      .map((d) => text(field(d, "id", "id")))
      .filter(Boolean)
      .sort();
  }

  async function list(params: ListDirectoryParams): Promise<ListDirectoryResult> {
    const cursor = decodeCursor(params.cursor);
    const ids = await directoryIds();
    const empty = { tenantId: organizationId, users: [], groups: [], memberships: [] };
    const directoryId = ids[cursor.directory];
    if (!directoryId) return { ...empty, nextCursor: null };

    const query = { page_size: String(PAGE_SIZE), page_token: cursor.pageToken };
    const path = `${orgPath}/directories/${encodeURIComponent(directoryId)}/${cursor.phase}`;
    const page = await api<Json>(path, `list directory ${cursor.phase}`, {}, cursor.phase === "users" ? { ...query, include_detail: "true" } : query);
    const pageToken = text(field(page, "next_page_token", "nextPageToken"));
    const next: Cursor | null = pageToken
      ? { ...cursor, pageToken }
      : cursor.phase === "groups"
        ? { directory: cursor.directory, phase: "users", pageToken: "" }
        : cursor.directory + 1 < ids.length
          ? { directory: cursor.directory + 1, phase: "groups", pageToken: "" }
          : null;
    const nextCursor = next ? encodeCursor(next) : null;

    if (cursor.phase === "groups") {
      const groups: DirectoryGroup[] = (Array.isArray(page.groups) ? page.groups : [])
        .map((g) => ({ externalId: groupKey(g), name: text(field(g, "display_name", "displayName")) || null }))
        .filter((g) => g.externalId);
      return { ...empty, groups, nextCursor };
    }

    const users: DirectoryUser[] = [];
    const memberships: DirectoryMembership[] = [];
    for (const raw of Array.isArray(page.users) ? page.users : []) {
      const user = toUser(raw);
      if (!user) continue;
      users.push(user);
      const groups = field(raw, "groups", "groups");
      for (const group of Array.isArray(groups) ? groups : []) {
        const key = groupKey(group);
        if (key) memberships.push({ userExternalId: user.externalId, groupExternalId: key });
      }
    }
    return { ...empty, users, memberships, nextCursor };
  }

  async function apply(params: ApplyDirectoryChangeParams): Promise<ApplyDirectoryChangeResult> {
    const change = params.change;
    if (change.op !== "create_user") {
      throw new ScalekitApiError(`Scalekit directory does not support "${change.op}"; the identity provider owns user status`, null, null);
    }
    const result = await api<Json>(
      `${orgPath}/users`,
      "create organization user",
      {
        method: "POST",
        body: JSON.stringify({ email: change.email, ...(change.name ? { user_profile: { name: change.name } } : {}) }),
      },
      { send_invitation_email: "true" },
    );
    const user = (result.user ?? {}) as Json;
    const profile = field(user, "user_profile", "userProfile");
    if (!text(user.id)) return { user: null };
    return {
      user: {
        externalId: text(user.id),
        sub: null,
        email: text(user.email) || change.email,
        name: text(field(profile, "name", "name")) || change.name || null,
        active: true,
      },
    };
  }

  return { list, apply };
}

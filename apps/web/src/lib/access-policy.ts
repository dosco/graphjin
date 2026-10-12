/**
 * One table for who may reach each route. The proxy applies it to every
 * request, and a path with no entry is refused, so a new route cannot ship
 * open by accident (test/lib/access-policy.test.ts fails on a missing entry).
 *
 * Route handlers keep their own checks. This table decides who reaches a
 * route; the handler still decides what that caller may see inside it,
 * including which items they hold.
 *
 * Rules:
 * - `public`: no session, e.g. the sign-in flow.
 * - `token`: the route authenticates its own caller, e.g. a workflow API key.
 * - `signed-in`: a valid session. Solo installs have one operator and pass.
 * - `admin`: a member of Administrators.
 *
 * Prefixes cover an area, so a new route inside one inherits its rule. A new
 * top-level area has no entry, and the test names it until it is listed.
 */
export type AccessRule = "public" | "token" | "signed-in" | "admin";

export interface AccessPolicy {
  /** Path prefix, matched against the whole path or a following "/". */
  prefix: string;
  rule: AccessRule;
  why?: string;
}

/** Longest prefix wins, so a narrower entry can sit inside a broader one. */
export const ACCESS_POLICIES: AccessPolicy[] = [
  { prefix: "/signin", rule: "public" },
  { prefix: "/api/auth", rule: "public", why: "the sign-in flow itself" },
  { prefix: "/api/sso", rule: "public", why: "SSO setup runs before the first sign-in" },
  { prefix: "/admin/settings/sso", rule: "public", why: "SSO setup page; the page checks the actor" },
  { prefix: "/api/version", rule: "public" },
  { prefix: "/api/health", rule: "public" },
  { prefix: "/api/v1", rule: "token", why: "workflow API bearer tokens" },
  { prefix: "/api/hooks", rule: "token", why: "Reckon-compatible webhooks carry X-Webhook-Token" },
  { prefix: "/api/channels", rule: "token", why: "channel webhooks carry a provider signature" },
  { prefix: "/api/integrations/connect", rule: "token", why: "OAuth callbacks carry the state cookie" },
  { prefix: "/api/pack-accounts", rule: "token", why: "pack OAuth callbacks carry the state cookie" },

  { prefix: "/admin", rule: "admin" },
  { prefix: "/dev", rule: "admin", why: "development previews; the pages return 404 in production" },
  { prefix: "/api/admin", rule: "admin" },
  { prefix: "/api/settings", rule: "admin" },
  { prefix: "/api/policies", rule: "admin" },
  { prefix: "/api/plugins", rule: "admin" },
  { prefix: "/api/onboarding", rule: "admin" },
  { prefix: "/onboarding", rule: "admin" },
  { prefix: "/business-profile", rule: "admin" },
  { prefix: "/settings", rule: "admin", why: "older paths that redirect into /admin" },

  { prefix: "/", rule: "signed-in", why: "the home briefing; this entry matches only the home path" },
  { prefix: "/a", rule: "signed-in", why: "records apps; the records engine applies app grants" },
  { prefix: "/actions", rule: "signed-in" },
  { prefix: "/apps", rule: "signed-in" },
  { prefix: "/approvals", rule: "signed-in" },
  { prefix: "/integrations", rule: "signed-in" },
  { prefix: "/library", rule: "signed-in" },
  { prefix: "/memory", rule: "signed-in" },
  { prefix: "/runs", rule: "signed-in" },
  { prefix: "/skills", rule: "signed-in" },
  { prefix: "/chat", rule: "signed-in" },
  { prefix: "/work", rule: "signed-in" },
  { prefix: "/workflows", rule: "signed-in" },
  { prefix: "/api/a", rule: "signed-in", why: "records apps; the records engine applies app grants" },
  { prefix: "/api/action-requests", rule: "signed-in" },
  { prefix: "/api/approvals", rule: "signed-in" },
  { prefix: "/api/briefing", rule: "signed-in" },
  { prefix: "/api/insights", rule: "signed-in" },
  { prefix: "/api/integrations", rule: "signed-in" },
  { prefix: "/api/library", rule: "signed-in" },
  { prefix: "/api/my", rule: "signed-in" },
  { prefix: "/api/observations", rule: "signed-in" },
  { prefix: "/api/profile", rule: "signed-in" },
  { prefix: "/api/settings/persona", rule: "signed-in", why: "each person edits their own persona; the route allows only their own row" },
  { prefix: "/profile", rule: "signed-in" },
  { prefix: "/api/subscriptions", rule: "signed-in" },
  { prefix: "/api/bots", rule: "signed-in" },
  { prefix: "/api/work", rule: "signed-in" },
  { prefix: "/api/workflow-runs", rule: "signed-in" },
  { prefix: "/api/workflows", rule: "signed-in" },
];

const byLength = [...ACCESS_POLICIES].sort((a, b) => b.prefix.length - a.prefix.length);

/** Null for a path no entry covers. The proxy refuses those. */
export function accessPolicyFor(pathname: string): AccessPolicy | null {
  const path = pathname.length > 1 && pathname.endsWith("/") ? pathname.slice(0, -1) : pathname;
  return byLength.find((policy) => path === policy.prefix || (policy.prefix !== "/" && path.startsWith(`${policy.prefix}/`))) ?? null;
}

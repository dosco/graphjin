/**
 * GET /api/integrations/connect/[plugin]/start
 *
 * Begin the OAuth flow for the current operator against the named
 * connect-capable plugin. Mints CSRF state + PKCE code_verifier,
 * stashes both in a signed cookie, asks the plugin to build the
 * authorization URL (via the worker admin port), then 302s the
 * browser there. The matching /callback finishes the dance.
 */

import { requireItem } from "@/lib/entitlements";
import { NextRequest, NextResponse } from "next/server";
import { getCurrentUser } from "@/lib/auth";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import {
  beginConnect,
  buildConnectCallbackUri,
  DEPLOYMENT_CONNECT_OPERATOR_ID,
  listConnectProviders,
  newPkceVerifier,
  newStateToken,
  writeConnectStateCookie,
} from "@/lib/integrations";

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ plugin: string }> },
) {
  const { plugin } = await params;
  const pluginName = decodeURIComponent(plugin);
  if (!pluginName) {
    return NextResponse.json({ error: "plugin param required" }, { status: 400 });
  }
  // Verify the plugin is installed + declares connect; gives the operator
  // a clear 404 instead of a worker-side error when they hit a stale link.
  const providers = await listConnectProviders();
  const provider = providers.find((p) => p.pluginName === pluginName);
  if (!provider) {
    return NextResponse.json(
      { error: `connect plugin "${pluginName}" not installed` },
      { status: 404 },
    );
  }
  // Deployment-scoped connectors are org-level: an admin can authorize
  // them before any SSO session exists (solo mode). Per-operator
  // connectors still require a signed-in user.
  if (provider.credentialScope === "deployment") {
    const actor = await requireAdminActor();
    if (isDenied(actor)) return actor;
  } else {
    const user = await getCurrentUser();
    if (!user) {
      return NextResponse.json({ error: "not signed in" }, { status: 401 });
    }
    const deniedIntegration = await requireItem("integration", pluginName, { notFound: `connect plugin "${pluginName}" not installed` });
    if (deniedIntegration) return deniedIntegration;
  }
  // Deployment-scoped connectors use a reserved slot — the worker ignores
  // the operator id and stores one org-wide credential.
  const operatorId =
    provider.credentialScope === "deployment"
      ? DEPLOYMENT_CONNECT_OPERATOR_ID
      : (await getCurrentUser())!.id;
  const url = new URL(request.url);
  const returnTo = url.searchParams.get("returnTo") ?? "/integrations";
  const state = newStateToken();
  const codeVerifier = newPkceVerifier();
  await writeConnectStateCookie({
    pluginName,
    operatorId,
    state,
    codeVerifier,
    returnPath: returnTo,
  });
  const redirectUri = buildConnectCallbackUri(request.url, pluginName);
  try {
    const result = await beginConnect(pluginName, {
      operatorId,
      redirectUri,
      state,
      scopes: provider.scopes,
      codeVerifier,
    });
    if (result.oauthState) {
      // mcp-oauth: the plugin's in-flight state must survive the browser
      // round-trip — stash it in a second signed cookie for the callback.
      await writeConnectStateCookie({
        pluginName,
        operatorId,
        state,
        codeVerifier,
        returnPath: returnTo,
        oauthState: result.oauthState,
      });
    }
    return NextResponse.redirect(result.authorizationUrl, { status: 302 });
  } catch (err) {
    return NextResponse.json(
      { error: err instanceof Error ? err.message : String(err) },
      { status: 502 },
    );
  }
}

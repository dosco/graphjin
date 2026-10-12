/**
 * GET /api/integrations/connect/[plugin]/callback
 *
 * Finish the OAuth dance. Steps:
 *   1. Read the state cookie. If absent / forged, fail closed.
 *   2. Verify the plugin in the URL matches the plugin in the cookie.
 *   3. Verify the state query param matches the cookie's state.
 *   4. Call completeConnect with code + redirect_uri + state + verifier;
 *      worker stores the credential under the operator's slot.
 *   5. Redirect back to the originating returnPath with ?connected=<plugin>.
 */

import { NextRequest } from "next/server";
import {
  buildConnectCallbackUri,
  completeConnect,
  listConnectProviders,
  readAndClearConnectStateCookie,
} from "@/lib/integrations";
import { appRedirect } from "@/lib/public-url";

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ plugin: string }> },
) {
  const url = new URL(request.url);
  const error = url.searchParams.get("error");
  if (error) {
    const description = url.searchParams.get("error_description") ?? error;
    return appRedirect(`/integrations?error=${encodeURIComponent(description)}`);
  }
  const { plugin } = await params;
  const pluginName = decodeURIComponent(plugin);
  const code = url.searchParams.get("code");
  const state = url.searchParams.get("state");
  if (!code || !state) {
    return appRedirect("/integrations?error=missing+code+or+state");
  }
  const stored = await readAndClearConnectStateCookie();
  if (!stored) {
    return appRedirect("/integrations?error=state+cookie+missing+or+expired");
  }
  if (stored.pluginName !== pluginName) {
    return appRedirect("/integrations?error=plugin+mismatch");
  }
  if (stored.state !== state) {
    return appRedirect("/integrations?error=state+mismatch");
  }
  // Re-verify the plugin is still installed (rare race; operator might've
  // removed it during the IdP dance).
  const providers = await listConnectProviders();
  const provider = providers.find((p) => p.pluginName === pluginName);
  if (!provider) {
    return appRedirect(`/integrations?error=plugin+removed`);
  }
  try {
    await completeConnect(pluginName, {
      operatorId: stored.operatorId,
      code,
      redirectUri: buildConnectCallbackUri(request.url, pluginName),
      state,
      codeVerifier: stored.codeVerifier,
      scopes: provider.scopes,
      ...(stored.oauthState ? { oauthState: stored.oauthState } : {}),
    });
    return appRedirect(`${stored.returnPath}?connected=${encodeURIComponent(pluginName)}`);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    return appRedirect(`${stored.returnPath}?error=${encodeURIComponent(message)}`);
  }
}

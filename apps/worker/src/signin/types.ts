/** Normalized identity a sign-in provider returns after a successful callback. */
export interface AuthIdentity {
  /** Stable subject identifier from the IdP. Treated as the primary key. */
  sub: string;
  email: string;
  name?: string | null;
  /** IdP tenant, e.g. the Scalekit organization id. */
  orgId?: string | null;
  /** Group memberships from the IdP; empty for self-asserted providers. */
  groups: Array<string | { id: string; name?: string | null }>;
}

export interface BeginAuthParams {
  /** Where the provider sends the user after sign-in; OpenNeko's callback. */
  redirectUri: string;
  /** CSRF token the web minted; forwarded verbatim as the OAuth state. */
  state: string;
  /** Email or domain hint typed on the sign-in page. */
  loginHint?: string | null;
}

export interface CompleteAuthParams {
  code: string;
  redirectUri: string;
  state: string;
}

export interface BeginConnectParams {
  operatorId: string;
  redirectUri: string;
  state: string;
  scopes?: string[];
  codeVerifier?: string | null;
}

export interface CompleteConnectParams {
  operatorId: string;
  code: string;
  redirectUri: string;
  state: string;
  scopes?: string[];
  codeVerifier?: string | null;
  /** Opaque in-flight state returned by begin, echoed back by the web. */
  oauthState?: string;
}

/** Sign-in values a provider reads from its secrets-store section. */
export interface SignInSetting {
  key: string;
  required: boolean;
  secret: boolean;
  /** The host generates the value (an HMAC key nobody types). */
  autogenerate?: boolean;
}

"use client";

import { useEffect, useState } from "react";
import { toast } from "sonner";
import AppHeader from "@/components/AppHeader";
import CreatorCredit from "@/components/CreatorCredit";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";
import { Button, ButtonLink } from "@/components/ui/button";
import { Field, Input, NativeSelect } from "@/components/ui/field";
import type {
  SsoEnvironmentRow,
  SsoOrganizationRow,
  SsoSetupStatus,
  SsoSetupStatusResult,
} from "@/lib/sso-setup";
import { copyText } from "@/lib/copy-text";

const copyClass = "text-ui-body-sm text-text3 leading-[1.45]";

function StepHeader({
  step,
  done,
  title,
}: {
  step: number;
  done: boolean;
  title: string;
}) {
  return (
    <div className="flex items-center gap-2">
      {done ? (
        <span
          className="flex items-center justify-center w-6 h-6 rounded-full bg-accent text-bg text-ui-caption font-bold"
          aria-label="done"
        >
          ✓
        </span>
      ) : (
        <span className="flex items-center justify-center w-6 h-6 rounded-full border-[1.5px] border-text3 text-text2 text-ui-caption font-semibold">
          {step}
        </span>
      )}
      <span className="text-ui-subsection font-semibold text-text">{title}</span>
      {done ? (
        <span className="text-ui-label font-semibold text-accent">Done</span>
      ) : null}
    </div>
  );
}

export default function SsoSetupForm({
  initial,
  loadError,
  workspaceConnected,
}: {
  initial: SsoSetupStatusResult | null;
  loadError: string | null;
  workspaceConnected: boolean;
}) {
  const [status, setStatus] = useState<SsoSetupStatusResult | null>(initial);
  const [environmentId, setEnvironmentId] = useState(
    initial?.environmentId ?? "",
  );
  const [organizationId, setOrganizationId] = useState(
    initial?.organizationId ?? "",
  );
  const [tier, setTier] = useState<string>(initial?.environmentTier ?? "dev");
  const [envOptions, setEnvOptions] = useState<SsoEnvironmentRow[]>([]);
  const [orgOptions, setOrgOptions] = useState<SsoOrganizationRow[]>([]);
  const [pickerBusy, setPickerBusy] = useState(false);
  const [editingStep, setEditingStep] = useState<number | null>(null);
  const [environmentUrl, setEnvironmentUrl] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [generating, setGenerating] = useState(false);
  const [checking, setChecking] = useState(false);
  const [savingIds, setSavingIds] = useState(false);
  const [savingCreds, setSavingCreds] = useState(false);
  const [autofilling, setAutofilling] = useState(false);

  async function autofillCredentials() {
    if (!environmentId) return;
    setAutofilling(true);
    try {
      const res = await fetch(
        `/api/sso/setup/credentials-info?environmentId=${encodeURIComponent(environmentId)}`,
        { cache: "no-store" },
      );
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { error?: string } | null;
        throw new Error(body?.error ?? `HTTP ${res.status}`);
      }
      const out = (await res.json()) as {
        environmentUrl: string;
        clientId: string;
      };
      setEnvironmentUrl(out.environmentUrl);
      setClientId(out.clientId);
      toast.success("Environment URL and client id filled in. Now paste the secret.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setAutofilling(false);
    }
  }

  // Once the workspace is connected, fetch the real environments and
  // organizations so the admin picks from a list instead of pasting ids.
  useEffect(() => {
    if (!workspaceConnected || envOptions.length > 0) return;
    let cancelled = false;
    (async () => {
      setPickerBusy(true);
      try {
        const res = await fetch("/api/sso/setup/environments", {
          cache: "no-store",
        });
        if (!res.ok) {
          const body = (await res.json().catch(() => null)) as { error?: string } | null;
          throw new Error(body?.error ?? `HTTP ${res.status}`);
        }
        const { environments } = (await res.json()) as {
          environments: SsoEnvironmentRow[];
        };
        if (cancelled) return;
        setEnvOptions(environments);
        if (environments.length > 0 && !initial?.environmentId) {
          // Dev by default — free trial.
          const dev =
            environments.find((e) => e.tier === "DEV") ?? environments[0]!;
          setEnvironmentId(dev.id);
          setTier(dev.tier === "PRD" ? "prod" : "dev");
        }
      } catch {
        // Fetch failure → fall back to manual id entry below.
      } finally {
        if (!cancelled) setPickerBusy(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [workspaceConnected, envOptions.length, initial?.environmentId]);

  async function loadOrganizations(envId: string) {
    if (!envId) return;
    try {
      const res = await fetch(
        `/api/sso/setup/organizations?environmentId=${encodeURIComponent(envId)}`,
        { cache: "no-store" },
      );
      if (!res.ok) return;
      const { organizations } = (await res.json()) as {
        organizations: SsoOrganizationRow[];
      };
      setOrgOptions(organizations);
      if (organizations.length > 0 && !initial?.organizationId) {
        setOrganizationId(organizations[0]!.id);
      }
    } catch {
      // fall back to manual entry
    }
  }

  // When the environment picker changes, refresh the organization list.
  useEffect(() => {
    if (envOptions.length > 0 && environmentId) {
      const initial = window.setTimeout(
        () => void loadOrganizations(environmentId),
        0,
      );
      return () => window.clearTimeout(initial);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [environmentId, envOptions.length]);

  function onEnvSelect(nextId: string) {
    setEnvironmentId(nextId);
    const env = envOptions.find((e) => e.id === nextId);
    if (env) setTier(env.tier === "PRD" ? "prod" : "dev");
  }

  async function saveIds() {
    setSavingIds(true);
    try {
      const res = await fetch("/api/sso/setup/ids", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ environmentId, organizationId, tier }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { error?: string } | null;
        throw new Error(body?.error ?? `HTTP ${res.status}`);
      }
      setStatus((s) => ({
        ...(s ?? emptyStatus()),
        environmentId,
        organizationId,
        environmentTier: tier,
      }));
      setEditingStep(null);
      toast.success("Environment set.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setSavingIds(false);
    }
  }

  async function saveCredentials() {
    setSavingCreds(true);
    try {
      const res = await fetch("/api/sso/setup/credentials", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ environmentUrl, clientId, clientSecret }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { error?: string } | null;
        throw new Error(body?.error ?? `HTTP ${res.status}`);
      }
      setStatus((s) => ({ ...(s ?? emptyStatus()), signInConfigured: true }));
      setClientSecret("");
      setEditingStep(null);
      toast.success("Credentials saved. Sign-in is now live.");
      // The login gate engages the moment credentials are saved. Send the
      // admin to /signin (back here afterwards) so they finish setup in a
      // real session — the first sign-in becomes the admin.
      setTimeout(() => {
        window.location.href = "/signin?returnTo=%2Fadmin%2Fsettings%2Fsso";
      }, 900);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setSavingCreds(false);
    }
  }

  async function generate() {
    setGenerating(true);
    try {
      const res = await fetch("/api/sso/setup/generate-portal-link", {
        method: "POST",
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { error?: string } | null;
        throw new Error(body?.error ?? `HTTP ${res.status}`);
      }
      const out = (await res.json()) as {
        portalLink: string;
        expiresAt: string | null;
      };
      setStatus((s) => ({
        ...(s ?? emptyStatus()),
        status: "awaiting_portal",
        portalLink: out.portalLink,
        portalLinkExpiresAt: out.expiresAt,
        lastError: null,
      }));
      toast.success("Portal link generated.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setGenerating(false);
    }
  }

  async function check() {
    setChecking(true);
    try {
      const res = await fetch("/api/sso/setup/check", { method: "POST" });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { error?: string } | null;
        throw new Error(body?.error ?? `HTTP ${res.status}`);
      }
      const out = (await res.json()) as {
        status: SsoSetupStatus;
        connection: SsoSetupStatusResult["connection"];
        lastError: string | null;
        setupCompletedAt: string | null;
      };
      setStatus((s) => ({
        ...(s ?? emptyStatus()),
        status: out.status,
        connection: out.connection,
        lastError: out.lastError,
        setupCompletedAt: out.setupCompletedAt,
      }));
      if (out.status === "connected") {
        toast.success("SSO is live.");
      } else if (out.status === "awaiting_portal") {
        toast.info("No connection yet. Finish the IdP setup in the portal.");
      } else if (out.status === "failed") {
        toast.error(out.lastError ?? "Connection check failed.");
      } else {
        toast.info("Still in progress.");
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e));
    } finally {
      setChecking(false);
    }
  }

  async function copyLink() {
    if (!status?.portalLink) return;
    try {
      await copyText(status.portalLink);
      toast.success("Link copied.");
    } catch {
      toast.error("Could not copy. Select the link and copy manually.");
    }
  }

  const step1Done = workspaceConnected;
  const step2Done = Boolean(status?.environmentId && status?.organizationId);
  const step3Done = status?.signInConfigured === true;
  const step4Done = status?.status === "connected";

  return (
    <>
      <div
        className="root"
      >
        <AppHeader back={{ href: "/admin/settings", label: "Settings" }}>
          <SectionNav current="admin" />
        </AppHeader>

        <PageHeading
          title="Single sign-on"
          description="Connect your identity provider (Okta, Entra ID, and others) through Scalekit. The agent fetches the environment URL and client id for you; you paste the client secret once."
        />

        <section className="flex flex-col gap-4">
          {loadError ? (
            <div className="rounded-xl border border-border bg-bg px-4 py-3">
              <p className={copyClass}>{loadError}</p>
            </div>
          ) : null}

          {status?.lastError && status.status === "failed" ? (
            <div className="rounded-xl border border-red-300 bg-bg px-4 py-3">
              <p className={copyClass}>{status.lastError}</p>
            </div>
          ) : null}

          {/* Step 1 — workspace authorization */}
          <div className="settings-card !mt-0 flex flex-col gap-3">
            <StepHeader
              step={1}
              done={step1Done}
              title="Authorize the Scalekit workspace"
            />
            {step1Done ? (
              <p className={copyClass}>
                Connected. The agent manages your Scalekit workspace
                (environments, organizations, users, connections) with this
                org-wide authorization.
              </p>
            ) : (
              <>
                <p className={copyClass}>
                  Opens Scalekit in your browser. Sign in or create the account
                  there; the consent screen names the scopes and endpoint.
                </p>
                <ButtonLink
                  href="/api/integrations/connect/scalekit/start?returnTo=%2Fadmin%2Fsettings%2Fsso"
                >
                  Connect Scalekit workspace
                </ButtonLink>
              </>
            )}
          </div>

          {/* Step 2 — environment selection */}
          <div className="settings-card !mt-0 flex flex-col gap-3">
            <StepHeader
              step={2}
              done={step2Done}
              title="Select the Scalekit environment"
            />
            {step2Done && editingStep !== 2 ? (
              <>
                <p className={copyClass}>
                  {status?.environmentTier === "prod" ? "Production" : "Development"}
                  {" · "}
                  {status?.environmentId}
                  {" · org "}
                  {status?.organizationId}
                </p>
                <div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => setEditingStep(2)}
                  >
                    Change
                  </Button>
                </div>
              </>
            ) : (
              <>
                <p className={copyClass}>
                  {pickerBusy
                    ? "Loading your Scalekit environments…"
                    : envOptions.length > 0
                      ? "These are the environments in your workspace. Development is the free trial. Switch to Production when you go live. Environments are isolated, so you re-run the IdP setup there."
                      : workspaceConnected
                        ? "Couldn't load the environment list. Paste the ids from the Scalekit dashboard instead."
                        : "Connect the workspace in step 1 and the list loads automatically."}
                </p>
                <div className="flex gap-2 flex-wrap items-center">
                  {pickerBusy ? (
                    <span className="flex items-center gap-2 text-ui-body-sm text-text2">
                      <span
                        className="inline-block w-4 h-4 border-2 border-border border-t-accent rounded-full animate-spin"
                        aria-hidden="true"
                      />
                      Loading environments…
                    </span>
                  ) : envOptions.length > 0 ? (
                    <NativeSelect
                      className="flex-1 min-w-[240px]"
                      value={environmentId}
                      onChange={(e) => onEnvSelect(e.target.value)}
                    >
                      {envOptions.map((e) => (
                        <option key={e.id} value={e.id}>
                          {e.name ?? e.id} ({e.tier === "PRD" ? "Production" : "Development"})
                          {e.domain ? ` · ${e.domain}` : ""}
                        </option>
                      ))}
                    </NativeSelect>
                  ) : (
                    <NativeSelect
                      className="max-w-[180px]"
                      value={tier}
                      onChange={(e) => setTier(e.target.value)}
                    >
                      <option value="dev">Development</option>
                      <option value="prod">Production</option>
                    </NativeSelect>
                  )}
                  {envOptions.length > 0 && orgOptions.length > 0 ? (
                    <NativeSelect
                      className="flex-1 min-w-[200px]"
                      value={organizationId}
                      onChange={(e) => setOrganizationId(e.target.value)}
                    >
                      {orgOptions.map((o) => (
                        <option key={o.id} value={o.id}>
                          {o.name ?? o.id}
                        </option>
                      ))}
                    </NativeSelect>
                  ) : null}
                </div>
                {envOptions.length === 0 && (
                  <div className="grid grid-cols-2 gap-4 max-[720px]:grid-cols-1">
                    <Field label="Environment ID" htmlFor="sso-environment-id">
                      <Input
                        id="sso-environment-id"
                        placeholder="env_…"
                        value={environmentId}
                        onChange={(e) => setEnvironmentId(e.target.value)}
                        spellCheck={false}
                        autoComplete="off"
                      />
                    </Field>
                    <Field label="Organization ID" htmlFor="sso-organization-id">
                      <Input
                        id="sso-organization-id"
                        placeholder="org_…"
                        value={organizationId}
                        onChange={(e) => setOrganizationId(e.target.value)}
                        spellCheck={false}
                        autoComplete="off"
                      />
                    </Field>
                  </div>
                )}
                <div className="flex justify-end">
                  <Button
                    type="button"
                    variant="secondary"
                    onClick={saveIds}
                    disabled={
                      savingIds || pickerBusy || !environmentId || !organizationId
                    }
                  >
                    {savingIds ? "Saving…" : "Set environment"}
                  </Button>
                </div>
              </>
            )}
          </div>

          {/* Step 3 — sign-in credentials */}
          <div className="settings-card !mt-0 flex flex-col gap-3">
            <StepHeader step={3} done={step3Done} title="Sign-in credentials" />
            {step3Done && editingStep !== 3 ? (
              <>
                <p className={copyClass}>Stored in the encrypted vault.</p>
                <div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => setEditingStep(3)}
                  >
                    Replace
                  </Button>
                </div>
              </>
            ) : (
              <>
                <p className={copyClass}>
                  The agent fetches the environment URL and client id for you.
                  The client secret is shown only once, in Scalekit, under
                  Settings → API Credentials for the selected environment.
                  Pasting it here stores it straight in the vault, never through
                  chat.
                </p>
                <div className="flex gap-2 flex-wrap items-center">
                  <Button
                    type="button"
                    variant="secondary"
                    onClick={autofillCredentials}
                    disabled={autofilling || !environmentId}
                  >
                    {autofilling ? "Fetching…" : "Auto-fill from Scalekit"}
                  </Button>
                  <ButtonLink
                    href={
                      environmentId
                        ? `https://app.scalekit.com/ws/environments/${encodeURIComponent(environmentId)}/settings/api-credentials`
                        : "https://app.scalekit.com"
                    }
                    target="_blank"
                    rel="noreferrer"
                  >
                    Find the secret in Scalekit ↗
                  </ButtonLink>
                </div>
                <div className="grid grid-cols-2 gap-4 max-[720px]:grid-cols-1">
                  <Field label="Environment URL" htmlFor="sso-environment-url">
                    <Input
                      id="sso-environment-url"
                      placeholder="https://your-app.scalekit.com"
                      value={environmentUrl}
                      onChange={(e) => setEnvironmentUrl(e.target.value)}
                      spellCheck={false}
                      autoComplete="off"
                    />
                  </Field>
                  <Field label="Client ID" htmlFor="sso-client-id">
                    <Input
                      id="sso-client-id"
                      placeholder="skc_…"
                      value={clientId}
                      onChange={(e) => setClientId(e.target.value)}
                      spellCheck={false}
                      autoComplete="off"
                    />
                  </Field>
                  <Field label="Client secret" htmlFor="sso-client-secret" className="col-span-full">
                    <Input
                      id="sso-client-secret"
                      type="password"
                      value={clientSecret}
                      onChange={(e) => setClientSecret(e.target.value)}
                      autoComplete="off"
                      spellCheck={false}
                    />
                  </Field>
                </div>
                <div className="flex justify-end">
                  <Button
                    type="button"
                    variant="secondary"
                    onClick={saveCredentials}
                    disabled={
                      savingCreds || !environmentUrl || !clientId || !clientSecret
                    }
                  >
                    {savingCreds ? "Saving…" : "Save credentials"}
                  </Button>
                </div>
              </>
            )}
          </div>

          {/* Step 4 — IdP connection */}
          <div className="settings-card !mt-0 flex flex-col gap-3">
            <StepHeader
              step={4}
              done={step4Done}
              title="Connect the identity provider"
            />
            {step4Done ? (
              <p className={copyClass}>
                SSO is live
                {status?.provider ? ` via ${status.provider}` : ""}
                {status?.setupCompletedAt
                  ? ` · connected ${new Date(status.setupCompletedAt).toLocaleString()}`
                  : ""}
              </p>
            ) : (
              <>
                <p className={copyClass}>
                  Optional for the trial. Sign-in already works through
                  Scalekit’s hosted login. Generate a portal link to connect
                  your company’s own IdP (Okta, Entra ID…): open it, follow the
                  guided setup, and the agent polls the connection until it’s
                  live.
                </p>
                {status?.portalLink ? (
                  <div className="flex flex-col gap-2">
                    <p className={copyClass}>
                      The link is single-use and expires in about a minute.
                      Once opened it stays valid for the session.
                    </p>
                    <div className="flex items-center gap-2">
                      <code className="flex-1 truncate text-ui-body-sm text-text2">
                        {status.portalLink}
                      </code>
                      <Button type="button" variant="secondary" onClick={copyLink}>
                        Copy
                      </Button>
                      <ButtonLink
                        href={status.portalLink}
                        target="_blank"
                        rel="noreferrer"
                      >
                        Open
                      </ButtonLink>
                    </div>
                  </div>
                ) : null}
                {status?.connection ? (
                  <p className={copyClass}>
                    Provider: {status.connection.provider ?? "unknown"} ·{" "}
                    {status.connection.status}
                    {status.connection.enabled ? " · enabled" : ""}
                  </p>
                ) : null}
                <div className="flex gap-2">
                  <Button type="button" onClick={generate} disabled={generating}>
                    {generating ? "Generating…" : "Generate portal link"}
                  </Button>
                  <Button
                    type="button"
                    variant="secondary"
                    onClick={check}
                    disabled={checking}
                  >
                    {checking ? "Checking…" : "Check connection"}
                  </Button>
                </div>
              </>
            )}
          </div>
        </section>
      </div>

      <CreatorCredit />
    </>
  );
}

function emptyStatus(): SsoSetupStatusResult {
  return {
    status: "not_started",
    portalLink: null,
    portalLinkExpiresAt: null,
    provider: null,
    connection: null,
    lastError: null,
    setupCompletedAt: null,
    environmentId: null,
    organizationId: null,
    environmentTier: null,
    signInConfigured: false,
  };
}

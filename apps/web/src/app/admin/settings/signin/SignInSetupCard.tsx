"use client";

import { useCallback, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input, NativeSelect } from "@/components/ui/field";

const MAGIC_LINK = "magic-link";

const PROVIDERS = [
  { id: "resend", label: "Resend", key: "RESEND_API_KEY" },
  { id: "postmark", label: "Postmark", key: "POSTMARK_SERVER_TOKEN" },
  { id: "sendgrid", label: "SendGrid", key: "SENDGRID_API_KEY" },
] as const;

type ProviderId = (typeof PROVIDERS)[number]["id"];

interface GateInfo {
  pluginName: string;
  providerLabel: string;
  provisioning: string;
  loginHintRequired: boolean;
}

interface Status {
  provider: GateInfo | null;
  pending:
    | (GateInfo & {
        missingEnv: string[];
        needsAdminUser: boolean;
        configuredEnv: string[];
      })
    | null;
  users: { total: number; activeAdmins: number };
  selfEmail: string | null;
  selfNeedsEmail: boolean;
}

/**
 * One-page magic-link setup: live/pending status with the exact
 * blockers, email delivery config, first users, and a delivery test.
 * Every write re-fetches status, so the "Sign-in is live" banner flips
 * the moment the last blocker clears.
 */
export default function SignInSetupCard() {
  const [status, setStatus] = useState<Status | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    try {
      const res = await fetch("/api/admin/signin/status", { cache: "no-store" });
      if (!res.ok) throw new Error(`status read failed (${res.status})`);
      setStatus((await res.json()) as Status);
      setLoadError(null);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    const initial = window.setTimeout(() => void reload(), 0);
    return () => window.clearTimeout(initial);
  }, [reload]);

  if (loadError) {
    return (
      <section className="settings-card">
        <p className="text-sm text-danger" role="alert">
          {loadError}
        </p>
      </section>
    );
  }
  if (!status) {
    return (
      <section className="settings-card">
        <p className="text-sm text-text2" role="status">
          Loading sign-in status…
        </p>
      </section>
    );
  }

  const gate = status.provider ?? status.pending;
  if (!gate) {
    return (
      <section className="settings-card">
        <div className="settings-card-head">
          <div>
            <h2 className="settings-card-title">Email-link sign-in is off</h2>
            <p className="settings-card-copy">
              Turn it on, then set up email delivery and add the people who
              may sign in. The deployment stays in single-operator mode until
              setup is complete.
            </p>
          </div>
        </div>
        <UseMagicLinkButton label="Turn on email-link sign-in" onDone={reload} />
      </section>
    );
  }

  if (gate.pluginName !== MAGIC_LINK) {
    return (
      <section className="settings-card">
        <div className="settings-card-head">
          <div>
            <h2 className="settings-card-title">
              {gate.providerLabel} is the sign-in provider
            </h2>
            <p className="settings-card-copy">
              Configure it on the{" "}
              <a href="/admin/settings/sso" className="underline">
                Single sign-on
              </a>{" "}
              page. Switching to email-link sign-in limits sign-in to the
              users you add here.
            </p>
          </div>
        </div>
        <UseMagicLinkButton label="Use email-link sign-in instead" onDone={reload} />
      </section>
    );
  }

  return (
    <>
      <StatusBanner status={status} />
      <DeliverySection status={status} onSaved={reload} />
      <UsersSection status={status} onSaved={reload} />
      <TestSection status={status} />
    </>
  );
}

function UseMagicLinkButton({ label, onDone }: { label: string; onDone: () => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function choose() {
    setBusy(true);
    setError(null);
    try {
      const res = await fetch("/api/admin/signin/provider", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ provider: MAGIC_LINK }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { error?: string } | null;
        throw new Error(body?.error ?? `could not turn on email-link sign-in (${res.status})`);
      }
      await onDone();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-3">
      <Button type="button" variant="primary" disabled={busy} onClick={() => void choose()}>
        {busy ? "Turning on…" : label}
      </Button>
      {error ? (
        <span className="text-sm text-danger" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}

function StatusBanner({ status }: { status: Status }) {
  if (status.provider) {
    return (
      <section className="settings-card">
        <div className="settings-card-head">
          <div>
            <h2 className="settings-card-title">Sign-in is live</h2>
            <p className="settings-card-copy">
              Provisioned users can sign in at <code>/signin</code> with an
              emailed link. Manage who gets in on the{" "}
              <a href="/admin/users" className="underline">
                Users
              </a>{" "}
              page.
            </p>
          </div>
          <div className="settings-source">
            <strong className="is-ok">Live</strong>
          </div>
        </div>
      </section>
    );
  }
  const pending = status.pending!;
  const blockers: string[] = [];
  const missing = new Set(pending.missingEnv);
  const providerConfigured = PROVIDERS.some((p) =>
    pending.configuredEnv.includes(p.key),
  );
  if (!providerConfigured) blockers.push("choose an email provider below");
  if (missing.has("MAGIC_LINK_FROM")) blockers.push("set the From address");
  if (pending.needsAdminUser) blockers.push("add at least one admin user");
  for (const key of pending.missingEnv) {
    if (key !== "MAGIC_LINK_FROM" && key !== "MAGIC_LINK_SIGNING_SECRET") {
      blockers.push(`set ${key}`);
    }
  }
  return (
    <section className="settings-card">
      <div className="settings-card-head">
        <div>
          <h2 className="settings-card-title">Sign-in is not live yet</h2>
          <p className="settings-card-copy">
            The deployment stays in single-operator mode until setup is
            complete. Nobody can be locked out by a half-finished
            configuration. Remaining:{" "}
            {blockers.length > 0 ? blockers.join(" · ") : "finishing up…"}
          </p>
        </div>
        <div className="settings-source">
          <strong className="is-warn">Setup</strong>
        </div>
      </div>
    </section>
  );
}

function DeliverySection({
  status,
  onSaved,
}: {
  status: Status;
  onSaved: () => Promise<void>;
}) {
  const configured = status.pending?.configuredEnv ?? [];
  const active = PROVIDERS.find((p) => configured.includes(p.key));
  const [provider, setProvider] = useState<ProviderId>(active?.id ?? "resend");
  const [apiKey, setApiKey] = useState("");
  const [from, setFrom] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const keyStored =
    configured.includes(PROVIDERS.find((p) => p.id === provider)!.key) ||
    (Boolean(status.provider) && active?.id === provider);

  async function save(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      const res = await fetch("/api/admin/signin/email-provider", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          provider,
          apiKey: apiKey || undefined,
          from,
        }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as {
          error?: string;
        } | null;
        throw new Error(body?.error ?? `save failed (${res.status})`);
      }
      setApiKey("");
      setSaved(true);
      await onSaved();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="settings-card !mt-5">
      <div className="settings-card-head">
        <div>
          <h2 className="settings-card-title">Email delivery</h2>
          <p className="settings-card-copy">
            The sign-in link is emailed through one provider. The signing
            secret is generated and stored automatically. You never handle
            it.
          </p>
        </div>
        {active ? (
          <div className="settings-source">
            <strong className="is-ok">{active.label} configured</strong>
          </div>
        ) : null}
      </div>

      {error ? (
        <div className="mb-3 rounded-md bg-danger-soft px-3 py-2 text-sm text-danger" role="alert">
          {error}
        </div>
      ) : null}
      {saved ? (
        <div className="mb-3 rounded-md bg-success-soft px-3 py-2 text-sm text-success-ink" role="status">
          Delivery settings saved.
        </div>
      ) : null}

      <form onSubmit={save} className="flex flex-wrap items-end gap-3">
        <label className="flex flex-col gap-1 text-xs font-semibold text-text2">
          Provider
          <NativeSelect
            value={provider}
            onChange={(event) => setProvider(event.target.value as ProviderId)}
          >
            {PROVIDERS.map((p) => (
              <option key={p.id} value={p.id}>
                {p.label}
              </option>
            ))}
          </NativeSelect>
        </label>
        <label className="flex flex-col gap-1 text-xs font-semibold text-text2">
          API key
          <Input
            type="password"
            value={apiKey}
            onChange={(event) => setApiKey(event.target.value)}
            placeholder={keyStored ? "(stored, paste to replace)" : "paste the API key"}
            autoComplete="off"
            spellCheck={false}
          />
        </label>
        <label className="flex flex-col gap-1 text-xs font-semibold text-text2">
          From address
          <Input
            type="text"
            required
            value={from}
            onChange={(event) => setFrom(event.target.value)}
            placeholder="OpenNeko <signin@company.com>"
            className="w-72"
            spellCheck={false}
          />
        </label>
        <Button
          type="submit"
          variant="primary"
          disabled={busy}
        >
          {busy ? "Saving…" : "Save delivery settings"}
        </Button>
      </form>
      <p className="mt-2 text-xs text-text3">
        The From address must be a sender verified with the provider you
        pick.
      </p>
    </section>
  );
}

function UsersSection({
  status,
  onSaved,
}: {
  status: Status;
  onSaved: () => Promise<void>;
}) {
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<"admin" | "member">(
    status.users.activeAdmins === 0 ? "admin" : "member",
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function addUser(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = await fetch("/api/admin/users", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          email,
          role: status.selfNeedsEmail ? "admin" : role,
          ...(status.selfNeedsEmail ? { updateSoloAccount: true } : {}),
        }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as {
          error?: string;
        } | null;
        throw new Error(body?.error ?? `create failed (${res.status})`);
      }
      setEmail("");
      await onSaved();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="settings-card !mt-5">
      <div className="settings-card-head">
        <div>
          <h2 className="settings-card-title">Who can sign in</h2>
          <p className="settings-card-copy">
            Only users listed here can request a link. Everyone else gets
            silently ignored. {status.users.total} provisioned,{" "}
            {status.users.activeAdmins} active admin
            {status.users.activeAdmins === 1 ? "" : "s"}. Full management on
            the{" "}
            <a href="/admin/users" className="underline">
              Users
            </a>{" "}
            page.
          </p>
        </div>
      </div>

      {status.users.activeAdmins === 0 ? (
        <div
          className="mb-3 rounded-md px-3 py-2 text-sm"
          style={{ background: "var(--warn-soft)", color: "var(--warn-ink)" }}
          role="alert"
        >
          Add <strong>your own email as admin first</strong>. Sign-in stays
          off until an admin exists, so you cannot lock yourself out.
        </div>
      ) : null}
      {status.selfNeedsEmail ? (
        <p className="mb-3 text-sm text-text2">
          Add your email to the existing admin account before inviting anyone else.
        </p>
      ) : null}
      {error ? (
        <div className="mb-3 rounded-md bg-danger-soft px-3 py-2 text-sm text-danger" role="alert">
          {error}
        </div>
      ) : null}

      <form onSubmit={addUser} className="flex flex-wrap items-end gap-3">
        <label className="flex flex-col gap-1 text-xs font-semibold text-text2">
          Email
          <Input
            type="email"
            required
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            placeholder="person@company.com"
            className="w-72"
            autoComplete="email"
          />
        </label>
        {!status.selfNeedsEmail && (
          <label className="flex flex-col gap-1 text-xs font-semibold text-text2">
            Role
            <NativeSelect
              value={role}
              onChange={(event) =>
                setRole(event.target.value === "admin" ? "admin" : "member")
              }
            >
              <option value="admin">admin</option>
              <option value="member">member</option>
            </NativeSelect>
          </label>
        )}
        <Button
          type="submit"
          variant="primary"
          disabled={busy}
        >
          {busy ? "Saving…" : status.selfNeedsEmail ? "Save my email" : "Add user"}
        </Button>
      </form>
    </section>
  );
}

function TestSection({ status }: { status: Status }) {
  const [email, setEmail] = useState(status.selfEmail ?? "");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(
    null,
  );

  async function sendTest(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setResult(null);
    try {
      const res = await fetch("/api/admin/signin/test", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      const body = (await res.json().catch(() => null)) as {
        error?: string;
      } | null;
      if (!res.ok) throw new Error(body?.error ?? `test failed (${res.status})`);
      setResult({
        ok: true,
        text: `Test email sent to ${email}. Check the inbox. Its link is deliberately unusable; real sign-in happens at /signin.`,
      });
    } catch (err) {
      setResult({
        ok: false,
        text: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="settings-card !mt-5">
      <div className="settings-card-head">
        <div>
          <h2 className="settings-card-title">Delivery test</h2>
          <p className="settings-card-copy">
            Sends a real email through the configured provider so you can
            confirm delivery before anyone needs to sign in.
          </p>
        </div>
      </div>

      {result ? (
        <div
          className={`mb-3 rounded-md px-3 py-2 text-sm ${
            result.ok
              ? "bg-success-soft text-success-ink"
              : "bg-danger-soft text-danger"
          }`}
          role={result.ok ? "status" : "alert"}
        >
          {result.text}
        </div>
      ) : null}

      <form onSubmit={sendTest} className="flex flex-wrap items-end gap-3">
        <label className="flex flex-col gap-1 text-xs font-semibold text-text2">
          Send to
          <Input
            type="email"
            required
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            placeholder="you@company.com"
            className="w-72"
            autoComplete="email"
          />
        </label>
        <Button
          type="submit"
          disabled={busy}
        >
          {busy ? "Sending…" : "Send test email"}
        </Button>
      </form>
    </section>
  );
}

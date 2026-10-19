"use client";

import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import EntryShell from "@/components/EntryShell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

interface ProviderInfo {
  pluginName: string;
  providerLabel: string;
  provisioning?: "automatic" | "manual";
  loginHintRequired?: boolean;
}

function SignInBody() {
  const params = useSearchParams();
  const error = params.get("error");
  const notice = params.get("notice");
  const returnTo = params.get("returnTo") ?? "/";
  const [provider, setProvider] = useState<ProviderInfo | null>(null);
  const [pendingLabel, setPendingLabel] = useState<string | null>(null);
  const [providerLoaded, setProviderLoaded] = useState(false);
  const [loginHint, setLoginHint] = useState("");

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch("/api/auth/status", { cache: "no-store" });
        const body = (await res.json()) as {
          provider: ProviderInfo | null;
          pending?: { providerLabel: string } | null;
        };
        if (cancelled) return;
        setProvider(body.provider ?? null);
        setPendingLabel(body.pending?.providerLabel ?? null);
      } catch {
        if (cancelled) return;
        setProvider(null);
      } finally {
        if (!cancelled) setProviderLoaded(true);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <EntryShell
      title="Return to the work."
      description="Sign in to your OpenNeko deployment and resume the briefings, decisions, and agent runs waiting for you."
    >
      <div className="entry-section-head">
        <p className="entry-section-kicker">Authentication</p>
        <h2>Sign in</h2>
        <p>Your organization’s identity provider controls access.</p>
      </div>

      {error ? (
        <div className="entry-error" role="alert">
          {error}
        </div>
      ) : null}

      {notice === "link-sent" ? (
        <div className="entry-system-state" role="status">
          If that email belongs to a provisioned user, a sign-in link is on its
          way. Open it in this browser within 10 minutes.
        </div>
      ) : null}

      {!providerLoaded ? (
        <div className="entry-system-state" role="status">
          <span className="entry-system-pulse" aria-hidden="true" />
          Checking sign-in options…
        </div>
      ) : provider ? (
        <form action="/api/auth/begin" method="GET" className="entry-fields">
          <input
            data-ui-bespoke-reason="sign-in entry fields"
            type="hidden"
            name="returnTo"
            value={returnTo}
          />
          <label className="entry-field">
            <span>
              Email{" "}
              {provider.loginHintRequired ? null : (
                <span className="entry-optional">(optional)</span>
              )}
            </span>
            <Input
              type="email"
              name="loginHint"
              value={loginHint}
              onChange={(event) => setLoginHint(event.target.value)}
              placeholder="you@company.com"
              autoComplete="email"
              required={Boolean(provider.loginHintRequired)}
              className="entry-control"
            />
            <span className="entry-field-help">
              {provider.loginHintRequired
                ? `${provider.providerLabel} emails a single-use sign-in link to provisioned users.`
                : `Helps ${provider.providerLabel} route you to the correct identity provider.`}
            </span>
          </label>
          <Button type="submit" variant="primary" size="lg" className="w-full">
            {provider.loginHintRequired
              ? "Email me a sign-in link"
              : `Sign in with ${provider.providerLabel}`}
          </Button>
        </form>
      ) : pendingLabel ? (
        <div className="entry-empty">
          <p className="entry-section-kicker">Setup in progress</p>
          <h3>{pendingLabel} sign-in is being set up</h3>
          <p>
            An administrator is still configuring sign-in for this deployment.
            Try again shortly.
          </p>
        </div>
      ) : (
        <div className="entry-empty">
          <p className="entry-section-kicker">Single-operator mode</p>
          <h3>Sign-in is not set up</h3>
          <p>
            An administrator can turn on single sign-on or email-link sign-in
            under Admin, Settings. Until then, OpenNeko remains in
            single-operator mode.
          </p>
        </div>
      )}
    </EntryShell>
  );
}

export default function SignInPage() {
  return (
    <Suspense fallback={null}>
      <SignInBody />
    </Suspense>
  );
}

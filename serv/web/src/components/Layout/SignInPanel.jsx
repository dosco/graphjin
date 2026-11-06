import React from "react";

import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { errorText } from "@/services/graphql";
import { consoleLoginURL, signInAsDemo, signInWithToken, signOut } from "@/services/session";

// SignInPanel is the identity sheet on a server that only trusts JWTs. The
// development identity headers do nothing there, so the console signs in to a
// server-held session instead. onChange runs after the session starts or ends.
const SignInPanel = ({ state, onChange }) => {
  const [token, setToken] = React.useState("");
  const [pending, setPending] = React.useState("");
  const [error, setError] = React.useState("");

  async function run(kind, action) {
    setPending(kind);
    setError("");
    try {
      await action();
      onChange?.();
    } catch (err) {
      setError(errorText(err, "Sign-in failed."));
    } finally {
      setPending("");
    }
  }

  if (state.signedIn) {
    return (
      <div className="grid content-start gap-4 py-5">
        <div className="text-sm">
          <div className="hud-label">Signed in as</div>
          <div className="mt-1 truncate text-base font-bold">{state.name}</div>
          {state.role && <div className="mt-0.5 font-mono text-xs text-muted-foreground">{state.role}</div>}
        </div>
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        <div className="flex justify-end">
          <Button type="button" variant="secondary" disabled={pending !== ""} onClick={() => run("out", signOut)}>
            Sign out
          </Button>
        </div>
      </div>
    );
  }

  const has = (method) => state.methods.includes(method);
  return (
    <div className="grid content-start gap-4 overflow-y-auto py-5">
      <p className="text-sm text-muted-foreground">Sign in to see your agents, threads and watches.</p>
      {has("oidc") && state.loginURL && (
        <Button asChild>
          <a href={consoleLoginURL(state.loginURL)}>Sign in with single sign-on</a>
        </Button>
      )}
      {has("demo") && (
        <Button type="button" variant={has("oidc") ? "outline" : "default"} disabled={pending !== ""} onClick={() => run("demo", signInAsDemo)}>
          {pending === "demo" ? "Signing in…" : "Sign in as the demo operator"}
        </Button>
      )}
      {has("token") && (
        <form
          className="grid gap-2 pt-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (token.trim()) {
              void run("token", () => signInWithToken(token.trim()));
            }
          }}
        >
          <label className="grid gap-1.5 text-sm">
            <span className="font-medium">Access token</span>
            <Textarea value={token} onChange={(event) => setToken(event.target.value)} placeholder="eyJhbGciOi…" rows={4} autoComplete="off" spellCheck={false} className="font-mono text-xs" />
          </label>
          <div className="flex justify-end">
            <Button type="submit" variant="secondary" disabled={!token.trim() || pending !== ""}>
              {pending === "token" ? "Checking…" : "Use token"}
            </Button>
          </div>
        </form>
      )}
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    </div>
  );
};

export default SignInPanel;

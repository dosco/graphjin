import React from "react";
import { Link, useLocation } from "react-router-dom";

import { availableWorkspaces, workspaceIDFromPath } from "@/app/workspaces";
import { cn } from "@/lib/utils";
import { hasOperatorIdentity, useOperatorIdentity } from "@/services/identity";
import { signInState } from "@/services/session";
import { useConsole } from "./console-context";

// ContextRail is the right-hand column on wide screens: compact cards with
// the session the console is acting as and the workspaces it can reach. It
// only reads what the shell already loaded, so it costs no extra requests.
const ContextRail = () => {
  const location = useLocation();
  const { bootstrap } = useConsole();
  const identity = useOperatorIdentity();
  const session = signInState(bootstrap);
  const workspaceID = workspaceIDFromPath(location.pathname);
  const workspaces = availableWorkspaces(bootstrap);
  const byID = new Map((bootstrap?.workspaces || []).map((item) => [item?.id, item]));

  const who = session.required
    ? (session.signedIn ? session.name : "Not signed in")
    : (hasOperatorIdentity(identity) ? identity.userId : "Default identity");
  const rows = [
    ["Identity", who],
    ["Role", session.role || identity.role || bootstrap?.identity?.role || "default"],
    ["Environment", bootstrap?.scope?.environment || bootstrap?.scope?.source || "local"],
    ["Auth", session.required ? (session.signedIn ? "session" : "required") : "dev headers"],
  ];

  return (
    <aside aria-label="Console context" className="sticky top-0 hidden h-dvh w-[21rem] shrink-0 flex-col gap-4 self-start overflow-y-auto px-5 py-4 min-[1400px]:flex">
      <RailCard title="Session">
        <dl className="grid">
          {rows.map(([label, value]) => (
            <div key={label} className="flex items-baseline justify-between gap-4 px-4 py-2.5">
              <dt className="hud-label">{label}</dt>
              <dd className="min-w-0 truncate font-mono text-[0.8125rem] text-foreground" title={value}>{value}</dd>
            </div>
          ))}
        </dl>
      </RailCard>

      <RailCard title="Workspaces">
        <ul className="grid">
          {workspaces.map((workspace) => {
            const current = workspace.id === workspaceID;
            const capabilities = byID.get(workspace.id)?.capabilities?.length;
            return (
              <li key={workspace.id}>
                <Link
                  to={workspace.defaultPath}
                  aria-current={current ? "true" : undefined}
                  className={cn("grid gap-0.5 px-4 py-3 transition-colors hover:bg-accent", current && "text-foreground")}
                >
                  <span className="font-mono text-[0.6875rem] text-muted-foreground">
                    {workspace.items.length} views{typeof capabilities === "number" ? ` · ${capabilities} capabilities` : ""}
                  </span>
                  <span className={cn("text-[0.9375rem] font-bold", !current && "text-muted-foreground")}>
                    {workspace.label}
                    {current && <span className="ml-2 font-mono text-[0.6875rem] font-medium text-brand">current</span>}
                  </span>
                  <span className="text-[0.8125rem] text-muted-foreground">{workspace.description}</span>
                </Link>
              </li>
            );
          })}
        </ul>
      </RailCard>

      <p className="px-4 font-mono text-[0.6875rem] leading-5 text-muted-foreground">
        Press ⌘K to jump anywhere.
      </p>
    </aside>
  );
};

function RailCard({ title, children, className }) {
  return (
    <section className={cn("pb-2", className)}>
      <h2 className="hud-label px-4 pb-1 pt-3">{title}</h2>
      {children}
    </section>
  );
}

export default ContextRail;

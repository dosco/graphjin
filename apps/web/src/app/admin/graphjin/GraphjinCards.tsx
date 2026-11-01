import type { ReactNode } from "react";

// Layout pieces shared by the GraphJin admin pages. They import nothing
// server-only, so client components can use them too.

export function SettingsCard({
  title,
  copy,
  aside,
  children,
}: {
  title: string;
  copy?: ReactNode;
  aside?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <section className="settings-card">
      <div className="settings-card-head">
        <div className="min-w-0">
          <h2 className="settings-card-title">{title}</h2>
          {copy ? <p className="settings-card-copy">{copy}</p> : null}
        </div>
        {aside}
      </div>
      {children}
    </section>
  );
}

export function JsonBlock({ value }: { value: unknown }) {
  return (
    <pre className="max-h-[520px] overflow-auto whitespace-pre-wrap break-words rounded-inner bg-neutral-soft p-3 font-mono text-xs leading-5 text-text">
      <code>{typeof value === "string" ? value : JSON.stringify(value, null, 2)}</code>
    </pre>
  );
}

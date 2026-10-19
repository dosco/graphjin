import type { ReactNode } from "react";
import AppHeader from "@/components/AppHeader";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";

export function AdminShell({
  title,
  subtitle,
  children,
  back,
  narrow = false,
}: {
  title: string;
  subtitle?: string;
  children: ReactNode;
  back?: { href: string; label: string };
  narrow?: boolean;
}) {
  return (
    <div className={narrow ? "root is-narrow" : "root"}>
      <AppHeader back={back}>
        <SectionNav current="admin" />
      </AppHeader>
      <PageHeading
        title={title}
        description={subtitle}
      />
      {children}
    </div>
  );
}

export function AdminDenied() {
  return (
    <AdminShell
      title="Administration"
      subtitle="OpenNeko configuration, users, packs, and data access."
    >
      <div className="settings-card">
        <div className="settings-card-head">
          <div>
            <h2 className="settings-card-title">Admin only</h2>
            <p className="settings-card-copy">
              Your current role cannot access this area.
            </p>
          </div>
          <div className="settings-source">
            <strong className="is-warn">403</strong>
          </div>
        </div>
      </div>
    </AdminShell>
  );
}

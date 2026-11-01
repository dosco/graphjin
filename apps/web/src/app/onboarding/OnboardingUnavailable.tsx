import type { ReactNode } from "react";
import EntryShell from "@/components/EntryShell";
import { ButtonLink } from "@/components/ui/button";

type OnboardingUnavailableProps = {
  retryAction?: ReactNode;
  mode?: "database" | "workspace";
};

export default function OnboardingUnavailable({
  retryAction,
  mode = "database",
}: OnboardingUnavailableProps) {
  if (mode === "workspace") {
    return (
      <EntryShell
        title="An admin needs to finish setup."
        description="Your personal workspace will open after the organization model has been created."
      >
        <div className="entry-section-head">
          <p className="entry-section-kicker">Waiting for an administrator</p>
          <h2>Organization setup is incomplete</h2>
          <p>
            Ask an OpenNeko admin to finish the business setup. Your own role
            and priorities come next.
          </p>
        </div>
        <div className="entry-actions is-end">
          <ButtonLink href="/onboarding" variant="primary" size="lg">
            Check again
          </ButtonLink>
        </div>
      </EntryShell>
    );
  }

  return (
    <EntryShell
      title="Setup needs a connection."
      description="OpenNeko keeps failure states explicit so configuration problems are recoverable."
    >
      <div className="entry-section-head">
        <p className="entry-section-kicker">Onboarding unavailable</p>
        <h2>Workspace state could not be read</h2>
        <p>
          OpenNeko could not read the workspace setup state. Check the database
          settings, then retry.
        </p>
      </div>
      <div className="entry-error" role="alert">
        The database connection is unavailable. Your onboarding data has not
        been changed.
      </div>
      <div className="entry-actions">
        <ButtonLink href="/admin/settings" size="lg">
          Admin settings
        </ButtonLink>
        {retryAction ?? (
          <ButtonLink href="/onboarding" variant="primary" size="lg">
            Retry
          </ButtonLink>
        )}
      </div>
    </EntryShell>
  );
}

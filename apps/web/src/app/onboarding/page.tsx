import { getAuthProvider } from "@/lib/auth";
import { Suspense } from "react";
import { redirect } from "next/navigation";
import { connection } from "next/server";
import {
  and,
  customer_profile,
  db,
  eq,
  onboarding_wizard,
  operator_profile,
  organization,
} from "@neko/db";
import { getOrgId } from "@/lib/db";
import { getCurrentActor } from "@/lib/actor";
import { getSetupCompleteAt } from "@/lib/org-state";
import OnboardingWizard, { type WizardInitial } from "./OnboardingWizard";
import OnboardingUnavailable from "./OnboardingUnavailable";
import PersonaStep from "./PersonaStep";

const ORG_NAME_STUB = "My Workspace";

type OnboardingLoadResult =
  | { kind: "settings" }
  | {
      kind: "persona";
      initialRoleTemplate: string;
      initialFocusAreas: string[];
    }
  | { kind: "workspace_pending" }
  | { kind: "wizard"; initial: WizardInitial; teamMode: boolean };

export default async function OnboardingPage() {
  await connection();
  const result = await loadOnboarding().catch((error) => {
    console.error("[onboarding] failed to load", error);
    return { kind: "error" as const };
  });

  if (result.kind === "settings") {
    redirect("/admin/settings");
  }

  if (result.kind === "error") {
    return <OnboardingUnavailable />;
  }

  if (result.kind === "persona") {
    return (
      <Suspense fallback={null}>
        <PersonaStep
          initialRoleTemplate={result.initialRoleTemplate}
          initialFocusAreas={result.initialFocusAreas}
        />
      </Suspense>
    );
  }

  if (result.kind === "workspace_pending") {
    return <OnboardingUnavailable mode="workspace" />;
  }

  return (
    <Suspense fallback={null}>
      <OnboardingWizard initial={result.initial} teamMode={result.teamMode} />
    </Suspense>
  );
}

async function loadOnboarding(): Promise<OnboardingLoadResult> {
  const orgId = await getOrgId();
  const setupCompleteAt = await getSetupCompleteAt(orgId);
  if (!setupCompleteAt) {
    return { kind: "settings" };
  }

  const actor = await getCurrentActor();
  const personaUserId = (await getAuthProvider()) ? actor.userId : null;

  const [wizardRows, orgRows, currentProfiles, ownPersonas] = await Promise.all([
    db()
      .select({
        company_note: onboarding_wizard.company_note,
        fiscal_year_start_month: onboarding_wizard.fiscal_year_start_month,
        active_seats: onboarding_wizard.active_seats,
        priorities: onboarding_wizard.priorities,
      })
      .from(onboarding_wizard)
      .where(eq(onboarding_wizard.org_id, orgId))
      .limit(1),
    db()
      .select({ name: organization.name })
      .from(organization)
      .where(eq(organization.id, orgId))
      .limit(1),
    db()
      .select({ id: customer_profile.id })
      .from(customer_profile)
      .where(
        and(
          eq(customer_profile.org_id, orgId),
          eq(customer_profile.is_current, true),
        ),
      )
      .limit(1),
    personaUserId
      ? db()
          .select({
            roleTemplate: operator_profile.role_template,
            focusAreas: operator_profile.focus_areas,
          })
          .from(operator_profile)
          .where(
            and(
              eq(operator_profile.org_id, orgId),
              eq(operator_profile.user_id, personaUserId),
            ),
          )
          .limit(1)
      : Promise.resolve([]),
  ]);

  // Once the organization model exists, every SSO identity gets its own
  // persona. Admins are included: an admin is a person, not the org-default
  // CEO view. Query the exact user row so an org fallback cannot silently
  // count as completed personal setup.
  if (personaUserId && currentProfiles.length > 0) {
    return {
      kind: "persona",
      initialRoleTemplate: ownPersonas[0]?.roleTemplate ?? "",
      initialFocusAreas: ownPersonas[0]?.focusAreas ?? [],
    };
  }

  // A member cannot define organization-wide business context. This state is
  // uncommon (the first SSO identity is bootstrapped as admin) but must not
  // bounce between onboarding and the dashboard.
  if (actor.role === "member" && actor.userId) {
    return { kind: "workspace_pending" };
  }

  const row = wizardRows[0];
  const orgName = orgRows[0]?.name ?? "";
  const initial: WizardInitial = {
    companyName: orgName === ORG_NAME_STUB ? "" : orgName,
    companyNote: row?.company_note ?? "",
    fiscalYearStartMonth: row?.fiscal_year_start_month ?? 1,
    activeSeats: row?.active_seats ?? [],
    priorities: row?.priorities ?? [],
  };

  return { kind: "wizard", initial, teamMode: personaUserId !== null };
}

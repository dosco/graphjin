"use client";

import { useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import EntryShell from "@/components/EntryShell";
import { toast } from "sonner";
import Select from "@/components/Select";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";

// Quick-pick suggestions only — seats are free text (CV3 personas). A
// custom role flows through metrics, briefing tabs, and the persona
// prompt the same as these.
const SUGGESTED_SEATS = ["CEO", "CFO", "CRO", "COO", "CIO", "CPO"];
const MAX_SEAT_LENGTH = 40;
const MONTHS = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December",
];
const MONTH_OPTIONS = MONTHS.map((label, i) => ({
  value: String(i + 1),
  label,
}));
const SOLO_ONBOARDING_STEPS = [
  "Business",
  "Briefing views",
  "Priorities",
] as const;
const TEAM_ONBOARDING_STEPS = ["Business", "Priorities"] as const;

export type WizardInitial = {
  companyName: string;
  companyNote: string;
  fiscalYearStartMonth: number;
  activeSeats: string[];
  priorities: string[];
};

const EMPTY_INITIAL: WizardInitial = {
  companyName: "",
  companyNote: "",
  fiscalYearStartMonth: 1,
  activeSeats: [],
  priorities: [],
};

export default function OnboardingWizard({
  initial = EMPTY_INITIAL,
  teamMode = false,
}: {
  initial?: WizardInitial;
  teamMode?: boolean;
}) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [step, setStep] = useState(0);
  const [companyName, setCompanyName] = useState(initial.companyName);
  const [companyNote, setCompanyNote] = useState(initial.companyNote);
  const [fyMonth, setFyMonth] = useState(initial.fiscalYearStartMonth);
  const [seats, setSeats] = useState<string[]>(
    teamMode
      ? ["Organization"]
      : initial.activeSeats.length > 0
        ? initial.activeSeats
        : ["CEO"],
  );
  const [customSeat, setCustomSeat] = useState("");
  const [prioritiesText, setPrioritiesText] = useState(
    initial.priorities.join("\n"),
  );
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // If we landed here because the previous profile build failed, surface
  // a safe failure toast. The URL only carries a flag (?failed=1); the
  // server decides whether a real failure exists and returns user-safe copy.
  useEffect(() => {
    if (searchParams.get("failed") !== "1") return;
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch("/api/onboarding/status");
        const status = await res.json();
        if (cancelled) return;
        if (status.state === "failed" && typeof status.message === "string") {
          toast.error("Setup failed", { description: status.message });
        }
      } catch {
        // network error — drop silently; user is already on the wizard
      } finally {
        // Strip the flag so a refresh doesn't re-fire the toast.
        if (!cancelled) router.replace("/onboarding");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [searchParams, router]);

  const addCustomSeat = () => {
    const s = customSeat.trim().slice(0, MAX_SEAT_LENGTH);
    if (!s) return;
    setSeats((prev) =>
      prev.some((x) => x.toLowerCase() === s.toLowerCase())
        ? prev
        : [...prev, s],
    );
    setCustomSeat("");
  };

  const submit = async () => {
    setSubmitting(true);
    setError(null);
    try {
      const priorities = prioritiesText
        .split("\n")
        .map((s) => s.trim())
        .filter(Boolean);
      const res = await fetch("/api/onboarding/submit", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          companyName: companyName.trim(),
          companyNote,
          fiscalYearStartMonth: fyMonth,
          activeSeats: seats,
          priorities,
        }),
      });
      const body = await res.json().catch(() => ({}));
      if (!res.ok) {
        throw new Error(
          typeof body.error === "string"
            ? body.error
            : "Workspace setup could not start. Please try again.",
        );
      }
      router.push("/business-profile?from=setup");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setSubmitting(false);
    }
  };

  const canSubmit =
    companyName.trim().length > 0 &&
    companyNote.trim().length > 0 &&
    seats.length > 0 &&
    !submitting;
  const onboardingSteps = teamMode
    ? TEAM_ONBOARDING_STEPS
    : SOLO_ONBOARDING_STEPS;
  const prioritiesStep = teamMode ? 1 : 2;

  return (
    <EntryShell
      title="Build the business model."
      description="Define what the agent should monitor, prioritize, and explain."
      steps={onboardingSteps}
      currentStep={step}
    >
      {step === 0 ? (
        <>
          <div className="entry-section-head">
            <p className="entry-section-kicker">01 · Business</p>
            <h2>Map the business</h2>
            <p>
              Start with enough context for useful answers, not a company
              biography.
            </p>
          </div>
          <div className="entry-fields">
            <Field label="Company name">
              <Input
                className="entry-control"
                type="text"
                value={companyName}
                onChange={(event) => setCompanyName(event.target.value)}
                placeholder="AdventureWorks Cycles"
                autoComplete="organization"
                aria-label="Company name"
                required
              />
            </Field>

            <Field label="What does the company do?">
              <Textarea
                className="entry-control"
                value={companyNote}
                onChange={(event) => setCompanyNote(event.target.value)}
                rows={4}
                placeholder="We make and sell bicycles to retailers across North America and Europe. This quarter we are protecting wholesale margin while expanding direct sales."
                aria-label="Company operating context"
              />
              <span className="entry-field-help">
                Include customers, regions, business model, and the current
                operating constraint.
              </span>
            </Field>

            <Field label="Fiscal year starts">
              <Select
                value={String(fyMonth)}
                onChange={(value) => setFyMonth(Number(value))}
                options={MONTH_OPTIONS}
                ariaLabel="Fiscal year start month"
              />
            </Field>
          </div>
          <div className="entry-actions is-end">
            <Button
              variant="primary"
              size="lg"
              disabled={!companyName.trim() || !companyNote.trim()}
              onClick={() => setStep(1)}
            >
              Continue
            </Button>
          </div>
        </>
      ) : null}

      {!teamMode && step === 1 ? (
        <>
          <div className="entry-section-head">
            <p className="entry-section-kicker">02 · Briefing views</p>
            <h2>Which shared views do you need?</h2>
            <p>
              These are organization-level lenses for a solo workspace, not user
              accounts.
            </p>
          </div>
          <fieldset className="entry-field">
            <legend className="sr-only">Active decision-maker seats</legend>
            <ToggleGroup
              type="multiple"
              value={seats}
              onValueChange={setSeats}
              className="entry-chip-grid"
            >
              {[
                ...SUGGESTED_SEATS,
                ...seats.filter((seat) => !SUGGESTED_SEATS.includes(seat)),
              ].map((seat) => {
                const selected = seats.includes(seat);
                return (
                  <ToggleGroupItem
                    key={seat}
                    value={seat}
                    className={`entry-chip${selected ? " is-selected" : ""}`}
                  >
                    {seat}
                  </ToggleGroupItem>
                );
              })}
            </ToggleGroup>
            <div className="entry-inline">
              <Input
                className="entry-control"
                value={customSeat}
                maxLength={MAX_SEAT_LENGTH}
                placeholder="Add a role — e.g. Head of Operations"
                onChange={(event) => setCustomSeat(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key !== "Enter") return;
                  event.preventDefault();
                  addCustomSeat();
                }}
                aria-label="Add a custom decision-maker seat"
              />
              <Button
                size="lg"
                onClick={addCustomSeat}
                disabled={!customSeat.trim()}
              >
                Add role
              </Button>
            </div>
          </fieldset>
          <div className="entry-actions">
            <Button size="lg" onClick={() => setStep(0)}>
              Back
            </Button>
            <Button
              variant="primary"
              size="lg"
              disabled={seats.length === 0}
              onClick={() => setStep(2)}
            >
              Continue
            </Button>
          </div>
        </>
      ) : null}

      {step === prioritiesStep ? (
        <>
          <div className="entry-section-head">
            <p className="entry-section-kicker">
              {teamMode ? "02 · Priorities" : "03 · Priorities"}
            </p>
            <h2>Set the first watchlist</h2>
            <p>
              OpenNeko will use these priorities to rank signals while it learns
              from your work.
            </p>
          </div>
          <div className="entry-fields">
            <Field label="What needs attention this quarter?">
              <Textarea
                className="entry-control"
                value={prioritiesText}
                onChange={(event) => setPrioritiesText(event.target.value)}
                rows={5}
                placeholder={
                  "Defend wholesale margins\nReduce stock-outs on top SKUs\nGrow direct sales in Europe"
                }
                aria-label="Quarterly priorities"
              />
              <span className="entry-field-help">
                Optional. Add one priority per line; you can change these later.
              </span>
            </Field>

            {error ? (
              <div className="entry-error" role="alert">
                {error}
              </div>
            ) : null}
          </div>
          <div className="entry-actions">
            <Button
              size="lg"
              disabled={submitting}
              onClick={() => setStep(teamMode ? 0 : 1)}
            >
              Back
            </Button>
            <Button
              variant="primary"
              size="lg"
              disabled={!canSubmit}
              onClick={() => void submit()}
            >
              {submitting ? "Building workspace…" : "Build workspace"}
            </Button>
          </div>
        </>
      ) : null}
    </EntryShell>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="entry-field">
      <span>{label}</span>
      {children}
    </div>
  );
}

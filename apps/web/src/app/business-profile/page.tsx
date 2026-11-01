"use client";

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useRouter } from "next/navigation";
import AppHeader from "@/components/AppHeader";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";
import EditableMarkdown from "@/components/EditableMarkdown";
import { Button } from "@/components/ui/button";
import { Segment, SegmentedControl } from "@/components/ui/tabs";
import { useDebouncedSave } from "@/hooks/useDebouncedSave";
import type { StageKind } from "@/lib/db";

type Phase = "loading" | "processing" | "review";
type Tab = "profile" | "insights";
type InsightsStatus = "ready" | "processing" | "disabled" | "pending";

const STAGE_LABELS: Record<StageKind, string> = {
  business_profile_build: "Profile",
  industry_insights_build: "Industry insights",
  bootstrap_metrics_build: "Picking metrics",
  metric_refresh: "Computing metrics",
};
const STAGE_ORDER: StageKind[] = [
  "business_profile_build",
  "industry_insights_build",
];
const STAGE_FALLBACK_COPY: Record<StageKind, readonly string[]> = {
  business_profile_build: [
    "Reading your data sources…",
    "Mapping tables and relationships…",
    "Drafting your business profile…",
  ],
  industry_insights_build: [
    "Researching your industry…",
    "Gathering competitive context…",
    "Distilling industry trends…",
  ],
  bootstrap_metrics_build: ["Picking the metrics that matter…"],
  metric_refresh: ["Computing your numbers…"],
};
const FALLBACK_DEFAULT = "Reading your data sources…";
const FALLBACK_CYCLE_MS = 3500;

const noSubscribe = () => () => {};

export default function ProcessingPage() {
  const router = useRouter();
  const [phase, setPhase] = useState<Phase>("loading");
  const [tab, setTab] = useState<Tab>("profile");
  const [stageKind, setStageKind] = useState<StageKind | null>(null);
  const [stageMessage, setStageMessage] = useState<string | null>(null);
  const [fallbackIdx, setFallbackIdx] = useState(0);
  const [profile, setProfile] = useState("");
  const [insights, setInsights] = useState("");
  const [insightsStatus, setInsightsStatus] = useState<InsightsStatus>("processing");
  const [needsPersona, setNeedsPersona] = useState(false);
  const fromSetup = useSyncExternalStore(
    noSubscribe,
    () => new URLSearchParams(window.location.search).get("from") === "setup",
    () => false,
  );
  const profileEditedRef = useRef(false);
  const insightsEditedRef = useRef(false);

  const saveProfile = useCallback(async (v: string) => {
    const res = await fetch("/api/profile", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ businessProfile: v }),
    });
    if (!res.ok) throw new Error(`save failed: ${res.status}`);
  }, []);
  const saveInsights = useCallback(async (v: string) => {
    const res = await fetch("/api/profile", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ industryInsights: v }),
    });
    if (!res.ok) throw new Error(`save failed: ${res.status}`);
  }, []);
  const profileSaver = useDebouncedSave<string>({ delayMs: 800, save: saveProfile });
  const insightsSaver = useDebouncedSave<string>({ delayMs: 800, save: saveInsights });
  const flushAll = useCallback(
    async () => {
      await Promise.all([profileSaver.flush(), insightsSaver.flush()]);
    },
    [profileSaver, insightsSaver],
  );
  const saveError = profileSaver.lastError ?? insightsSaver.lastError;

  const handleProfileChange = useCallback(
    (next: string) => {
      profileEditedRef.current = true;
      setProfile(next);
      profileSaver.schedule(next);
    },
    [profileSaver],
  );
  const handleInsightsChange = useCallback(
    (next: string) => {
      insightsEditedRef.current = true;
      setInsights(next);
      insightsSaver.schedule(next);
    },
    [insightsSaver],
  );

  useEffect(() => {
    if (phase === "review") return;

    let cancelled = false;

    const poll = async () => {
      while (!cancelled) {
        try {
          const res = await fetch("/api/onboarding/status");
          const status = await res.json();
          if (status.state === "ready" || status.state === "needs_persona") {
            setNeedsPersona(status.state === "needs_persona");
            const pRes = await fetch("/api/profile");
            if (pRes.ok) {
              const data = await pRes.json();
              if (!profileEditedRef.current) setProfile(data.businessProfile ?? "");
              if (data.industryInsights && !insightsEditedRef.current) {
                setInsights(data.industryInsights);
              }
              setInsightsStatus(data.industryInsightsStatus ?? "processing");
            }
            if (!cancelled) setPhase("review");
            return;
          }
          if (status.state === "processing" && !cancelled) {
            const cs = status.currentStage as { kind?: StageKind; message?: string | null } | undefined;
            const nextStage = cs?.kind ?? null;
            const nextMsg = cs?.message ?? null;
            setStageKind((prev) => {
              if (prev !== nextStage) setFallbackIdx(0);
              return nextStage;
            });
            setStageMessage(nextMsg);
            setPhase("processing");
          }
          if (status.state === "needs_wizard") {
            router.replace("/onboarding");
            return;
          }
          if (status.state === "failed") {
            router.replace("/onboarding?failed=1");
            return;
          }
        } catch {}
        await new Promise((r) => setTimeout(r, 3000));
      }
    };
    poll();

    return () => {
      cancelled = true;
    };
  }, [phase, router]);

  useEffect(() => {
    if (phase !== "processing") return;
    if (stageMessage) return;
    const id = setInterval(() => {
      setFallbackIdx((i) => i + 1);
    }, FALLBACK_CYCLE_MS);
    return () => clearInterval(id);
  }, [phase, stageMessage, stageKind]);

  useEffect(() => {
    if (phase !== "review") return;
    if (insights || insightsStatus === "disabled") return;

    let cancelled = false;
    (async () => {
      try {
        const res = await fetch("/api/insights/ensure", { method: "POST" });
        if (!res.ok || cancelled) return;
        const body = await res.json();
        if (body.state === "disabled") setInsightsStatus("disabled");
        else if (body.state === "ready") {
          const pRes = await fetch("/api/profile");
          if (pRes.ok && !cancelled) {
            const pData = await pRes.json();
            if (pData.industryInsights && !insightsEditedRef.current) {
              setInsights(pData.industryInsights);
            }
            setInsightsStatus(pData.industryInsightsStatus ?? "ready");
          }
        } else {
          setInsightsStatus("processing");
        }
      } catch {}
    })();
    return () => { cancelled = true; };
  }, [phase, insights, insightsStatus]);

  useEffect(() => {
    if (phase !== "review") return;
    if (insights || insightsStatus === "disabled") return;

    let cancelled = false;
    const poll = async () => {
      while (!cancelled) {
        try {
          const res = await fetch("/api/profile");
          if (res.ok) {
            const data = await res.json();
            setInsightsStatus(data.industryInsightsStatus ?? "processing");
            if (data.industryInsights) {
              if (!insightsEditedRef.current) setInsights(data.industryInsights);
              return;
            }
          }
        } catch {}
        await new Promise((r) => setTimeout(r, 4000));
      }
    };
    poll();
    return () => { cancelled = true; };
  }, [phase, insights, insightsStatus]);

  if (phase === "loading") {
    return (
      <div className="root is-narrow">
        <AppHeader>
          <SectionNav current="business-profile" />
        </AppHeader>
      </div>
    );
  }

  if (phase === "processing") {
    let message: string;
    if (stageMessage) {
      message = stageMessage;
    } else if (stageKind) {
      const cycle = STAGE_FALLBACK_COPY[stageKind];
      message = cycle[fallbackIdx % cycle.length] ?? FALLBACK_DEFAULT;
    } else {
      message = FALLBACK_DEFAULT;
    }
    return (
      <div className="root is-narrow">
        <AppHeader>
          <SectionNav current="business-profile" />
        </AppHeader>
        <PageHeading
          title="Building the profile"
          description="OpenNeko is turning your onboarding brief into operating context for future answers and monitoring."
          meta="in progress"
        />
        <StageStrip current={stageKind} />
        <div
          key={message}
          className="date-note"
          style={{ marginTop: 24, animation: "fadeUp 0.5s ease both" }}
        >
          {message}
        </div>
      </div>
    );
  }

  const insightsPending = !insights && insightsStatus !== "disabled";

  return (
    <div className="root is-narrow">
      <AppHeader>
        <SectionNav current="business-profile" />
      </AppHeader>
      <PageHeading
        title="Business profile"
        description="Review and refine the operating context OpenNeko uses in briefings, answers, and agent runs."
        meta={insightsPending ? "Research running" : undefined}
      />

      <SegmentedControl
        aria-label="Business context section"
        className="mb-6"
        style={{ animation: "fadeUp 0.5s ease 0.12s both" }}
      >
        <Segment
          selected={tab === "profile"}
          onClick={async () => {
            await flushAll();
            setTab("profile");
          }}
        >
          Business profile
        </Segment>
        <Segment
          selected={tab === "insights"}
          onClick={async () => {
            await flushAll();
            setTab("insights");
          }}
        >
          Industry insights
          {insightsPending && (
            <span
              style={{
                marginLeft: 8,
                fontSize: "var(--type-caption)",
                opacity: 0.75,
              }}
            >
              · running
            </span>
          )}
        </Segment>
      </SegmentedControl>

      <div className="profile-card" style={{ animation: "fadeUp 0.6s ease 0.3s both" }}>
        {tab === "profile" ? (
          <EditableMarkdown
            value={profile}
            onChange={handleProfileChange}
            onCommit={() => profileSaver.flush()}
            placeholder={<ProfileEmpty />}
            components={mdComponents}
            ariaLabel="Business profile"
          />
        ) : insightsStatus === "disabled" ? (
          <InsightsDisabled />
        ) : insights ? (
          <EditableMarkdown
            value={insights}
            onChange={handleInsightsChange}
            onCommit={() => insightsSaver.flush()}
            components={mdComponents}
            ariaLabel="Industry insights"
          />
        ) : (
          <InsightsLoading />
        )}
      </div>
      {saveError && (
        <div
          style={{
            marginTop: 8,
            fontSize: "var(--type-caption)",
            color: "var(--text3)",
            textAlign: "right",
          }}
          role="status"
        >
          Couldn&apos;t save — will retry on your next edit.
        </div>
      )}

      <div
        style={{
          marginTop: 28,
          display: "flex",
          gap: 10,
          alignItems: "center",
          flexWrap: "wrap",
          animation: "fadeUp 0.5s ease 0.45s both",
        }}
      >
        {needsPersona || fromSetup ? (
          <Button
            variant="primary"
            onClick={async () => {
              await flushAll();
              router.replace(needsPersona ? "/onboarding" : "/");
            }}
          >
            {needsPersona ? "Continue to personal setup" : "Continue to your briefing"}
          </Button>
        ) : null}
        {insights && insightsStatus !== "disabled" && (
          <Button
            onClick={async () => {
              await flushAll();
              try {
                const res = await fetch("/api/insights/ensure", {
                  method: "POST",
                  headers: { "Content-Type": "application/json" },
                  body: JSON.stringify({ force: true }),
                });
                if (res.ok) {
                  insightsEditedRef.current = false;
                  setInsights("");
                  setInsightsStatus("processing");
                  setTab("insights");
                }
              } catch {}
            }}
          >
            Regenerate insights
          </Button>
        )}
      </div>
    </div>
  );
}

function StageStrip({ current }: { current: StageKind | null }) {
  const idx = current ? STAGE_ORDER.indexOf(current) : 0;
  return (
    <div
      style={{
        display: "flex",
        gap: 8,
        justifyContent: "center",
        marginTop: 36,
      }}
    >
      {STAGE_ORDER.map((kind, i) => {
        const state = i < idx ? "done" : i === idx ? "active" : "pending";
        const dot =
          state === "done" ? "✓" : state === "active" ? "●" : "○";
        return (
          <div
            key={kind}
            style={{
              padding: "8px 14px",
              borderRadius: 999,
              border: "1px solid var(--border)",
              background: state === "active" ? "var(--accent-soft)" : "transparent",
              color: state === "done" ? "var(--text2)" : state === "active" ? "var(--accent)" : "var(--text3)",
              fontSize: "var(--type-body-sm)",
              display: "inline-flex",
              alignItems: "center",
              gap: 6,
            }}
          >
            <span aria-hidden style={{ fontSize: "var(--type-label)" }}>{dot}</span>
            <span>{STAGE_LABELS[kind]}</span>
          </div>
        );
      })}
    </div>
  );
}

const PM_H2_CLS = "font-display text-ui-section font-bold leading-[1.3] tracking-[-0.3px] mb-4 text-text";
const PM_H3_CLS = "font-display text-ui-caption font-semibold text-text3 mt-6 mb-2 first:mt-0";
const PM_P_CLS = "text-ui-body leading-[1.65] text-text2 mb-1.5";
const PM_CITE_CLS =
  "text-ui-label font-semibold text-accent no-underline bg-accent-soft rounded-[4px] px-[5px] py-px mx-px align-super leading-none transition-[background-color,color] duration-150 hover:bg-accent hover:text-white";

function InsightsLoading() {
  return (
    <div className="py-6 px-1 text-center">
      <div className={`${PM_H3_CLS} mt-0 mb-2`}>Researching your industry…</div>
      <div className={`${PM_P_CLS} text-text2`}>
        Deep-diving into industry trends and benchmarks — this usually takes a minute or two.
      </div>
    </div>
  );
}

function ProfileEmpty() {
  return (
    <div className="py-6 px-1 text-center">
      <div className={`${PM_H3_CLS} mt-0 mb-2`}>Your business profile is being assembled</div>
      <div className={`${PM_P_CLS} text-text2`}>
        OpenNeko reads what you shared in onboarding plus signals from your connected
        data sources. The profile will appear here as soon as it&apos;s ready.
      </div>
    </div>
  );
}

function InsightsDisabled() {
  return (
    <div className="py-6 px-1 text-center">
      <div className={`${PM_H3_CLS} mt-0 mb-2`}>Industry research is off</div>
      <div className={`${PM_P_CLS} text-text2`}>Enable industry research from Settings.</div>
    </div>
  );
}

const mdComponents = {
  h1: (p: React.ComponentProps<"h2">) => <h2 className={PM_H2_CLS} {...p} />,
  h2: (p: React.ComponentProps<"h3">) => <h3 className={PM_H3_CLS} {...p} />,
  p: (p: React.ComponentProps<"p">) => <p className={PM_P_CLS} {...p} />,
  ul: (p: React.ComponentProps<"ul">) => <ul className="pm-ul" {...p} />,
  a: (p: React.ComponentProps<"a">) => <a className={PM_CITE_CLS} target="_blank" rel="noopener noreferrer" {...p} />,
};

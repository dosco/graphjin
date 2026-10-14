"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Disclosure } from "@/components/ui/disclosure";
import { Skeleton } from "@/components/ui/skeleton";
import { presentWorkFailure } from "@/lib/work-failure";
import type { WorkEvent } from "./work-types";

const ANSWER_CARD_PLACEHOLDER_MS = 60_000;

/**
 * A long run shows where its answer will appear, with the step it is on and
 * the data it has read, until the agent sends its card.
 */
export function AnswerInProgress({ since, step, sources }: { since?: string; step: string | null; sources: string[] }) {
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const started = since ? Date.parse(since) : Date.now();
    const wait = Math.max(0, ANSWER_CARD_PLACEHOLDER_MS - (Date.now() - started));
    const id = window.setTimeout(() => setShown(true), wait);
    return () => window.clearTimeout(id);
  }, [since]);
  if (!shown) return null;
  return (
    <div className="work-surface-frame answer-in-progress" role="status" aria-live="polite">
      <div className="work-surface">
        <div className="answer-draft-note">
          <span className="answer-draft-pulse" aria-hidden="true" />
          Preparing the answer
        </div>
        {step ? <div className="answer-in-progress-step">{step}</div> : null}
        {sources.length > 0 ? (
          <div className="answer-in-progress-sources">
            <span>Read so far</span>
            <strong>{sources.slice(0, 6).join(" · ")}{sources.length > 6 ? ` · ${sources.length - 6} more` : ""}</strong>
          </div>
        ) : null}
        <div className="answer-in-progress-figures" aria-hidden="true">
          <Skeleton className="h-16" />
          <Skeleton className="h-16" />
          <Skeleton className="h-16" />
        </div>
        <Skeleton className="mt-3 h-28" />
      </div>
    </div>
  );
}

export function WorkFailureNotice({ message }: { message: string }) {
  const failure = presentWorkFailure(message);
  return (
    <div
      className="rounded-2xl border border-warn/40 bg-warn-soft px-3 py-2.5 text-ui-body-sm text-warn-ink"
      role="alert"
    >
      <p className="font-semibold">{failure.summary}</p>
      <Disclosure
        title="Technical details"
        className="mt-2 border-warn/40 bg-card/70"
      >
        <p className="break-words font-mono text-ui-caption text-text2">
          {failure.detail}
        </p>
      </Disclosure>
    </div>
  );
}

export function NeedsInputNotice({
  request,
  onRespond,
}: {
  request: Extract<WorkEvent, { type: "needs_input" }>;
  onRespond: (text: string) => void;
}) {
  const questions: NonNullable<typeof request.questions> = request.questions?.length
    ? request.questions
    : [{
        id: "q1",
        header: undefined,
        question: request.question,
        options: request.options?.map((label) => ({ label })),
      }];
  return (
    <section className="work-surface-frame" aria-label="Clarification needed">
      <div className="work-surface">
        <div className="work-surface-eyebrow">
          <span className="work-surface-eyebrow-rule" aria-hidden="true" />
          Clarification
        </div>
        <div className="work-surface-title">A detail before I continue</div>
        {request.reason ? <div className="work-surface-sub">{request.reason}</div> : null}
        <div className="work-a2ui-layout is-column">
          {questions.map((question) => (
            <div key={question.id} className="work-a2ui-field">
              <span className="work-a2ui-label">
                {question.header ? `${question.header}: ` : ""}{question.question}
              </span>
              {question.options?.length ? (
                <div className="work-a2ui-chips">
                  {question.options.map((option) => (
                    <Button
                      key={option.label}
                      size="sm"
                      variant="secondary"
                      className="work-choice-btn"
                      onClick={() => onRespond(`${question.question}\n\n${option.label}`)}
                    >
                      <span>{option.label}</span>
                      <span aria-hidden="true">→</span>
                    </Button>
                  ))}
                </div>
              ) : (
                <Button
                  size="sm"
                  className="work-a2ui-button"
                  onClick={() => onRespond(`${question.question}\n\n`)}
                >
                  Answer in composer
                </Button>
              )}
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

export function CapabilityDeniedNotice({
  denial,
  canAdminister,
  onRequest,
}: {
  denial: Extract<WorkEvent, { type: "capability_denied" }>;
  canAdminister: boolean;
  onRequest: (prompt: string) => void;
}) {
  const endpoint = `${denial.host}${denial.port ? `:${denial.port}` : ""}`;
  const requestPrompt = [
    `Find an appropriate approved integration that can securely access ${endpoint}`,
    "for the request I just made.",
    "Show me the exact integration and file an approval-gated install request.",
    "Do not enable blanket network access.",
  ].join(" ");
  return (
    <section className="work-capability-denied" role="status">
      <div className="work-capability-kicker">Network access blocked</div>
      <div className="work-capability-grid">
        <div>
          <h3>{endpoint} is outside this workspace</h3>
          <p>
            OpenNeko did not receive live data. The sandbox stayed
            default-deny, so this answer must not present fallback estimates as
            current facts.
          </p>
          <code>
            {denial.method ?? "REQUEST"} {denial.path ?? "/"}
          </code>
        </div>
        <div className="work-capability-recovery">
          <span>Safe recovery</span>
          {canAdminister ? (
            <>
              <p>
                Ask OpenNeko to find the right integration. Installation will
                still require your explicit approval.
              </p>
              <Button
                size="sm"
                variant="primary"
                onClick={() => onRequest(requestPrompt)}
              >
                Request secure integration
              </Button>
            </>
          ) : (
            <p>
              Contact an OpenNeko administrator to install an integration
              approved for this host.
            </p>
          )}
          <Link href={canAdminister ? "/admin/plugins" : "/integrations"}>
            {canAdminister ? "Review plugins" : "View integrations"}
          </Link>
        </div>
      </div>
    </section>
  );
}

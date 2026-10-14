"use client";

import { Button } from "@/components/ui/button";
import { LocalDateTime } from "@/components/ui/local-date-time";
import { formatSavedShort } from "@/lib/hours-saved";
import { workDuration } from "./work-progress";
import type { RunArtifact, RunRecord } from "./work-types";

function artifactHref(path: string): string {
  return `/api/work/files/${path.replace(
    /^.*\/(runs|uploads|skills|memory)\//,
    "$1/",
  )}`;
}

export function AnswerRunFooter({
  run,
  sources,
  artifacts,
  followups,
  onFollowup,
}: {
  run: RunRecord | null;
  sources: string[];
  artifacts: RunArtifact[];
  followups: string[];
  onFollowup: (prompt: string) => void;
}) {
  const saved =
    typeof run?.analysisMinutesSaved === "number" &&
    run.analysisMinutesSaved > 0
      ? formatSavedShort(run.analysisMinutesSaved)
      : null;
  const answeredIn = workDuration(run);
  const hasEvidence = sources.length > 0 || artifacts.length > 0 || saved || answeredIn;
  if (!hasEvidence && followups.length === 0) return null;
  return (
    <footer className="work-answer-footer">
      {hasEvidence ? (
        <div className="work-answer-evidence">
          {sources.length > 0 ? (
            <div>
              <span>Evidence</span>
              <strong>{sources.join(" · ")}</strong>
            </div>
          ) : null}
          {artifacts.map((artifact) => (
            <a
              key={artifact.path}
              href={artifactHref(artifact.path)}
              title={artifact.path}
              download={artifact.label}
            >
              <span>Artifact</span>
              <strong>{artifact.label}</strong>
            </a>
          ))}
          {answeredIn && run?.finishedAt ? (
            <div>
              <span>Answered in</span>
              <strong>
                {answeredIn}
                <LocalDateTime value={run.finishedAt} className="work-answer-time" />
              </strong>
            </div>
          ) : null}
          {saved ? (
            <div title={run?.analysisMinutesBasis ?? undefined}>
              <span>Analysis avoided</span>
              <strong>{saved}</strong>
            </div>
          ) : null}
        </div>
      ) : null}
      {followups.length > 0 ? (
        <div className="work-answer-followups">
          <span>Continue</span>
          <div>
            {followups.map((prompt) => (
              <Button
                key={prompt}
                size="sm"
                variant="ghost"
                onClick={() => onFollowup(prompt)}
              >
                {prompt}
                <span aria-hidden="true">↗</span>
              </Button>
            ))}
          </div>
        </div>
      ) : null}
    </footer>
  );
}

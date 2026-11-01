"use client";

import {
  File,
  FileArchive,
  FileAudio,
  FileCode,
  FileImage,
  FileJson,
  FileSpreadsheet,
  FileText,
  FileVideo,
  Presentation,
  type LucideIcon,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Disclosure } from "@/components/ui/disclosure";
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

const FILE_ICONS: Array<[LucideIcon, string[]]> = [
  [FileSpreadsheet, ["csv", "tsv", "xls", "xlsx", "ods"]],
  [FileJson, ["json", "jsonl", "ndjson"]],
  [FileText, ["txt", "md", "pdf", "doc", "docx", "rtf", "odt", "log"]],
  [Presentation, ["ppt", "pptx", "key", "odp"]],
  [FileImage, ["png", "jpg", "jpeg", "gif", "svg", "webp", "bmp", "heic"]],
  [FileCode, ["html", "htm", "js", "ts", "py", "sql", "sh", "xml", "yaml", "yml", "css"]],
  [FileArchive, ["zip", "gz", "tgz", "tar", "7z", "rar"]],
  [FileVideo, ["mp4", "mov", "webm", "avi", "mkv"]],
  [FileAudio, ["mp3", "wav", "m4a", "ogg", "flac"]],
];

function fileIcon(name: string): LucideIcon {
  const extension = name.split(".").pop()?.toLowerCase() ?? "";
  return FILE_ICONS.find(([, extensions]) => extensions.includes(extension))?.[0] ?? File;
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
  const hasEvidence = sources.length > 0 || saved || answeredIn;
  if (!hasEvidence && artifacts.length === 0 && followups.length === 0) return null;
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
      {artifacts.length > 0 ? (
        <Disclosure
          className="work-answer-files"
          title={artifacts.length === 1 ? "File" : "Files"}
          meta={artifacts.length}
        >
          <ul>
            {artifacts.map((artifact) => {
              const Icon = fileIcon(artifact.label);
              return (
                <li key={artifact.path}>
                  <a href={artifactHref(artifact.path)} download={artifact.label}>
                    <Icon aria-hidden="true" strokeWidth={1.8} />
                    {artifact.label}
                  </a>
                </li>
              );
            })}
          </ul>
        </Disclosure>
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

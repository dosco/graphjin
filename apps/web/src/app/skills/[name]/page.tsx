"use client";

import { ItemAccessPanel } from "@/components/admin/ItemAccessPanel";
import { use as usePromise, useCallback, useEffect, useState, type ComponentPropsWithoutRef } from "react";
import Link from "next/link";
import { ArrowLeft, FileText, Pencil } from "lucide-react";
import ReactMarkdown from "react-markdown";
import PageHeading from "@/components/PageHeading";
import { Button, buttonClassName } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";

function stripFrontmatter(markdown: string): string {
  return markdown.replace(/^---\n[\s\S]*?\n---\n?/, "").trimStart();
}

type SkillFile = {
  path: string;
  bytes: number;
  binary: boolean;
  editable: boolean;
  truncated: boolean;
  text: string | null;
};

type SkillDetail = {
  name: string;
  description: string;
  fileCount: number;
  updatedAt: string;
  skillMarkdown: string;
  files: Array<{ path: string; bytes: number }>;
};

type PageProps = {
  params: Promise<{ name: string }>;
};

async function fetchSkill(
  name: string,
  signal?: AbortSignal,
): Promise<SkillDetail | null> {
  const response = await fetch(`/api/work/skills/${encodeURIComponent(name)}`, {
    cache: "no-store",
    signal,
  });
  if (response.status === 404) return null;
  if (!response.ok) throw new Error("Skill could not be loaded.");
  const data = (await response.json()) as { skill: SkillDetail };
  return data.skill;
}

export default function SkillDetailPage({ params }: PageProps) {
  const { name } = usePromise(params);
  const [skill, setSkill] = useState<SkillDetail | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "missing" | "error">(
    "loading",
  );

  const load = useCallback(async () => {
    try {
      const nextSkill = await fetchSkill(name);
      if (!nextSkill) {
        setState("missing");
        return;
      }
      setSkill(nextSkill);
      setState("ready");
    } catch {
      setState("error");
    }
  }, [name]);

  useEffect(() => {
    const controller = new AbortController();
    void fetchSkill(name, controller.signal)
      .then((nextSkill) => {
        if (!nextSkill) {
          setState("missing");
          return;
        }
        setSkill(nextSkill);
        setState("ready");
      })
      .catch(() => {
        if (!controller.signal.aborted) setState("error");
      });
    return () => controller.abort();
  }, [name]);

  if (state !== "ready" || !skill) {
    return (
      <div className="library-page skill-detail-page">
        <PageHeading
          eyebrow={
            <Link href="/skills" className="skill-back-link">
              <ArrowLeft aria-hidden="true" strokeWidth={2} />
              Skills
            </Link>
          }
          title={state === "loading" ? "Loading skill" : "Skill unavailable"}
        />
        <main className="library-main">
          {state === "loading" ? (
            <div className="library-loading" role="status" aria-label="Loading skill">
              <span />
              <span />
              <span />
            </div>
          ) : (
            <div className="library-error" role="alert">
              <div>
                <strong>{state === "missing" ? "Skill not found" : "Load failed"}</strong>
                <span>
                  {state === "missing"
                    ? "This skill is no longer in the organization workspace."
                    : "OpenNeko could not read this skill."}
                </span>
              </div>
              {state === "error" ? (
                <Button
                  variant="danger"
                  size="sm"
                  className="shrink-0"
                  onClick={() => void load()}
                >
                  Retry
                </Button>
              ) : (
                <Link href="/skills" className={buttonClassName({ size: "sm" })}>
                  All skills
                </Link>
              )}
            </div>
          )}
        </main>
      </div>
    );
  }

  return (
    <div className="library-page skill-detail-page">
      <PageHeading
        eyebrow={
          <Link href="/skills" className="skill-back-link">
            <ArrowLeft aria-hidden="true" strokeWidth={2} />
            Skills
          </Link>
        }
        title={skill.name}
        actions={
          <div className="library-head-stats" aria-label="Skill details">
            <div>
              <strong>{skill.fileCount}</strong>
              <span>{skill.fileCount === 1 ? "file" : "files"}</span>
            </div>
            <div>
              <strong>{formatShortDate(skill.updatedAt)}</strong>
              <span>updated</span>
            </div>
          </div>
        }
      />

      <SkillWorkspace skill={skill} skillName={name} onReload={load} />
      <ItemAccessPanel itemType="skill" itemId={skill.name} label={skill.name} />
    </div>
  );
}

// The page already owns the h1; a document title renders one level down.
const SKILL_MARKDOWN_COMPONENTS = {
  h1: (props: ComponentPropsWithoutRef<"h1"> & { node?: unknown }) => {
    const { node, ...rest } = props;
    void node;
    return <h2 className="library-markdown-title" {...rest} />;
  },
};

function SkillWorkspace({
  skill,
  skillName,
  onReload,
}: {
  skill: SkillDetail;
  skillName: string;
  onReload: () => Promise<void>;
}) {
  const defaultPath = skill.files.some((f) => f.path === "SKILL.md")
    ? "SKILL.md"
    : (skill.files[0]?.path ?? "SKILL.md");
  const [selected, setSelected] = useState(defaultPath);
  const [file, setFile] = useState<SkillFile | null>(null);
  const [fileState, setFileState] = useState<"loading" | "ready" | "error">(
    "loading",
  );
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const loadFile = useCallback(
    async (path: string, signal?: AbortSignal) => {
      try {
        const res = await fetch(
          `/api/work/skills/${encodeURIComponent(skillName)}/file?path=${encodeURIComponent(path)}`,
          { cache: "no-store", signal },
        );
        if (!res.ok) {
          setFileState("error");
          return;
        }
        const data = (await res.json()) as { file: SkillFile };
        setFile(data.file);
        setFileState("ready");
      } catch (error) {
        if (!(error instanceof DOMException && error.name === "AbortError")) {
          setFileState("error");
        }
      }
    },
    [skillName],
  );

  useEffect(() => {
    const controller = new AbortController();
    const id = window.setTimeout(() => {
      void loadFile(selected, controller.signal);
    }, 0);
    return () => {
      window.clearTimeout(id);
      controller.abort();
    };
  }, [selected, loadFile]);

  const selectFile = (path: string) => {
    if (path === selected) return;
    setEditing(false);
    setSaveError(null);
    setFileState("loading");
    setSelected(path);
  };

  const retry = () => {
    setFileState("loading");
    void loadFile(selected);
  };

  const isSkillMd = selected === "SKILL.md";
  const size = file ? formatBytes(file.bytes) : "";

  const beginEdit = () => {
    if (!file || file.text === null) return;
    setDraft(file.text);
    setSaveError(null);
    setEditing(true);
  };

  const save = async () => {
    setSaving(true);
    setSaveError(null);
    try {
      const res = await fetch(
        `/api/work/skills/${encodeURIComponent(skillName)}/file?path=${encodeURIComponent(selected)}`,
        {
          method: "PUT",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ content: draft }),
        },
      );
      if (!res.ok) {
        const data = (await res.json().catch(() => ({}))) as { error?: string };
        setSaveError(data.error ?? `Save failed (HTTP ${res.status}).`);
        return;
      }
      setEditing(false);
      await loadFile(selected);
      if (isSkillMd) await onReload();
    } catch {
      setSaveError("Save failed. Check your connection and try again.");
    } finally {
      setSaving(false);
    }
  };

  return (
    <main className="skill-detail-workspace">
      <aside className="skill-manifest">
        <section>
          <span className="skill-detail-label">Files</span>
          <ol>
            {skill.files.map((f, index) => (
              <li key={f.path}>
                <button
                  type="button"
                  data-ui-bespoke-reason="Skill file picker selects a file and drives the adjacent viewer"
                  className="skill-file-btn"
                  aria-pressed={selected === f.path}
                  onClick={() => selectFile(f.path)}
                >
                  <span className="library-index">
                    {index + 1}
                  </span>
                  <FileText aria-hidden="true" strokeWidth={1.9} />
                  <span>{f.path}</span>
                  <small>{formatBytes(f.bytes)}</small>
                </button>
              </li>
            ))}
          </ol>
        </section>
      </aside>

      <article className="skill-instructions">
        <div className="skill-file-bar">
          <div className="skill-file-id">
            {isSkillMd ? (
              <span className="skill-file-kicker">Primary instruction</span>
            ) : null}
            <strong>{selected}</strong>
            {file && !editing ? (
              <small>
                {size}
                {file.truncated ? " · truncated" : ""}
              </small>
            ) : null}
          </div>
          <div className="skill-file-actions">
            {editing ? (
              <>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setEditing(false)}
                  disabled={saving}
                >
                  Cancel
                </Button>
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => void save()}
                  disabled={saving}
                >
                  {saving ? "Saving…" : "Save"}
                </Button>
              </>
            ) : file && file.editable ? (
              <Button size="sm" onClick={beginEdit}>
                <Pencil aria-hidden="true" strokeWidth={2} size={14} />
                Edit
              </Button>
            ) : null}
          </div>
        </div>

        {saveError ? (
          <p className="skill-save-error" role="alert">
            {saveError}
          </p>
        ) : null}

        {fileState === "loading" ? (
          <div className="library-loading" role="status" aria-label="Loading file">
            <span />
            <span />
            <span />
          </div>
        ) : fileState === "error" ? (
          <div className="skill-file-note">
            This file could not be loaded.{" "}
            <Button variant="ghost" size="sm" onClick={retry}>
              Retry
            </Button>
          </div>
        ) : editing ? (
          <Textarea
            aria-label={`Edit ${selected}`}
            className="skill-editor font-mono"
            value={draft}
            spellCheck={false}
            onChange={(event) => setDraft(event.target.value)}
          />
        ) : !file ? null : file.binary ? (
          <p className="skill-file-note">
            This is a binary file ({size}). OpenNeko can’t preview or edit it here.
          </p>
        ) : isSkillMd ? (
          <>
            {skill.description ? (
              <p className="skill-detail-summary">{skill.description}</p>
            ) : null}
            <div className="library-markdown">
              <ReactMarkdown components={SKILL_MARKDOWN_COMPONENTS}>
                {stripFrontmatter(file.text ?? "")}
              </ReactMarkdown>
            </div>
            {file.truncated ? (
              <p className="skill-file-note">
                Shown truncated. The full file is larger than the editor limit.
              </p>
            ) : null}
          </>
        ) : (
          <>
            <pre className="skill-code">
              <code>{file.text}</code>
            </pre>
            {file.truncated ? (
              <p className="skill-file-note">
                Shown truncated. The full file is larger than the editor limit.
              </p>
            ) : null}
          </>
        )}
      </article>
    </main>
  );
}

function formatShortDate(value: string): string {
  return new Date(value)
    .toLocaleDateString("en-IN", { month: "short", day: "numeric" })
    .toUpperCase();
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  return `${(bytes / 1024).toFixed(bytes < 10_240 ? 1 : 0)} KB`;
}

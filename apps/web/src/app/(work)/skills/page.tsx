"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { ArrowUpRight, FileText, Trash2, Upload } from "lucide-react";
import { confirmDialog } from "@/components/ConfirmModal";
import PageHeading from "@/components/PageHeading";
import { Button } from "@/components/ui/button";
import { MenuItem, OverflowMenu } from "@/components/ui/overflow-menu";
import { EmptyState } from "@/components/ui/empty";
import { SearchInput } from "@/components/ui/search-input";
import { Input } from "@/components/ui/input";
import { matchesListSearch } from "@/lib/list-search";

type SkillSummary = {
  name: string;
  description: string;
  fileCount: number;
  updatedAt: string;
};

async function fetchSkills(signal?: AbortSignal): Promise<{ skills: SkillSummary[]; canImport: boolean }> {
  const response = await fetch("/api/work/skills", {
    cache: "no-store",
    signal,
  });
  if (!response.ok) throw new Error("Skills could not be loaded.");
  const data = (await response.json()) as { skills?: SkillSummary[]; canImport?: boolean };
  return { skills: data.skills ?? [], canImport: data.canImport === true };
}

export default function SkillsPage() {
  const router = useRouter();
  const archiveInput = useRef<HTMLInputElement>(null);
  const [skills, setSkills] = useState<SkillSummary[]>([]);
  const [canImport, setCanImport] = useState(false);
  const [importing, setImporting] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busyName, setBusyName] = useState<string | null>(null);
  const [query, setQuery] = useState("");

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const data = await fetchSkills();
      setSkills(data.skills);
      setCanImport(data.canImport);
      setError(null);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Skills could not be loaded.");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void fetchSkills(controller.signal)
      .then((data) => {
        setSkills(data.skills);
        setCanImport(data.canImport);
        setError(null);
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        setError(cause instanceof Error ? cause.message : "Skills could not be loaded.");
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, []);

  const importArchive = useCallback(async (file: File | undefined) => {
    if (!file) return;
    if (file.size > 16 * 1024 * 1024) {
      toast.error("Skill archive must be at most 16 MB.");
      if (archiveInput.current) archiveInput.current.value = "";
      return;
    }
    setImporting(true);
    try {
      const form = new FormData();
      form.set("file", file);
      const response = await fetch("/api/work/skills", { method: "POST", body: form });
      const data = await response.json() as { name?: string; error?: string };
      if (!response.ok || !data.name) throw new Error(data.error ?? "Skill could not be imported.");
      toast.success(`Imported ${data.name}.`);
      router.push(`/skills/${encodeURIComponent(data.name)}`);
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : "Skill could not be imported.");
    } finally {
      setImporting(false);
      if (archiveInput.current) archiveInput.current.value = "";
    }
  }, [router]);

  const remove = useCallback(
    async (skillName: string) => {
      const ok = await confirmDialog({
        title: `Delete skill "${skillName}"?`,
        description: "This removes the skill folder from the organization workspace.",
        confirmLabel: "Delete",
        destructive: true,
      });
      if (!ok) return;
      setBusyName(skillName);
      try {
        const response = await fetch(
          `/api/work/skills/${encodeURIComponent(skillName)}`,
          { method: "DELETE" },
        );
        if (!response.ok) throw new Error("Skill could not be deleted.");
        await refresh();
      } catch (cause) {
        setError(
          cause instanceof Error ? cause.message : "Skill could not be deleted.",
        );
      } finally {
        setBusyName(null);
      }
    },
    [refresh],
  );

  const totalFiles = useMemo(
    () => skills.reduce((total, skill) => total + skill.fileCount, 0),
    [skills],
  );
  const visibleSkills = useMemo(
    () =>
      skills.filter((skill) =>
        matchesListSearch(query, skill.name, skill.description),
      ),
    [query, skills],
  );

  return (
    <div className="library-page skills-library">
      <PageHeading
        title="Skills"
        description="Capabilities your agents can call while they work a run."
        actions={
          <div className="flex flex-wrap items-center gap-4">
            <div className="library-head-stats" aria-label="Skill inventory">
              <div>
                <strong>{skills.length}</strong>
                <span>installed</span>
              </div>
              <div>
                <strong>{totalFiles}</strong>
                <span>files</span>
              </div>
            </div>
            {canImport ? (
              <Button variant="primary" disabled={importing} onClick={() => archiveInput.current?.click()}>
                <Upload aria-hidden="true" />{importing ? "Importing…" : "Import skill"}
              </Button>
            ) : null}
          </div>
        }
      />

      <Input
        ref={archiveInput}
        type="file"
        accept=".skill,.zip,application/zip"
        hidden
        aria-label="Choose a skill archive"
        onChange={(event) => void importArchive(event.target.files?.[0])}
      />

      <main className="library-main">
        <SearchInput
          label="Search skills"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Search installed skills"
        />

        {error ? (
          <div className="library-error" role="alert">
            <div>
              <strong>Skills unavailable</strong>
              <span>{error}</span>
            </div>
            <Button
              variant="danger"
              size="sm"
              className="shrink-0"
              onClick={() => void refresh()}
            >
              Retry
            </Button>
          </div>
        ) : null}

        <section className="library-section">
          <header className="library-section-head">
            <div>
              <span>Installed skills</span>
              <h2>Installed skills</h2>
            </div>
          </header>

          {loading ? (
            <div className="library-loading" role="status" aria-label="Loading skills">
              <span />
              <span />
              <span />
            </div>
          ) : visibleSkills.length === 0 ? (
            <EmptyState
              className="library-empty"
              title={query ? "No matching skills" : "No installed skills"}
              body={
                query
                  ? "Try another skill name or trigger phrase."
                  : "Skills appear here when OpenNeko saves a reusable capability or an administrator imports one."
              }
            />
          ) : (
            <>
              <div className="skills-table-head" aria-hidden="true">
                <span />
                <span>Skill</span>
                <span>When it is used</span>
                <span>Files</span>
                <span>Updated</span>
                <span />
              </div>
              <ol className="skills-index">
                {visibleSkills.map((skill, index) => (
                  <li key={skill.name} className="skill-index-row">
                    <Link
                      href={`/skills/${encodeURIComponent(skill.name)}`}
                      className="skill-index-link"
                    >
                      <span className="library-index">
                        {index + 1}
                      </span>
                      <strong>{skill.name}</strong>
                      <p>{skill.description || "No trigger description provided."}</p>
                      <span className="skill-file-count">
                        <FileText aria-hidden="true" strokeWidth={1.9} />
                        {skill.fileCount}
                      </span>
                      <span className="skill-updated">{formatDate(skill.updatedAt)}</span>
                      <ArrowUpRight
                        className="skill-row-arrow"
                        aria-hidden="true"
                        strokeWidth={1.9}
                      />
                    </Link>
                    <OverflowMenu
                      label={`Actions for ${skill.name}`}
                      className="skill-delete-control"
                    >
                      <MenuItem danger disabled={busyName === skill.name} onSelect={() => void remove(skill.name)}>
                        <Trash2 aria-hidden="true" strokeWidth={1.9} />
                        Delete skill
                      </MenuItem>
                    </OverflowMenu>
                  </li>
                ))}
              </ol>
            </>
          )}
        </section>
        {canImport ? (
          <p className="text-ui-caption text-text3">
            Import a .skill or ZIP archive that holds one skill directory with SKILL.md and its supporting files.
          </p>
        ) : null}
      </main>
    </div>
  );
}

function formatDate(value: string): string {
  return new Date(value).toLocaleDateString("en-IN", {
    month: "short",
    day: "numeric",
    year: "2-digit",
  });
}

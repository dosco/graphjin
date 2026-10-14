"use client";

import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import Link from "next/link";
import type {
  ContextRemoteKind,
  ContextRemoteMode,
  ContextRemoteSettings,
  ItemDiff,
  PublishPreviewItem,
  PublishResult,
  RemoteUpdates,
  UpdateChoices,
} from "@neko/llm/config-vcs";
import { AdminError } from "@/components/admin/AdminError";
import { adminApi } from "@/components/admin/admin-api";
import AppHeader from "@/components/AppHeader";
import CreatorCredit from "@/components/CreatorCredit";
import PageHeading from "@/components/PageHeading";
import SectionNav from "@/components/SectionNav";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, Input, NativeSelect } from "@/components/ui/field";
import { copyText } from "@/lib/copy-text";

const KINDS: Array<{ kind: ContextRemoteKind; label: string; private?: boolean }> = [
  { kind: "skills", label: "Skills" },
  { kind: "skill-overlays", label: "Skill learnings" },
  { kind: "workflows", label: "Workflows" },
  { kind: "memory", label: "Memories", private: true },
  { kind: "library", label: "Library concepts", private: true },
];

type Draft = { url: string; mode: ContextRemoteMode; branch: string; kinds: ContextRemoteKind[]; username: string; token: string };

function toDraft(remote: ContextRemoteSettings | null): Draft {
  return {
    url: remote?.url ?? "",
    mode: remote?.mode ?? "pull_request",
    branch: remote?.branch ?? "main",
    kinds: remote?.kinds ?? ["skills", "skill-overlays", "workflows"],
    username: remote?.username ?? "",
    token: "",
  };
}

export default function RepositoryForm({ initial }: { initial: ContextRemoteSettings | null }) {
  const [remote, setRemote] = useState(initial);
  const [draft, setDraft] = useState<Draft>(() => toDraft(initial));
  const [busy, setBusy] = useState<"save" | "publish" | "remove" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [preview, setPreview] = useState<{ items: PublishPreviewItem[] | null; error: string | null; loading: boolean }>(
    { items: null, error: null, loading: false },
  );

  const loadPreview = useCallback(async () => {
    setPreview((p) => ({ ...p, loading: true, error: null }));
    const result = await adminApi<{ items: PublishPreviewItem[] }>("/api/admin/context-remote/publish");
    setPreview(result.ok ? { items: result.body.items, error: null, loading: false } : { items: null, error: result.error, loading: false });
  }, []);

  useEffect(() => {
    if (!remote) return;
    const id = window.setTimeout(() => void loadPreview(), 0);
    return () => window.clearTimeout(id);
  }, [remote, loadPreview]);

  async function save(event: React.FormEvent) {
    event.preventDefault();
    setBusy("save");
    setError(null);
    const { token, ...rest } = draft;
    const result = await adminApi<{ remote: ContextRemoteSettings }>("/api/admin/context-remote", "PUT", {
      ...rest,
      ...(token.trim() ? { token } : {}),
    });
    setBusy(null);
    if (!result.ok) return setError(result.error);
    setRemote(result.body.remote);
    setDraft(toDraft(result.body.remote));
    toast.success("Repository saved.");
  }

  async function publish() {
    setBusy("publish");
    setError(null);
    const result = await adminApi<{ result: PublishResult; remote: ContextRemoteSettings }>("/api/admin/context-remote/publish", "POST");
    setBusy(null);
    if (!result.ok) {
      setError(result.error);
      const refreshed = await adminApi<{ remote: ContextRemoteSettings | null }>("/api/admin/context-remote");
      if (refreshed.ok) setRemote(refreshed.body.remote);
      return;
    }
    setRemote(result.body.remote);
    toast.success(result.body.result.detail);
  }

  async function disconnect() {
    setBusy("remove");
    setError(null);
    const result = await adminApi<{ remote: null }>("/api/admin/context-remote", "DELETE");
    setBusy(null);
    if (!result.ok) return setError(result.error);
    setRemote(null);
    setDraft(toDraft(null));
    toast.success("Repository disconnected.");
  }

  const sshDraft = /^ssh:\/\//.test(draft.url.trim()) || /^[^\s@/]+@[^\s:/]+:(?!\/)/.test(draft.url.trim());

  async function newKey() {
    if (!remote) return;
    setBusy("save");
    setError(null);
    const result = await adminApi<{ remote: ContextRemoteSettings }>("/api/admin/context-remote", "PUT", {
      url: remote.url, mode: remote.mode, branch: remote.branch, kinds: remote.kinds, username: remote.username, newSshKey: true,
    });
    setBusy(null);
    if (!result.ok) return setError(result.error);
    setRemote(result.body.remote);
    toast.success("New deploy key created. Replace the old key in the repository.");
  }

  async function confirmHostKey() {
    setBusy("save");
    setError(null);
    const result = await adminApi<{ remote: ContextRemoteSettings }>("/api/admin/context-remote/host-key", "POST");
    setBusy(null);
    if (!result.ok) return setError(result.error);
    setRemote(result.body.remote);
    toast.success("Host key confirmed.");
  }

  const toggle = (kind: ContextRemoteKind, on: boolean) =>
    setDraft((d) => ({ ...d, kinds: on ? [...d.kinds, kind] : d.kinds.filter((k) => k !== kind) }));

  return (
    <>
      <div className="root">
        <AppHeader back={{ href: "/admin/settings", label: "Settings" }}>
          <SectionNav current="admin" />
        </AppHeader>

        <PageHeading
          title="Context repository"
          description="OpenNeko keeps a git history of the company's skills, workflows and other context. Publish it to a repository on GitHub, GitLab or another git host."
        />

        {remote ? (
          <section className="settings-card mb-6 flex flex-col gap-3" aria-label="Publishing">
            <div className="flex items-start justify-between gap-4 max-[720px]:flex-col">
              <div className="flex flex-col gap-1">
                <h2 className="settings-card-title">Publish</h2>
                <p className="settings-card-copy">
                  {remote.mode === "push"
                    ? `Pushes the company version to ${remote.branch}.`
                    : `Opens a pull request against ${remote.branch}.`}{" "}
                  OpenNeko replaces only the folders it publishes. Other files in the repository stay as they are.
                </p>
                {remote.lastPublish ? (
                  <p className={`text-ui-body-sm ${remote.lastPublish.status === "ok" ? "text-text2" : "text-danger"}`}>
                    {remote.lastPublish.at.slice(0, 16).replace("T", " ")} UTC · {remote.lastPublish.detail}{" "}
                    {remote.lastPublish.link ? (
                      <a className="underline" href={remote.lastPublish.link} target="_blank" rel="noreferrer">
                        Open
                      </a>
                    ) : null}
                  </p>
                ) : null}
                <PublishPreview {...preview} />
              </div>
              <Button
                type="button"
                variant="primary"
                disabled={busy !== null || preview.loading || preview.items?.length === 0}
                onClick={() => void publish()}
              >
                {busy === "publish" ? "Publishing…" : remote.mode === "push" ? "Push now" : "Open pull request"}
              </Button>
            </div>
          </section>
        ) : null}

        {remote?.ssh ? (
          <section className="settings-card mb-6 flex flex-col gap-4" aria-label="SSH access">
            <div>
              <h2 className="settings-card-title">SSH access</h2>
              <p className="settings-card-copy">
                Add this key to the repository as a deploy key with write access. On GitHub: Settings, Deploy keys. On GitLab: Settings, Repository, Deploy keys.
              </p>
            </div>
            <div className="flex items-start gap-2 max-[720px]:flex-col">
              <code className="min-w-0 flex-1 break-all rounded-[4px] bg-neutral px-2 py-1.5 font-mono text-ui-caption text-text2">
                {remote.ssh.publicKey}
              </code>
              <Button type="button" size="sm" variant="secondary"
                onClick={() => void copyText(remote.ssh!.publicKey).then(() => toast.success("Key copied."))}>
                Copy
              </Button>
              <Button type="button" size="sm" variant="secondary" disabled={busy !== null} onClick={() => void newKey()}>
                Create a new key
              </Button>
            </div>
            <div className="flex flex-col gap-1.5">
              <span className="text-ui-body-sm text-text">Server host keys</span>
              <ul className="flex flex-col gap-0.5">
                {remote.ssh.hostFingerprints.map((fingerprint) => (
                  <li key={fingerprint} className="font-mono text-ui-caption text-text2">{fingerprint}</li>
                ))}
              </ul>
              {remote.ssh.hostConfirmed ? (
                <p className="text-ui-caption text-success-mid">Confirmed. OpenNeko connects only to a server with these keys.</p>
              ) : (
                <div className="flex items-start justify-between gap-4 max-[720px]:flex-col">
                  <p className="text-ui-caption text-warn-ink">
                    Compare these fingerprints with the ones your git host publishes. OpenNeko does not connect until you confirm them.
                  </p>
                  <Button type="button" size="sm" variant="primary" disabled={busy !== null} onClick={() => void confirmHostKey()}>
                    Confirm host key
                  </Button>
                </div>
              )}
            </div>
          </section>
        ) : null}

        {remote ? <Updates onApplied={() => void loadPreview()} /> : null}

        <form onSubmit={save} className="settings-card flex flex-col gap-5">
          <div>
            <h2 className="settings-card-title">Remote repository</h2>
            <p className="settings-card-copy">
              Use an empty repository, or one where OpenNeko may own the folders you choose below. Personal changes are never published.
            </p>
          </div>
          <AdminError message={error} />
          <div className="grid grid-cols-2 gap-4 max-[720px]:grid-cols-1">
            <Field
              label="Repository address"
              htmlFor="repo-url"
              hint="HTTPS, such as https://github.com/acme/openneko-context.git, or SSH, such as git@github.com:acme/openneko-context.git."
            >
              <Input
                id="repo-url"
                type="text"
                spellCheck={false}
                required
                value={draft.url}
                onChange={(e) => setDraft((d) => ({ ...d, url: e.target.value }))}
              />
            </Field>
            <Field label="Branch" htmlFor="repo-branch" hint="The branch OpenNeko pushes to, or opens pull requests against.">
              <Input
                id="repo-branch"
                required
                value={draft.branch}
                onChange={(e) => setDraft((d) => ({ ...d, branch: e.target.value }))}
              />
            </Field>
            <Field
              label={sshDraft ? "Access token for pull requests" : "Access token"}
              htmlFor="repo-token"
              hint={remote?.hasToken
                ? "A token is saved. Enter a new one to replace it."
                : sshDraft
                  ? "Optional. OpenNeko pushes with its SSH deploy key. A token lets it also open pull requests on GitHub or GitLab."
                  : "A token that can write to the repository and open pull requests. OpenNeko stores it encrypted."}
            >
              <Input
                id="repo-token"
                type="password"
                autoComplete="off"
                value={draft.token}
                onChange={(e) => setDraft((d) => ({ ...d, token: e.target.value }))}
              />
            </Field>
            {sshDraft ? null : (
              <Field label="Username" htmlFor="repo-username" hint="Leave blank for GitHub and GitLab. Some hosts need the account name for the token.">
                <Input
                  id="repo-username"
                  autoComplete="off"
                  value={draft.username}
                  onChange={(e) => setDraft((d) => ({ ...d, username: e.target.value }))}
                />
              </Field>
            )}
            <Field label="How to publish" htmlFor="repo-mode" hint="A pull request lets someone review the change on the git host first.">
              <NativeSelect
                id="repo-mode"
                value={draft.mode}
                onChange={(e) => setDraft((d) => ({ ...d, mode: e.target.value as ContextRemoteMode }))}
              >
                <option value="pull_request">Open a pull request</option>
                <option value="push">Push to the branch</option>
              </NativeSelect>
            </Field>
          </div>
          <fieldset className="flex flex-col gap-2">
            <legend className="mb-1 text-ui-body-sm text-text">What to publish</legend>
            {KINDS.map(({ kind, label, private: sensitive }) => (
              <div key={kind} className="grid gap-1">
                <Checkbox
                  label={label}
                  checked={draft.kinds.includes(kind)}
                  onCheckedChange={(checked) => toggle(kind, checked === true)}
                />
                {sensitive ? (
                  <p className="pl-6 text-ui-caption text-text3">Holds business facts. Publish it only to a private repository.</p>
                ) : null}
              </div>
            ))}
          </fieldset>
          <div className="flex justify-end gap-2">
            {remote ? (
              <Button type="button" variant="secondary" disabled={busy !== null} onClick={() => void disconnect()}>
                {busy === "remove" ? "Disconnecting…" : "Disconnect"}
              </Button>
            ) : null}
            <Button type="submit" variant="primary" disabled={busy !== null}>
              {busy === "save" ? "Saving…" : "Save"}
            </Button>
          </div>
        </form>
      </div>

      <CreatorCredit />
    </>
  );
}

const KIND_LABELS: Record<ContextRemoteKind, string> = {
  skills: "Skill",
  "skill-overlays": "Skill learning",
  workflows: "Workflow",
  memory: "Memory",
  library: "Library concept",
};

/** A unified diff, one file at a time, with added and removed lines tinted. */
function DiffView({ diff, truncated }: ItemDiff) {
  if (!diff.trim()) return <p className="text-ui-caption text-text3">No line changes.</p>;
  const lines = diff.split("\n").filter((line) => !/^(index |--- |\+\+\+ |new file mode|deleted file mode|similarity |\\ No newline)/.test(line));
  return (
    <div className="max-h-96 overflow-auto rounded-[4px] border border-border bg-card">
      <pre className="min-w-max font-mono text-ui-caption leading-5">
        {lines.map((line, index) => {
          if (line.startsWith("diff --git ")) {
            const path = line.split(" b/").pop();
            return <div key={index} className="sticky left-0 border-y border-border bg-neutral px-2 py-1 text-text">{path}</div>;
          }
          const tone = line.startsWith("@@") ? "text-text3"
            : line.startsWith("+") ? "bg-success-soft text-text"
            : line.startsWith("-") ? "bg-danger-soft text-text"
            : "text-text2";
          return <div key={index} className={`px-2 ${tone}`}>{line || " "}</div>;
        })}
      </pre>
      {truncated ? <p className="px-2 py-1 text-ui-caption text-warn-ink">The diff is long. OpenNeko shows the first part only.</p> : null}
    </div>
  );
}

/** A "Show changes" toggle that loads a diff from the given address the first time it opens. */
function ChangesToggle({ url }: { url: string }) {
  const [open, setOpen] = useState(false);
  const [state, setState] = useState<{ diff: ItemDiff | null; error: string | null }>({ diff: null, error: null });
  async function toggle() {
    const next = !open;
    setOpen(next);
    if (next && !state.diff) {
      const result = await adminApi<ItemDiff>(url);
      setState(result.ok ? { diff: result.body, error: null } : { diff: null, error: result.error });
    }
  }
  return (
    <div className="flex flex-col gap-1.5">
      <Button type="button" size="sm" variant="ghost" className="self-start" onClick={() => void toggle()}>
        {open ? "Hide changes" : "Show changes"}
      </Button>
      {open ? (
        state.error ? <p className="text-ui-caption text-danger">{state.error}</p>
          : state.diff ? <DiffView {...state.diff} />
          : <p className="text-ui-caption text-text3">Loading…</p>
      ) : null}
    </div>
  );
}

function PublishPreview({ items, error, loading }: { items: PublishPreviewItem[] | null; error: string | null; loading: boolean }) {
  if (loading) return <p className="text-ui-body-sm text-text3">Checking what would be published…</p>;
  if (error) return <p className="text-ui-body-sm text-danger">{error}</p>;
  if (!items) return null;
  if (items.length === 0) {
    return <p className="text-ui-body-sm text-text2">Nothing to publish. The repository has OpenNeko&apos;s current version.</p>;
  }
  const total = (item: PublishPreviewItem) => item.added + item.changed + item.removed;
  const counts = (item: PublishPreviewItem) =>
    [
      item.changed ? `${item.changed} changed` : "",
      item.added ? `${item.added} new` : "",
      item.removed ? `${item.removed} removed` : "",
    ].filter(Boolean).join(", ");
  return (
    <div className="flex flex-col gap-1">
      <span className="text-ui-body-sm text-text">Not yet published</span>
      <ul className="flex flex-col gap-0.5">
        {items.map((item) => (
          <li key={`${item.kind}/${item.name}`} className="flex flex-col gap-1 text-ui-body-sm text-text2">
            <span>{KIND_LABELS[item.kind]}: {item.name} · {total(item)} {total(item) === 1 ? "file" : "files"} ({counts(item)})</span>
            <ChangesToggle url={`/api/admin/context-remote/publish?kind=${encodeURIComponent(item.kind)}&name=${encodeURIComponent(item.name)}`} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function Updates({ onApplied }: { onApplied: () => void }) {
  const [updates, setUpdates] = useState<RemoteUpdates | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [addedPacks, setAddedPacks] = useState<string[]>([]);

  async function check() {
    setBusy("check");
    setError(null);
    const result = await adminApi<{ updates: RemoteUpdates }>("/api/admin/context-remote/updates");
    setBusy(null);
    if (!result.ok) return setError(result.error);
    setUpdates(result.body.updates);
  }

  async function apply(key: string, choices: UpdateChoices) {
    setBusy(key);
    setError(null);
    const result = await adminApi<{ updates: RemoteUpdates; packs: Array<{ id: string; ok: boolean; error?: string }> }>(
      "/api/admin/context-remote/updates",
      "POST",
      choices,
    );
    setBusy(null);
    if (!result.ok) return setError(result.error);
    setUpdates(result.body.updates);
    const failed = result.body.packs.find((pack) => !pack.ok);
    if (failed) return setError(`Pack ${failed.id} was not added: ${failed.error}`);
    setAddedPacks((ids) => [...ids, ...result.body.packs.map((pack) => pack.id)]);
    onApplied();
    toast.success(choices.packs.length ? "Pack added. Install it on the Packs page." : "Skills updated.");
  }

  const nothing = updates && updates.skills.length === 0 && updates.packs.length === 0;
  return (
    <section className="settings-card mb-6 flex flex-col gap-3" aria-label="Updates from the repository">
      <div className="flex items-start justify-between gap-4 max-[720px]:flex-col">
        <div className="flex flex-col gap-1">
          <h2 className="settings-card-title">Updates from the repository</h2>
          <p className="settings-card-copy">Skills and packs that someone changed in the repository. Nothing changes in OpenNeko until you choose.</p>
        </div>
        <Button type="button" variant="secondary" disabled={busy !== null} onClick={() => void check()}>
          {busy === "check" ? "Checking…" : "Check for updates"}
        </Button>
      </div>
      <AdminError message={error} />
      {nothing ? <p className="text-ui-body-sm text-text2">OpenNeko has everything in the repository.</p> : null}
      {updates?.skills.length ? (
        <ul className="flex flex-col gap-3">
          {updates.skills.map((skill) => (
            <li key={skill.name} className="flex items-start justify-between gap-4 max-[720px]:flex-col">
              <div className="flex flex-col gap-0.5">
                <span className="text-ui-body-sm text-text">Skill: {skill.name}</span>
                <span className="text-ui-caption text-text3">
                  {skill.status === "update"
                    ? "Changed in the repository."
                    : "Changed in both OpenNeko and the repository. Choose which version to keep."}
                </span>
                <ChangesToggle url={`/api/admin/context-remote/updates?skill=${encodeURIComponent(skill.name)}`} />
              </div>
              <div className="flex gap-2">
                {skill.status === "both_changed" ? (
                  <Button type="button" size="sm" variant="secondary" disabled={busy !== null}
                    onClick={() => void apply(skill.name, { skills: [{ name: skill.name, choice: "local" }], packs: [] })}>
                    Keep OpenNeko&apos;s
                  </Button>
                ) : null}
                <Button type="button" size="sm" variant="primary" disabled={busy !== null}
                  onClick={() => void apply(skill.name, { skills: [{ name: skill.name, choice: "remote" }], packs: [] })}>
                  {skill.status === "update" ? "Bring in" : "Use the repository's"}
                </Button>
              </div>
            </li>
          ))}
        </ul>
      ) : null}
      {updates?.packs.length ? (
        <ul className="flex flex-col gap-3">
          {updates.packs.map((pack) => (
            <li key={pack.id} className="flex items-start justify-between gap-4 max-[720px]:flex-col">
              <div className="flex flex-col gap-0.5">
                <span className="text-ui-body-sm text-text">Pack: {pack.id}</span>
                <span className={`text-ui-caption ${pack.status === "invalid" ? "text-danger" : "text-text3"}`}>{pack.detail}</span>
              </div>
              {pack.status === "invalid" ? null : (
                <Button type="button" size="sm" variant="primary" disabled={busy !== null}
                  onClick={() => void apply(pack.id, { skills: [], packs: [pack.id] })}>
                  Add
                </Button>
              )}
            </li>
          ))}
        </ul>
      ) : null}
      {addedPacks.length ? (
        <p className="text-ui-body-sm text-text2">
          Added {addedPacks.join(", ")}. <Link className="underline" href="/admin/settings/packs">Install on the Packs page</Link>
        </p>
      ) : null}
    </section>
  );
}

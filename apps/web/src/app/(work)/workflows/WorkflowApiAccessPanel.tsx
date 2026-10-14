"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  useSyncExternalStore,
} from "react";
import { Check, Copy, KeyRound, RotateCw, Save, ShieldOff } from "lucide-react";
import { confirmDialog } from "@/components/ConfirmModal";
import { ActionGroup } from "@/components/ui/action-group";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Disclosure } from "@/components/ui/disclosure";
import { Field, Input } from "@/components/ui/field";
import { Badge } from "@/components/ui/badge";
import { copyText as writeClipboard } from "@/lib/copy-text";

type WorkflowApiLimits = {
  requestLimitPerMinute: number;
  pollLimitPerMinute: number;
  queueCap: number;
  concurrencyCap: number;
  batchMaxRecords: number;
  batchChunkSize: number;
  maxRequestBytes: number;
  maxResultBytes: number;
  maxArtifactBytes: number;
  maxRuntimeSeconds: number;
  maxModelCalls: number;
  maxToolCalls: number;
  maxTokensPerRun: number;
  maxCostMicrosPerRun: number;
  rollingWindowSeconds: number;
  rollingTokenBudget: number;
  rollingCostMicrosBudget: number;
  retentionHours: number;
};

type WorkflowApiAccess = {
  workflowId: string;
  enabled: boolean;
  tokenPrefix: string | null;
  tokenCreatedAt: string | null;
  tokenRotatedAt: string | null;
  lastUsedAt: string | null;
  limits: WorkflowApiLimits;
  includeTelemetry: boolean;
  batch: {
    available: boolean;
    recordsField: string | null;
    columns: string[];
  };
  createdAt: string | null;
  updatedAt: string | null;
};

type LimitKey = keyof WorkflowApiLimits;

const LIMIT_DEFINITIONS: Array<{
  key: LimitKey;
  label: string;
  group: "Traffic" | "Execution" | "Batch and retention" | "Rolling budget";
  min: number;
  max: number;
  unit: string;
}> = [
  { key: "requestLimitPerMinute", label: "Invocations", group: "Traffic", min: 1, max: 600, unit: "per minute" },
  { key: "pollLimitPerMinute", label: "Status polls", group: "Traffic", min: 1, max: 1_200, unit: "per minute" },
  { key: "queueCap", label: "Queued runs", group: "Traffic", min: 1, max: 250, unit: "runs" },
  { key: "concurrencyCap", label: "Concurrent runs", group: "Traffic", min: 1, max: 20, unit: "runs" },
  { key: "maxRuntimeSeconds", label: "Runtime", group: "Execution", min: 30, max: 1_800, unit: "seconds" },
  { key: "maxModelCalls", label: "Model calls", group: "Execution", min: 1, max: 1_024, unit: "per run" },
  { key: "maxToolCalls", label: "Tool calls", group: "Execution", min: 1, max: 1_024, unit: "per run" },
  { key: "maxTokensPerRun", label: "Tokens", group: "Execution", min: 1_000, max: 3_000_000, unit: "per run" },
  { key: "maxCostMicrosPerRun", label: "Provider spend", group: "Execution", min: 1_000, max: 100_000_000, unit: "USD micros per run" },
  { key: "maxRequestBytes", label: "Request body", group: "Execution", min: 1_024, max: 1_048_576, unit: "bytes" },
  { key: "maxResultBytes", label: "Inline result", group: "Execution", min: 1_024, max: 1_048_576, unit: "bytes" },
  { key: "batchMaxRecords", label: "Batch records", group: "Batch and retention", min: 1, max: 10_000, unit: "records" },
  { key: "batchChunkSize", label: "Batch chunk", group: "Batch and retention", min: 1, max: 500, unit: "records" },
  { key: "maxArtifactBytes", label: "CSV artifact", group: "Batch and retention", min: 1_024, max: 52_428_800, unit: "bytes" },
  { key: "retentionHours", label: "Result retention", group: "Batch and retention", min: 1, max: 720, unit: "hours" },
  { key: "rollingWindowSeconds", label: "Budget window", group: "Rolling budget", min: 3_600, max: 604_800, unit: "seconds" },
  { key: "rollingCostMicrosBudget", label: "Spend budget", group: "Rolling budget", min: 1_000, max: 1_000_000_000, unit: "USD micros" },
];

const LIMIT_GROUPS = [
  "Traffic",
  "Execution",
  "Batch and retention",
  "Rolling budget",
] as const;

function formatDate(value: string | null): string {
  if (!value) return "Never";
  return new Date(value).toLocaleString("en-IN", {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "numeric",
    minute: "2-digit",
    hour12: true,
  });
}

function errorMessage(value: unknown, fallback: string): string {
  if (!value || typeof value !== "object") return fallback;
  const error = (value as { error?: unknown }).error;
  if (typeof error === "string") return error;
  if (error && typeof error === "object") {
    const message = (error as { message?: unknown }).message;
    if (typeof message === "string") return message;
  }
  return fallback;
}

function subscribeToOrigin(): () => void {
  return () => undefined;
}

export function WorkflowApiAccessPanel({ workflowId }: { workflowId: string }) {
  const [access, setAccess] = useState<WorkflowApiAccess | null>(null);
  const [lastRequest, setLastRequest] = useState<Record<string, unknown> | null>(null);
  const [draft, setDraft] = useState<Record<LimitKey, string> | null>(null);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [revealedToken, setRevealedToken] = useState<string | null>(null);
  const [copied, setCopied] = useState<string | null>(null);
  const origin = useSyncExternalStore(
    subscribeToOrigin,
    () => window.location.origin,
    () => "https://your-openneko.example",
  );

  const apiPath = `/api/v1/workflows/${workflowId}/runs`;
  const singleExample = useMemo(
    () =>
      [
        `curl -X POST "${origin}${apiPath}?mode=single" \\`,
        '  -H "Authorization: Bearer $WORKFLOW_API_TOKEN" \\',
        '  -H "Idempotency-Key: $(uuidgen)" \\',
        '  -H "Content-Type: application/json" \\',
        "  --data @- <<'JSON'",
        JSON.stringify(lastRequest ?? {}, null, 2),
        "JSON",
      ].join("\n"),
    [apiPath, lastRequest, origin],
  );
  const batchExample = useMemo(
    () => {
      const recordsField = access?.batch.recordsField ?? "records";
      return [
        `curl -X POST "${origin}${apiPath}?mode=batch" \\`,
        '  -H "Authorization: Bearer $WORKFLOW_API_TOKEN" \\',
        '  -H "Idempotency-Key: $(uuidgen)" \\',
        '  -H "Content-Type: application/json" \\',
        "  --data @- <<'JSON'",
        JSON.stringify({ [recordsField]: [{ id: "1" }, { id: "2" }] }, null, 2),
        "JSON",
      ].join("\n");
    },
    [access?.batch.recordsField, apiPath, origin],
  );

  const applyAccess = useCallback((next: WorkflowApiAccess) => {
    setAccess(next);
    setDraft(
      Object.fromEntries(
        Object.entries(next.limits).map(([key, value]) => [key, String(value)]),
      ) as Record<LimitKey, string>,
    );
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await fetch(`/api/workflows/${workflowId}/api-access`, {
        cache: "no-store",
      });
      if (response.status === 403) {
        setForbidden(true);
        return;
      }
      const body = (await response.json().catch(() => ({}))) as {
        access?: WorkflowApiAccess;
        lastRequest?: Record<string, unknown> | null;
        error?: unknown;
      };
      if (!response.ok || !body.access) {
        throw new Error(errorMessage(body, "API access could not be loaded."));
      }
      setForbidden(false);
      setLastRequest(body.lastRequest ?? null);
      applyAccess(body.access);
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "API access could not be loaded.",
      );
    } finally {
      setLoading(false);
    }
  }, [applyAccess, workflowId]);

  useEffect(() => {
    const timer = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(timer);
  }, [load]);

  const lifecycle = useCallback(
    async (action: "enable" | "rotate") => {
      setBusy(action);
      setError(null);
      setNotice(null);
      try {
        const response = await fetch(`/api/workflows/${workflowId}/api-access`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ action }),
        });
        const body = (await response.json().catch(() => ({}))) as {
          access?: WorkflowApiAccess;
          token?: string;
          error?: unknown;
        };
        if (!response.ok || !body.access || !body.token) {
          throw new Error(errorMessage(body, `Could not ${action} API access.`));
        }
        applyAccess(body.access);
        setRevealedToken(body.token);
        setNotice(
          action === "enable"
            ? "API access enabled. Save the token now; OpenNeko cannot show it again."
            : "Token rotated. The previous token stopped working immediately.",
        );
      } catch (cause) {
        setError(
          cause instanceof Error ? cause.message : `Could not ${action} API access.`,
        );
      } finally {
        setBusy(null);
      }
    },
    [applyAccess, workflowId],
  );

  const rotate = useCallback(async () => {
    const confirmed = await confirmDialog({
      title: "Rotate the workflow API token?",
      description:
        "The current token will stop working immediately. Already admitted runs will continue.",
      confirmLabel: "Rotate token",
      destructive: true,
    });
    if (confirmed) await lifecycle("rotate");
  }, [lifecycle]);

  const disable = useCallback(async () => {
    const confirmed = await confirmDialog({
      title: "Disable workflow API access?",
      description:
        "New calls and token-authenticated polling will be blocked. Internal run history remains available.",
      confirmLabel: "Disable API",
      destructive: true,
    });
    if (!confirmed) return;
    setBusy("disable");
    setError(null);
    setNotice(null);
    try {
      const response = await fetch(`/api/workflows/${workflowId}/api-access`, {
        method: "DELETE",
      });
      const body = (await response.json().catch(() => ({}))) as {
        access?: WorkflowApiAccess;
        error?: unknown;
      };
      if (!response.ok || !body.access) {
        throw new Error(errorMessage(body, "Could not disable API access."));
      }
      applyAccess(body.access);
      setRevealedToken(null);
      setNotice("API access disabled.");
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Could not disable API access.",
      );
    } finally {
      setBusy(null);
    }
  }, [applyAccess, workflowId]);

  const saveLimits = useCallback(async () => {
    if (!draft) return;
    const limits = Object.fromEntries(
      Object.entries(draft).map(([key, value]) => [key, Number(value)]),
    );
    if (Object.values(limits).some((value) => !Number.isSafeInteger(value))) {
      setError("Every limit must be a whole number.");
      return;
    }
    setBusy("limits");
    setError(null);
    setNotice(null);
    try {
      const response = await fetch(`/api/workflows/${workflowId}/api-access`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ limits }),
      });
      const body = (await response.json().catch(() => ({}))) as {
        access?: WorkflowApiAccess;
        error?: unknown;
      };
      if (!response.ok || !body.access) {
        throw new Error(errorMessage(body, "Could not save API limits."));
      }
      applyAccess(body.access);
      setNotice("API limits saved.");
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Could not save API limits.",
      );
    } finally {
      setBusy(null);
    }
  }, [applyAccess, draft, workflowId]);

  const saveTelemetry = useCallback(async (includeTelemetry: boolean) => {
    setBusy("telemetry");
    setError(null);
    setNotice(null);
    try {
      const response = await fetch(`/api/workflows/${workflowId}/api-access`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ includeTelemetry }),
      });
      const body = (await response.json().catch(() => ({}))) as {
        access?: WorkflowApiAccess;
        error?: unknown;
      };
      if (!response.ok || !body.access) {
        throw new Error(errorMessage(body, "Could not save the telemetry setting."));
      }
      applyAccess(body.access);
    } catch (cause) {
      setError(
        cause instanceof Error ? cause.message : "Could not save the telemetry setting.",
      );
    } finally {
      setBusy(null);
    }
  }, [applyAccess, workflowId]);

  const copyText = useCallback(async (key: string, value: string) => {
    try {
      await writeClipboard(value);
      setCopied(key);
      window.setTimeout(() => setCopied((current) => (current === key ? null : current)), 1_600);
    } catch {
      setError("Clipboard access was blocked. Select and copy the value manually.");
    }
  }, []);

  if (loading) {
    return (
      <section className="workflow-detail-section" aria-busy="true">
        <h3>API access</h3>
        <div className="workflow-detail-section-body" role="status">
          <span className="workflow-loading-line is-wide" />
          <span className="workflow-loading-line" />
        </div>
      </section>
    );
  }

  if (forbidden) {
    return (
      <section className="workflow-detail-section">
        <h3>API access</h3>
        <p className="workflow-detail-section-body text-text3">
          An administrator can enable and manage external API access for this workflow.
        </p>
      </section>
    );
  }

  if (!access) {
    return (
      <section className="workflow-detail-section">
        <h3>API access</h3>
        <div className="workflow-detail-section-body grid gap-2" role="alert">
          <span className="text-danger">{error ?? "API access could not be loaded."}</span>
          <Button size="sm" className="justify-self-start" onClick={() => void load()}>
            Retry
          </Button>
        </div>
      </section>
    );
  }

  return (
    <section className="workflow-detail-section workflow-api-panel">
      <div className="workflow-api-heading">
        <h3>API access</h3>
        <Badge variant={access.enabled ? "live" : "muted"}>
          {access.enabled ? "Enabled" : "Disabled"}
        </Badge>
      </div>
      <div className="workflow-detail-section-body grid gap-3">
        <p className="text-text2 leading-[1.5]">
          Admit asynchronous runs with a workflow-scoped bearer token. API access is off by default and does not change the workflow&apos;s internal schedule.
        </p>

        <div className="workflow-api-facts">
          <div>
            <span>Endpoint</span>
            <code>{apiPath}</code>
          </div>
          <div>
            <span>Token</span>
            <code>{access.tokenPrefix ? `${access.tokenPrefix}_…` : "Not issued"}</code>
          </div>
          <div>
            <span>Last used</span>
            <strong>{formatDate(access.lastUsedAt)}</strong>
          </div>
          <div>
            <span>Batch mode</span>
            <strong>{access.batch.available ? "Workflow contract ready" : "Unavailable"}</strong>
          </div>
        </div>

        <ActionGroup align="start">
          {!access.enabled ? (
            <Button
              size="sm"
              variant="primary"
              disabled={busy !== null}
              onClick={() => void lifecycle("enable")}
            >
              <KeyRound aria-hidden="true" />
              {busy === "enable" ? "Enabling…" : "Enable API"}
            </Button>
          ) : (
            <>
              <Button size="sm" disabled={busy !== null} onClick={() => void rotate()}>
                <RotateCw aria-hidden="true" />
                {busy === "rotate" ? "Rotating…" : "Rotate token"}
              </Button>
              <Button
                size="sm"
                variant="danger"
                disabled={busy !== null}
                onClick={() => void disable()}
              >
                <ShieldOff aria-hidden="true" />
                {busy === "disable" ? "Disabling…" : "Disable API"}
              </Button>
            </>
          )}
        </ActionGroup>

        {revealedToken ? (
          <Card className="workflow-api-token" role="status">
            <div className="workflow-api-token-head">
              <div>
                <Badge variant="watch">Shown once</Badge>
                <strong>Save this token now</strong>
              </div>
              <Button
                size="sm"
                onClick={() => void copyText("token", revealedToken)}
              >
                {copied === "token" ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
                {copied === "token" ? "Copied" : "Copy token"}
              </Button>
            </div>
            <code className="workflow-api-token-value">{revealedToken}</code>
            <p>OpenNeko stores only a verifier. Closing this notice permanently hides the plaintext token.</p>
            <Button size="sm" variant="ghost" onClick={() => setRevealedToken(null)}>
              I saved it
            </Button>
          </Card>
        ) : null}

        {error ? <p className="workflow-api-message is-error" role="alert">{error}</p> : null}
        {notice ? <p className="workflow-api-message" role="status">{notice}</p> : null}

        <Checkbox
          label="Include run telemetry (timings, tokens, cost) in poll results"
          checked={access.includeTelemetry}
          disabled={busy !== null}
          onCheckedChange={(checked) => void saveTelemetry(checked === true)}
        />

        <div className="workflow-api-examples">
            <ApiExample
              title="Call this workflow"
              value={singleExample}
              copied={copied === "single"}
              onCopy={() => void copyText("single", singleExample)}
              meta={lastRequest ? "Body from the last call" : "Replace {} with the JSON this workflow reads"}
            />
            <p className="text-ui-caption leading-[1.5] text-text3">
              Send one Idempotency-Key per request and reuse it on retries, so a retry returns the first run. Retry on 429 and 5xx with the same key until the call returns 202.
            </p>
            {access.batch.available ? (
              <ApiExample
                title="Batch"
                value={batchExample}
                copied={copied === "batch"}
                onCopy={() => void copyText("batch", batchExample)}
                meta={`${access.batch.columns.length} output columns · ${access.batch.recordsField ?? "records"}`}
              />
            ) : (
              <p className="text-ui-caption leading-[1.5] text-text3">
                Batch mode stays unavailable until this workflow defines a validated record and CSV-column contract. Skills are optional and do not control batch readiness.
              </p>
            )}
        </div>

        <Disclosure title="Admission limits" meta="Fail closed">
          <form
            className="workflow-api-limit-form"
            onSubmit={(event) => {
              event.preventDefault();
              void saveLimits();
            }}
          >
            {LIMIT_GROUPS.map((group) => (
              <fieldset key={group}>
                <legend>{group}</legend>
                <div className="workflow-api-limit-grid">
                  {LIMIT_DEFINITIONS.filter((definition) => definition.group === group).map(
                    (definition) => {
                      const id = `workflow-api-${workflowId}-${definition.key}`;
                      return (
                        <Field
                          key={definition.key}
                          label={definition.label}
                          hint={`${definition.unit} · ${definition.min.toLocaleString("en-IN")}–${definition.max.toLocaleString("en-IN")}`}
                          htmlFor={id}
                        >
                          <Input
                            id={id}
                            type="number"
                            inputMode="numeric"
                            min={definition.min}
                            max={definition.max}
                            step={1}
                            required
                            disabled={busy !== null}
                            value={draft?.[definition.key] ?? ""}
                            onChange={(event) =>
                              setDraft((current) =>
                                current
                                  ? { ...current, [definition.key]: event.target.value }
                                  : current,
                              )
                            }
                          />
                        </Field>
                      );
                    },
                  )}
                </div>
              </fieldset>
            ))}
            <div className="workflow-api-limit-foot">
              <span>
                Limits apply at durable admission and again during worker execution.
              </span>
              <Button type="submit" size="sm" disabled={busy !== null}>
                <Save aria-hidden="true" />
                {busy === "limits" ? "Saving…" : "Save limits"}
              </Button>
            </div>
          </form>
        </Disclosure>

        {access.tokenCreatedAt ? (
          <p className="workflow-api-dates">
            Issued {formatDate(access.tokenCreatedAt)}
            {access.tokenRotatedAt ? ` · Rotated ${formatDate(access.tokenRotatedAt)}` : ""}
          </p>
        ) : null}
      </div>
    </section>
  );
}

function ApiExample({
  title,
  value,
  copied,
  onCopy,
  meta,
}: {
  title: string;
  value: string;
  copied: boolean;
  onCopy: () => void;
  meta?: string;
}) {
  return (
    <div className="workflow-api-example">
      <div>
        <strong>{title}</strong>
        {meta ? <span>{meta}</span> : null}
      </div>
      <div className="workflow-api-code">
        <pre><code>{value}</code></pre>
        <Button
          size="icon-sm"
          variant="secondary"
          className="workflow-api-copy"
          aria-label={copied ? "Copied" : `Copy ${title.toLowerCase()}`}
          title={copied ? "Copied" : "Copy"}
          onClick={onCopy}
        >
          {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
        </Button>
      </div>
    </div>
  );
}

import { cn } from "@/lib/cn";
import { describeActionChanges, diffWords, trimDiffContext, type ActionChange } from "@/lib/action-changes";

const MAX_CHANGES = 8;

function wordCount(value: string): number {
  return value.split(/\s+/).filter(Boolean).length;
}

function ChangeValue({ change }: { change: ActionChange }) {
  if (change.before === null) {
    return <span className="whitespace-pre-line text-text">{change.after}</span>;
  }
  if (wordCount(change.before) <= 3 && wordCount(change.after) <= 3) {
    return (
      <span className="inline-flex flex-wrap items-baseline gap-x-2 gap-y-1">
        <span className="text-text3 line-through decoration-text3/60">{change.before}</span>
        <span aria-hidden="true" className="text-text3">→</span>
        <span className="sr-only">changes to</span>
        <span className="font-semibold text-text">{change.after}</span>
      </span>
    );
  }
  return (
    <span className="text-text2">
      {trimDiffContext(diffWords(change.before, change.after)).map((token, index) =>
        token.kind === "same" ? (
          <span key={index}>{token.text}</span>
        ) : (
          <span key={index}>
            {token.text.match(/^\s*/)?.[0]}
            {token.kind === "removed" ? (
              <del className="rounded-[3px] bg-danger-soft px-0.5 text-danger decoration-danger/50">
                {token.text.trimStart()}
              </del>
            ) : (
              <ins className="rounded-[3px] bg-success-soft px-0.5 font-semibold text-success-ink no-underline">
                {token.text.trimStart()}
              </ins>
            )}
          </span>
        ),
      )}
    </span>
  );
}

export function ActionChanges({
  payload,
  limit = MAX_CHANGES,
  className,
}: {
  payload: unknown;
  limit?: number;
  className?: string;
}) {
  const sets = describeActionChanges(payload);
  if (!sets) return null;
  const multiple = sets.length > 1;
  return (
    <div className={cn("grid gap-3", className)} data-slot="action-changes">
      {sets.map((set, setIndex) => {
        const shown = set.changes.slice(0, limit);
        const hidden = set.changes.length - shown.length;
        return (
          <section key={`${set.entity ?? "row"}-${setIndex}`} aria-label={set.entity ? `Changes to ${set.entity}` : "Proposed changes"}>
            <div className="mb-2 flex items-baseline gap-2 text-ui-caption font-semibold text-text3">
              <span>
                {set.changes.length} {set.changes.length === 1 ? "change" : "changes"}
                {multiple && set.entity ? ` to ${set.entity}` : ""}
              </span>
            </div>
            <dl className="grid divide-y divide-border overflow-hidden rounded-inner border border-border bg-card">
              {shown.map((change) => (
                <div key={change.key} className="grid gap-1 px-3.5 py-3">
                  <dt className="text-ui-caption font-semibold text-text3">{change.label}</dt>
                  <dd className="m-0 min-w-0 break-words text-ui-body-sm leading-[1.6]">
                    <ChangeValue change={change} />
                  </dd>
                </div>
              ))}
            </dl>
            {hidden > 0 ? (
              <p className="mt-2 text-ui-caption text-text3">
                {hidden} more {hidden === 1 ? "change" : "changes"}.
              </p>
            ) : null}
          </section>
        );
      })}
    </div>
  );
}

export function hasActionChanges(payload: unknown): boolean {
  return describeActionChanges(payload) !== null;
}

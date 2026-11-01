/**
 * Sentinel that marks the machine-readable workflow-mention block appended to
 * a /work user message. The block maps each @mention the operator inserted to
 * its workflow id so the agent acts on the exact workflow — names can collide
 * or drift, ids don't. The transcript strips it for display; the agent reads
 * the raw content. Mirrors {@link BRIEFING_CARD_SENTINEL}, but as a suffix.
 */
export const WORKFLOW_MENTION_SENTINEL = "::neko-workflow-mentions::";
export const WORK_MENTION_SENTINEL = "::neko-work-mentions::";

export type WorkflowMention = { id: string; name: string };
export type WorkMentionKind = "skill" | "workflow";
export type WorkMention = {
  kind: WorkMentionKind;
  id: string;
  name: string;
  description: string;
};
export type WorkMentionRef = Pick<WorkMention, "kind" | "id" | "name">;
export type DraftWorkMention = WorkMentionRef & {
  start: number;
  end: number;
};

export type WorkMentionKeyAction =
  | { type: "move"; index: number }
  | { type: "select"; index: number }
  | { type: "close" };

/** Append typed mention metadata. Descriptions stay UI-only and are never
 * persisted in the transcript. */
export function appendWorkMentionBlock(
  body: string,
  mentions: WorkMentionRef[],
): string {
  if (mentions.length === 0) return body;
  const seen = new Set<string>();
  const payload = mentions.flatMap((mention) => {
    const key = `${mention.kind}:${mention.id}`;
    if (seen.has(key)) return [];
    seen.add(key);
    return [
      { kind: mention.kind, id: mention.id, name: mention.name },
    ];
  });
  return `${body}\n\n${WORK_MENTION_SENTINEL}${JSON.stringify(payload)}`;
}

/**
 * Append the mention block to a message body. Returns the body unchanged when
 * there are no mentions, so callers can append unconditionally.
 */
export function appendWorkflowMentionBlock(
  body: string,
  mentions: WorkflowMention[],
): string {
  if (mentions.length === 0) return body;
  const payload = mentions.map((m) => ({ id: m.id, name: m.name }));
  return `${body}\n\n${WORKFLOW_MENTION_SENTINEL}${JSON.stringify(payload)}`;
}

/**
 * Strip the trailing mention block from a message for display/editing. The
 * agent still sees it in the stored content; this is purely cosmetic.
 */
export function stripWorkflowMentionBlock(content: string): string {
  return stripWorkMentionBlock(content);
}

/** Strip either the current typed block or the legacy workflow-only block.
 * Only a valid trailing JSON array is treated as internal metadata, so a user
 * can mention the sentinel text itself without losing visible content. */
export function stripWorkMentionBlock(content: string): string {
  for (const sentinel of [WORK_MENTION_SENTINEL, WORKFLOW_MENTION_SENTINEL]) {
    const marker = `\n\n${sentinel}`;
    const at = content.lastIndexOf(marker);
    if (at === -1) continue;
    try {
      const parsed = JSON.parse(content.slice(at + marker.length));
      if (Array.isArray(parsed)) return content.slice(0, at).trimEnd();
    } catch {
      // This is user-authored text, not a host-appended metadata block.
    }
  }
  return content;
}

function normalized(value: string): string {
  return value.trim().toLocaleLowerCase();
}

/** Filter and rank the mixed catalog deterministically. */
export function filterWorkMentions(
  options: WorkMention[],
  query: string,
  limit = 8,
): WorkMention[] {
  const q = normalized(query);
  const score = (option: WorkMention): number | null => {
    if (!q) return 4;
    const name = normalized(option.name);
    const description = normalized(option.description);
    if (name === q) return 0;
    if (name.startsWith(q)) return 1;
    if (name.includes(q)) return 2;
    if (description.includes(q)) return 3;
    return null;
  };

  return options
    .map((option) => ({ option, score: score(option) }))
    .filter(
      (entry): entry is { option: WorkMention; score: number } =>
        entry.score !== null,
    )
    .sort(
      (a, b) =>
        a.score - b.score ||
        a.option.name.localeCompare(b.option.name) ||
        a.option.kind.localeCompare(b.option.kind) ||
        a.option.id.localeCompare(b.option.id),
    )
    .slice(0, Math.max(0, limit))
    .map((entry) => entry.option);
}

export function workMentionKeyAction(
  key: string,
  activeIndex: number,
  optionCount: number,
): WorkMentionKeyAction | null {
  if (key === "Escape") return { type: "close" };
  if (optionCount < 1) return null;
  if (key === "ArrowDown") {
    return { type: "move", index: (activeIndex + 1) % optionCount };
  }
  if (key === "ArrowUp") {
    return {
      type: "move",
      index: (activeIndex - 1 + optionCount) % optionCount,
    };
  }
  if (key === "Enter" || key === "Tab") {
    return { type: "select", index: activeIndex };
  }
  return null;
}

export type TextEdit = { start: number; oldEnd: number; newEnd: number };

/** Infer the one contiguous edit represented by a textarea change. */
export function inferTextEdit(before: string, after: string): TextEdit {
  let start = 0;
  const prefixMax = Math.min(before.length, after.length);
  while (start < prefixMax && before[start] === after[start]) start += 1;

  let oldEnd = before.length;
  let newEnd = after.length;
  while (
    oldEnd > start &&
    newEnd > start &&
    before[oldEnd - 1] === after[newEnd - 1]
  ) {
    oldEnd -= 1;
    newEnd -= 1;
  }
  return { start, oldEnd, newEnd };
}

/** Move unaffected typed selections with an edit and drop any selection whose
 * visible @token was changed. This preserves exact kind/id metadata when a
 * skill and workflow share the same display name. */
export function updateDraftWorkMentions(
  mentions: DraftWorkMention[],
  edit: TextEdit,
): DraftWorkMention[] {
  const delta = edit.newEnd - edit.oldEnd;
  return mentions.flatMap((mention) => {
    if (mention.end <= edit.start) return [mention];
    if (mention.start >= edit.oldEnd) {
      return [
        {
          ...mention,
          start: mention.start + delta,
          end: mention.end + delta,
        },
      ];
    }
    return [];
  });
}

export function activeWorkMentions(
  draft: string,
  mentions: DraftWorkMention[],
): WorkMentionRef[] {
  return mentions
    .filter(
      (mention) =>
        draft.slice(mention.start, mention.end) === `@${mention.name}`,
    )
    .sort((a, b) => a.start - b.start)
    .map(({ kind, id, name }) => ({ kind, id, name }));
}

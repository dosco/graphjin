/**
 * Markdown → Telegram-flavoured HTML.
 *
 * Telegram HTML recognizes only the &amp; &lt; &gt; &quot; entities and the
 * tags b/i/u/s/code/pre/a/blockquote/spoiler/tg-emoji — no headings, lists, or
 * tables. So this converts the inline/emphasis Markdown agents emit and
 * degrades the rest: headings → bold line, list items → bullet line, rules →
 * em dash. Code spans + links are stashed behind private-use sentinels BEFORE
 * escaping so their content is never re-parsed and a number in the prose (e.g.
 * "$4.7M") is never mistaken for a stash index.
 */

/** Escape only the three structural chars Telegram needs (NOT `'` → &apos;,
 *  which Telegram renders literally). */
export const escapeHtml = (s: string): string =>
  s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

const OPEN = String.fromCharCode(0xe000);
const CLOSE = String.fromCharCode(0xe001);
const STASH_RE = new RegExp(`${OPEN}(\\d+)${CLOSE}`, "g");

/** Inline spans for ONE line: stash code + links, escape, then emphasis. */
export const inlineMd = (text: string): string => {
  const stash: string[] = [];
  const keep = (fragment: string): string => {
    stash.push(fragment);
    return `${OPEN}${stash.length - 1}${CLOSE}`;
  };
  let s = text.replace(/`([^`]+)`/g, (_m, c: string) => keep(`<code>${escapeHtml(c)}</code>`));
  s = s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_m, label: string, url: string) =>
    keep(`<a href="${escapeHtml(url)}">${escapeHtml(label)}</a>`),
  );
  s = escapeHtml(s);
  // bold before italic; skip single `_` italic so snake_case identifiers survive.
  s = s.replace(/\*\*([^*\n]+)\*\*/g, "<b>$1</b>").replace(/__([^_\n]+)__/g, "<b>$1</b>");
  s = s.replace(/(^|[^*])\*([^*\n]+)\*/g, "$1<i>$2</i>");
  s = s.replace(/~~([^~\n]+)~~/g, "<s>$1</s>");
  return s.replace(STASH_RE, (_m, n: string) => stash[Number(n)] ?? "");
};

/** Full block + inline conversion. May emit top-level <pre>/<blockquote>. */
export const mdToTelegramHtml = (md: string): string => {
  const lines = md.replace(/\r\n/g, "\n").split("\n");
  const out: string[] = [];
  let quote: string[] | null = null;
  const flushQuote = () => {
    if (quote && quote.length) out.push(`<blockquote>${quote.join("\n")}</blockquote>`);
    quote = null;
  };
  let i = 0;
  while (i < lines.length) {
    const line = lines[i] ?? "";
    const fence = line.match(/^```(\w*)\s*$/);
    if (fence) {
      flushQuote();
      const code: string[] = [];
      i++;
      while (i < lines.length && !/^```\s*$/.test(lines[i] ?? "")) {
        code.push(lines[i] ?? "");
        i++;
      }
      i++; // skip closing fence
      const body = escapeHtml(code.join("\n"));
      out.push(fence[1] ? `<pre><code class="language-${fence[1]}">${body}</code></pre>` : `<pre>${body}</pre>`);
      continue;
    }
    const bq = line.match(/^>\s?(.*)$/);
    if (bq) {
      (quote ??= []).push(inlineMd(bq[1] ?? ""));
      i++;
      continue;
    }
    flushQuote();
    const heading = line.match(/^#{1,6}\s+(.*)$/);
    if (heading) {
      out.push(`<b>${inlineMd(heading[1] ?? "")}</b>`);
      i++;
      continue;
    }
    if (/^\s*([-*_])\1{2,}\s*$/.test(line)) {
      out.push("—");
      i++;
      continue;
    }
    const li = line.match(/^\s*(?:[-*+]|\d+[.)])\s+(.*)$/);
    if (li) {
      out.push(`• ${inlineMd(li[1] ?? "")}`);
      i++;
      continue;
    }
    out.push(line.trim() === "" ? "" : inlineMd(line));
    i++;
  }
  flushQuote();
  return out.join("\n").replace(/\n{3,}/g, "\n\n").trim();
};

/**
 * Inline-only conversion: never emits a block tag (<pre>/<blockquote>). Used
 * where Telegram forbids nesting — e.g. inside a <blockquote> callout. Headings
 * and list items still degrade to bold / bullet lines; code fences flatten to
 * inline lines.
 */
export const inlineBlock = (md: string): string =>
  md
    .replace(/\r\n/g, "\n")
    .split("\n")
    .filter((l) => !/^```/.test(l))
    .map((l) => {
      const heading = l.match(/^#{1,6}\s+(.*)$/);
      if (heading) return `<b>${inlineMd(heading[1] ?? "")}</b>`;
      const li = l.match(/^\s*(?:[-*+]|\d+[.)])\s+(.*)$/);
      if (li) return `• ${inlineMd(li[1] ?? "")}`;
      const bq = l.match(/^>\s?(.*)$/);
      if (bq) return inlineMd(bq[1] ?? "");
      return inlineMd(l);
    })
    .join("\n")
    .replace(/\n{3,}/g, "\n\n")
    .trim();

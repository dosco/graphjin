export type FindingSummary = {
  excerpt: string;
  truncated: boolean;
};

const SUMMARY_HEADING = /^(executive summary|summary|overview|tl;?dr|key findings?|what changed)\b/i;
const MAX_ITEMS = 3;
const MAX_ITEM_CHARS = 240;

type Block = { heading: string | null; lines: string[] };

function blocksOf(markdown: string): Block[] {
  const blocks: Block[] = [];
  let heading: string | null = null;
  let current: string[] = [];
  const flush = () => {
    if (current.length > 0) blocks.push({ heading, lines: current });
    current = [];
  };
  for (const raw of markdown.replace(/\r\n?/g, "\n").split("\n")) {
    const line = raw.trimEnd();
    const h = line.match(/^#{1,6}\s+(.*)$/);
    if (h) {
      flush();
      heading = h[1].replace(/[*_`]/g, "").trim();
      continue;
    }
    if (/^\s*(---+|\*\*\*+|___+)\s*$/.test(line) || line.trim() === "") {
      flush();
      continue;
    }
    current.push(line);
  }
  flush();
  return blocks;
}

function shorten(item: string): string {
  if (item.length <= MAX_ITEM_CHARS) return item;
  const plain = item.replace(/\*\*|__|`/g, "");
  const cut = plain.slice(0, MAX_ITEM_CHARS);
  const sentence = cut.lastIndexOf(". ");
  const end = sentence > MAX_ITEM_CHARS * 0.5 ? sentence + 1 : cut.lastIndexOf(" ");
  return `${plain.slice(0, end > 0 ? end : MAX_ITEM_CHARS).trimEnd()}…`;
}

// Picks the part of an agent report that a reader needs on a card: the
// summary section when the report has one, else the first paragraph or list.
export function summarizeFinding(markdown: string): FindingSummary {
  const blocks = blocksOf(markdown);
  if (blocks.length === 0) return { excerpt: "", truncated: false };
  const block = blocks.find((b) => b.heading && SUMMARY_HEADING.test(b.heading)) ?? blocks[0];
  const isList = block.lines.every((line) => /^\s*([-*+]|\d+[.)])\s+/.test(line) || /^\s{2,}\S/.test(line));
  let excerpt: string;
  let cut = false;
  if (isList) {
    const items = block.lines.filter((line) => /^([-*+]|\d+[.)])\s+/.test(line));
    cut = items.length > MAX_ITEMS || items.length < block.lines.length;
    excerpt = items
      .slice(0, MAX_ITEMS)
      .map((line) => {
        const text = line.replace(/^([-*+]|\d+[.)])\s+/, "");
        const short = shorten(text);
        if (short !== text) cut = true;
        return `- ${short}`;
      })
      .join("\n");
  } else {
    const paragraph = block.lines.join(" ");
    excerpt = shorten(paragraph);
    cut = excerpt !== paragraph;
  }
  return { excerpt, truncated: cut || blocks.length > 1 };
}

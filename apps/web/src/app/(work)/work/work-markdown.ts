import remarkGfm from "remark-gfm";

// Matches paths to agent-generated/uploaded files inside the per-org workspace.
// Two shapes:
//   - absolute container paths, e.g.
//       ~/.config/openneko/agents/orgs/<orgId>/uploads/<threadId>/file.csv
//   - bare workspace-relative paths, e.g. `uploads/<threadId>/file.csv` — the
//     same form `joinMessageWithAttachments` emits into the user's outgoing
//     message and that the agent passes back when it cites files.
// Capture group 1 is the workspace-relative path the /api/work/files route serves.
const WORKSPACE_FILE_RE =
  /(?:\/agents\/orgs\/[A-Za-z0-9-]+\/|(?<![A-Za-z0-9._\-/]))((?:runs|uploads|skills|memory)\/[A-Za-z0-9._\-/]+\.[A-Za-z0-9]+)/g;

type MdNode = { type: string; value?: string; url?: string; children?: MdNode[] };

function autolinkWorkspaceFiles() {
  return (tree: unknown) => walkMdNode(tree, null);
}

function walkMdNode(node: unknown, parent: MdNode | null): void {
  if (!node || typeof node !== "object") return;
  const n = node as MdNode;
  if (n.type === "code" || n.type === "link") return;
  // inlineCode is the `path` case — assistant often wraps file paths in
  // backticks. Convert the whole inline-code node to a link if its value
  // matches a workspace file path.
  if (n.type === "inlineCode" && typeof n.value === "string" && parent?.children) {
    WORKSPACE_FILE_RE.lastIndex = 0;
    const match = WORKSPACE_FILE_RE.exec(n.value);
    if (match) {
      const idx = parent.children.indexOf(n);
      if (idx !== -1) {
        parent.children.splice(idx, 1, {
          type: "link",
          url: `/api/work/files/${match[1]}`,
          children: [{ type: "inlineCode", value: n.value.split("/").slice(-1)[0] }],
        });
      }
    }
    return;
  }
  if (n.type === "text" && typeof n.value === "string" && parent?.children) {
    const value = n.value;
    WORKSPACE_FILE_RE.lastIndex = 0;
    const matches = [...value.matchAll(WORKSPACE_FILE_RE)];
    if (matches.length === 0) return;
    const replacement: MdNode[] = [];
    let cursor = 0;
    for (const m of matches) {
      const start = m.index ?? 0;
      const end = start + m[0].length;
      if (start > cursor) {
        replacement.push({ type: "text", value: value.slice(cursor, start) });
      }
      replacement.push({
        type: "link",
        url: `/api/work/files/${m[1]}`,
        children: [{ type: "text", value: m[0].split("/").slice(-1)[0] }],
      });
      cursor = end;
    }
    if (cursor < value.length) {
      replacement.push({ type: "text", value: value.slice(cursor) });
    }
    const idx = parent.children.indexOf(n);
    if (idx !== -1) parent.children.splice(idx, 1, ...replacement);
    return;
  }
  if (Array.isArray(n.children)) {
    for (const child of n.children.slice()) walkMdNode(child, n);
  }
}

export const REMARK_PLUGINS = [remarkGfm, autolinkWorkspaceFiles];

"use client";

import type { AnchorHTMLAttributes, ReactNode } from "react";

// Shared markdown preprocessor that rewrites agent-mentioned absolute workspace
// paths into clickable links to /api/work/files. Applied wherever assistant
// prose is rendered — RunTimeline's ReactMarkdown invocations and the A2UI
// Markdown component used in synthesized surfaces.

const WORKSPACE_FILE_RE =
  /\/agents\/orgs\/[A-Za-z0-9-]+\/((?:runs|uploads|skills|memory)\/[A-Za-z0-9._\-/]+\.[A-Za-z0-9]+)/;

const INLINE_FILE_RE =
  /`([^`\n]*\/agents\/orgs\/[A-Za-z0-9-]+\/(?:runs|uploads|skills|memory)\/[A-Za-z0-9._\-/]+\.[A-Za-z0-9]+)`/g;

const BARE_FILE_RE =
  /(?<!\]\()(\/[A-Za-z0-9._\-/]*\/agents\/orgs\/[A-Za-z0-9-]+\/(?:runs|uploads|skills|memory)\/[A-Za-z0-9._\-/]+\.[A-Za-z0-9]+)(?!\))/g;

function relPathFor(absolute: string): string | null {
  const m = WORKSPACE_FILE_RE.exec(absolute);
  return m ? m[1] : null;
}

export function openFileLink(href: string): void {
  if (typeof window === "undefined") return;
  const w = window.open(href, "_blank", "noopener,noreferrer");
  if (w) return;
  const a = document.createElement("a");
  a.href = href;
  a.download = "";
  a.rel = "noopener noreferrer";
  a.style.display = "none";
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
}

// ReactMarkdown components map. Routes /api/work/files anchors through
// openFileLink (new tab + popup fallback) and leaves other anchors alone.
// Used by both the work RunTimeline and the a2ui Markdown component.
export const WORKSPACE_MARKDOWN_COMPONENTS = {
  a({
    href,
    children,
    onClick,
    ...props
  }: AnchorHTMLAttributes<HTMLAnchorElement> & { children?: ReactNode }) {
    const isFile = typeof href === "string" && href.startsWith("/api/work/files/");
    if (isFile) {
      return (
        <a
          href={href}
          target="_blank"
          rel="noopener noreferrer"
          onClick={(e) => {
            onClick?.(e);
            if (e.defaultPrevented) return;
            if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
            e.preventDefault();
            if (typeof href === "string") openFileLink(href);
          }}
          {...props}
        >
          {children}
        </a>
      );
    }
    return <a href={href} onClick={onClick} {...props}>{children}</a>;
  },
};

export function linkifyWorkspacePaths(md: string): string {
  if (!md) return md;
  return md
    .replace(INLINE_FILE_RE, (_full, path: string) => {
      const rel = relPathFor(path);
      if (!rel) return `\`${path}\``;
      const filename = path.split("/").slice(-1)[0];
      return `[\`${filename}\`](/api/work/files/${rel})`;
    })
    .replace(BARE_FILE_RE, (full: string, path: string) => {
      const rel = relPathFor(path);
      if (!rel) return full;
      const filename = path.split("/").slice(-1)[0];
      return `[${filename}](/api/work/files/${rel})`;
    });
}

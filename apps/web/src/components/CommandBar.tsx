"use client";

import { useCallback, useEffect, useMemo, useRef, useState, type ComponentType } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Dialog } from "radix-ui";
import { ArrowRight, CornerDownLeft, MessageCircle, MonitorPlay, Plus, Search, Settings, User } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ALL_NAV, hideAppChrome } from "@/lib/nav";
import { cn } from "@/lib/cn";

export const COMMAND_BAR_EVENT = "openneko:command-bar";

export function openCommandBar() {
  window.dispatchEvent(new Event(COMMAND_BAR_EVENT));
}

type Command = {
  id: string;
  label: string;
  hint?: string;
  group: "Ask" | "Go to" | "Workflows" | "Settings";
  icon: ComponentType<{ className?: string; "aria-hidden"?: boolean; strokeWidth?: number }>;
  keywords?: string;
  run: () => void;
};

type WorkflowSummary = { id: string; name: string; description: string | null };

const SETTINGS: Array<{ href: string; label: string }> = [
  { href: "/admin/settings/agent", label: "Agent settings" },
  { href: "/admin/settings/data", label: "Data source" },
  { href: "/admin/settings/spend", label: "Spending limits" },
  { href: "/admin/settings/security", label: "Security" },
  { href: "/admin/settings/packs", label: "Solution packs" },
  { href: "/admin/users", label: "Users" },
  { href: "/admin/settings/channels", label: "Channels" },
  { href: "/admin/rules", label: "Rules" },
];

function matches(command: Command, query: string): boolean {
  if (!query) return true;
  const haystack = `${command.label} ${command.hint ?? ""} ${command.keywords ?? ""}`.toLowerCase();
  return query
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((word) => haystack.includes(word));
}

export default function CommandBar() {
  const router = useRouter();
  const pathname = usePathname() ?? "/";
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const [workflows, setWorkflows] = useState<WorkflowSummary[]>([]);
  const listRef = useRef<HTMLDivElement>(null);
  const hidden = hideAppChrome(pathname);

  useEffect(() => {
    if (hidden) return;
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setOpen((current) => !current);
      }
    };
    const onOpen = () => setOpen(true);
    window.addEventListener("keydown", onKey);
    window.addEventListener(COMMAND_BAR_EVENT, onOpen);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener(COMMAND_BAR_EVENT, onOpen);
    };
  }, [hidden]);

  useEffect(() => {
    if (!open || workflows.length > 0) return;
    let cancelled = false;
    void fetch("/api/workflows", { cache: "no-store" })
      .then((res) => (res.ok ? res.json() : { workflows: [] }))
      .then((data: { workflows?: WorkflowSummary[] }) => {
        if (!cancelled) setWorkflows(data.workflows ?? []);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [open, workflows.length]);

  const go = useCallback(
    (href: string) => {
      setOpen(false);
      router.push(href);
    },
    [router],
  );

  const commands = useMemo<Command[]>(() => {
    const trimmed = query.trim();
    const ask: Command[] = trimmed
      ? [
          {
            id: "ask",
            label: `Ask OpenNeko: “${trimmed}”`,
            group: "Ask",
            icon: MessageCircle,
            run: () => go(`/work?seed=${encodeURIComponent(trimmed)}`),
          },
        ]
      : [];
    const nav: Command[] = [
      ...ALL_NAV.map((item) => ({
        id: `nav:${item.href}`,
        label: item.label,
        hint: item.description,
        group: "Go to" as const,
        icon: item.icon,
        run: () => go(item.href),
      })),
      { id: "nav:/apps", label: "Apps", group: "Go to", icon: ArrowRight, run: () => go("/apps") },
      { id: "nav:/profile", label: "Your account", hint: "Profile and display", group: "Go to", icon: User, run: () => go("/profile") },
    ];
    const flows: Command[] = [
      {
        id: "workflow:new",
        label: "New workflow",
        group: "Workflows",
        icon: Plus,
        run: () => go(`/work?seed=${encodeURIComponent("Set up a new workflow that ")}`),
      },
      ...workflows.map((w) => ({
        id: `workflow:${w.id}`,
        label: w.name,
        hint: w.description ?? undefined,
        group: "Workflows" as const,
        icon: MonitorPlay,
        run: () => go(`/workflows?id=${w.id}`),
      })),
    ];
    const settings: Command[] = SETTINGS.map((item) => ({
      id: `settings:${item.href}`,
      label: item.label,
      group: "Settings",
      icon: Settings,
      keywords: "settings admin",
      run: () => go(item.href),
    }));
    return [...ask, ...[...nav, ...flows, ...settings].filter((c) => matches(c, trimmed))].slice(0, 40);
  }, [go, query, workflows]);

  const activeIndex = Math.min(active, Math.max(commands.length - 1, 0));

  useEffect(() => {
    listRef.current
      ?.querySelector<HTMLElement>(`[data-index="${activeIndex}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [activeIndex]);

  if (hidden) return null;

  let lastGroup: Command["group"] | null = null;

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) {
          setQuery("");
          setActive(0);
        }
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay data-slot="command-overlay" className="fixed inset-0 bg-[var(--backdrop)] data-open:animate-in data-open:fade-in-0" />
        <Dialog.Content
          data-slot="command-content"
          aria-describedby={undefined}
          className="fixed left-1/2 top-[14vh] w-[min(640px,calc(100vw-32px))] -translate-x-1/2 overflow-hidden rounded-[18px] border border-border bg-card shadow-lift data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95"
          onKeyDown={(event) => {
            if (event.key === "ArrowDown") {
              event.preventDefault();
              setActive((i) => Math.min(i + 1, commands.length - 1));
            } else if (event.key === "ArrowUp") {
              event.preventDefault();
              setActive((i) => Math.max(i - 1, 0));
            } else if (event.key === "Enter") {
              event.preventDefault();
              commands[activeIndex]?.run();
            }
          }}
        >
          <Dialog.Title className="sr-only">Search OpenNeko</Dialog.Title>
          <div className="flex items-center gap-3 border-b border-border px-4">
            <Search className="size-4 shrink-0 text-text3" aria-hidden={true} />
            <Input
              autoFocus
              value={query}
              onChange={(event) => {
                setQuery(event.target.value);
                setActive(0);
              }}
              placeholder="Search pages and workflows, or ask a question"
              aria-label="Search OpenNeko"
              aria-controls="command-list"
              aria-activedescendant={commands[activeIndex] ? `command-${activeIndex}` : undefined}
              className="h-14 border-0 bg-transparent px-0 text-ui-body-lg shadow-none hover:border-transparent focus-visible:border-transparent focus-visible:shadow-none"
            />
          </div>
          <div ref={listRef} id="command-list" role="listbox" aria-label="Results" className="max-h-[min(420px,60vh)] overflow-y-auto p-2">
            {commands.length === 0 ? (
              <p className="px-3 py-6 text-center text-ui-body-sm text-text3">Nothing matches. Type a question to ask OpenNeko.</p>
            ) : (
              commands.map((command, index) => {
                const heading = command.group !== lastGroup ? command.group : null;
                lastGroup = command.group;
                const Icon = command.icon;
                const selected = index === activeIndex;
                return (
                  <div key={command.id}>
                    {heading ? (
                      <div className="px-3 pb-1 pt-3 text-ui-caption font-semibold text-text3 first:pt-1">{heading}</div>
                    ) : null}
                    <Button
                      id={`command-${index}`}
                      data-index={index}
                      role="option"
                      aria-selected={selected}
                      variant="ghost"
                      className={cn(
                        "h-auto min-h-10 w-full justify-start gap-3 rounded-[10px] px-3 py-2 text-left font-normal",
                        selected && "bg-neutral-soft",
                      )}
                      onMouseMove={() => setActive(index)}
                      onClick={() => command.run()}
                    >
                      <Icon className="size-4 shrink-0 text-text2" aria-hidden={true} strokeWidth={2} />
                      <span className="min-w-0 flex-1 truncate text-ui-body text-text">{command.label}</span>
                      {command.hint ? (
                        <span className="hidden min-w-0 max-w-[45%] truncate text-ui-caption text-text3 sm:block">{command.hint}</span>
                      ) : null}
                      {selected ? <CornerDownLeft className="size-3.5 shrink-0 text-text3" aria-hidden={true} /> : null}
                    </Button>
                  </div>
                );
              })
            )}
          </div>
          <div className="flex items-center gap-4 border-t border-border px-4 py-2 text-ui-caption text-text3">
            <span>↑↓ to move</span>
            <span>Enter to open</span>
            <span className="ml-auto">Esc to close</span>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

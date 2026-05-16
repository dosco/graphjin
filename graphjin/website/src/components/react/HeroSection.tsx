import { useState, useCallback } from "react";
import { Copy, Check, ArrowRight, Shield } from "lucide-react";
import GraphField from "./GraphField";

const INSTALL_COMMANDS = [
  { id: "npx", label: "npx", command: "npx graphjin serve" },
  { id: "brew", label: "brew", command: "brew install dosco/graphjin/graphjin" },
  { id: "curl", label: "curl", command: "curl -fsSL https://graphjin.com/install.sh | bash" },
] as const;

type CommandId = (typeof INSTALL_COMMANDS)[number]["id"];

const PILLS = [
  "Databases",
  "APIs",
  "Source code",
  "Files",
  "Workflows",
  "Security posture",
  "MCP + GraphQL",
];

export default function HeroSection() {
  const [activeCmd, setActiveCmd] = useState<CommandId>("npx");
  const [copied, setCopied] = useState(false);

  const activeCommand = INSTALL_COMMANDS.find((c) => c.id === activeCmd)!;

  const handleCopy = useCallback(async () => {
    await navigator.clipboard.writeText(activeCommand.command);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }, [activeCommand]);

  return (
    <section className="relative pt-32 pb-20 md:pt-40 md:pb-28 overflow-hidden">
      {/* gradient halo */}
      <div
        className="absolute inset-0 -z-20 pointer-events-none"
        style={{
          background:
            "radial-gradient(60% 50% at 50% 0%, color-mix(in srgb, var(--color-accent) 14%, transparent) 0%, transparent 70%)",
        }}
      />

      {/* animated graph backdrop — theme-aware via CSS variables */}
      <div
        className="absolute inset-0 -z-10 pointer-events-none"
        style={{
          maskImage:
            "radial-gradient(ellipse 85% 70% at 50% 35%, black 30%, transparent 85%)",
          WebkitMaskImage:
            "radial-gradient(ellipse 85% 70% at 50% 35%, black 30%, transparent 85%)",
        }}
      >
        <GraphField />
      </div>

      <div className="container-doc max-w-5xl">
        <div className="flex flex-col items-center text-center">
          <span className="eyebrow" style={{ marginBottom: "1rem" }}>
            v3 · Apache 2.0
          </span>

          <h1
            className="font-display font-bold tracking-tight text-balance"
            style={{
              color: "var(--color-text)",
              fontSize: "clamp(2.4rem, 5vw, 4.5rem)",
              lineHeight: 1.05,
              maxWidth: "20ch",
            }}
          >
            The compiler that connects AI to your organization.
          </h1>

          <p
            className="mt-5 text-lg md:text-xl leading-relaxed max-w-2xl"
            style={{ color: "var(--color-muted)" }}
          >
            Auto-learns your schema and gives AI agents one governed query
            graph across your databases, APIs, source code, filesystems, and
            workflows. Ask about the data, the services that enrich it, the code
            that touches it, and the files around it from one MCP-ready binary.
          </p>

          <p
            className="mt-4 max-w-3xl rounded-xl border px-4 py-3 text-sm md:text-base leading-relaxed"
            style={{
              color: "var(--color-muted)",
              background: "color-mix(in srgb, var(--color-surface) 78%, transparent)",
              borderColor: "color-mix(in srgb, var(--color-accent) 28%, var(--color-border))",
            }}
          >
            <strong style={{ color: "var(--color-text)" }}>One config</strong>{" "}
            defines what agents can discover, query, execute, edit, and never
            touch. Humans can audit it. Models can inspect it. GraphJin enforces
            it across your entire AI surface.
          </p>

          <div
            className="mt-7 flex flex-wrap items-center justify-center gap-x-5 gap-y-2 text-[13px] font-mono"
            style={{ color: "var(--color-muted)" }}
          >
            {PILLS.map((p) => (
              <span key={p} className="inline-flex items-center gap-1.5">
                <span
                  className="w-1.5 h-1.5 rounded-full"
                  style={{ background: "var(--color-accent)" }}
                />
                {p}
              </span>
            ))}
          </div>

          {/* install bar */}
          <div
            className="mt-10 flex w-full max-w-xl flex-col overflow-hidden rounded-xl border sm:inline-flex sm:w-auto sm:max-w-none sm:flex-row sm:items-center"
            style={{
              background: "var(--color-surface)",
              borderColor: "var(--color-border)",
            }}
          >
            <div
              className="flex items-center justify-center border-b sm:border-b-0 sm:border-r"
              style={{ borderColor: "var(--color-border)" }}
            >
              {INSTALL_COMMANDS.map((cmd) => (
                <button
                  key={cmd.id}
                  type="button"
                  onClick={() => setActiveCmd(cmd.id)}
                  className="px-3 py-2.5 font-mono text-xs transition-colors"
                  style={{
                    color:
                      activeCmd === cmd.id
                        ? "var(--color-accent)"
                        : "var(--color-muted)",
                    background:
                      activeCmd === cmd.id ? "var(--color-accent-soft)" : "transparent",
                  }}
                >
                  {cmd.label}
                </button>
              ))}
            </div>
            <code
              className="px-4 py-2.5 text-sm font-mono break-all sm:whitespace-nowrap"
              style={{ color: "var(--color-text)" }}
            >
              {activeCommand.command}
            </code>
            <button
              type="button"
              onClick={handleCopy}
              className="px-3 py-2.5 transition-colors border-t sm:border-l sm:border-t-0"
              style={{
                borderColor: "var(--color-border)",
                color: "var(--color-muted)",
              }}
              aria-label="Copy command"
            >
              {copied ? (
                <Check className="w-4 h-4" style={{ color: "var(--color-accent)" }} />
              ) : (
                <Copy className="w-4 h-4" />
              )}
            </button>
          </div>

          {/* CTAs */}
          <div className="mt-7 flex flex-wrap items-center justify-center gap-3">
            <a href="#quickstart" className="btn-primary">
              Get started
              <ArrowRight className="w-4 h-4" />
            </a>
            <a
              href="#security-model"
              className="btn-secondary"
            >
              <Shield className="w-4 h-4" />
              Security model
            </a>
          </div>
        </div>
      </div>
    </section>
  );
}

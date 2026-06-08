import { useState } from "react";
import { Copy, Check, Terminal } from "lucide-react";
import { motion } from "framer-motion";

const tabs = [
  { id: "npm", label: "npx", command: "npx graphjin serve --demo" },
  { id: "brew", label: "macOS", command: "brew install dosco/graphjin/graphjin" },
  { id: "scoop", label: "Windows", command: "scoop install graphjin" },
  { id: "curl", label: "curl", command: "curl -fsSL https://graphjin.com/install.sh | bash" },
];

const mcpClients = [
  {
    id: "claude-code",
    name: "Claude Code",
    logo: "/logos/claude-code.svg",
    command: "graphjin mcp install --client claude --scope global --yes",
  },
  {
    id: "openai-codex",
    name: "OpenAI Codex",
    logo: "/logos/openai-codex.svg",
    command: "graphjin mcp install --client codex --scope global --yes",
  },
];

export default function QuickStart() {
  const [activeTab, setActiveTab] = useState("npm");
  const [copied, setCopied] = useState(false);
  const [copiedMCP, setCopiedMCP] = useState<string | null>(null);

  const active = tabs.find((t) => t.id === activeTab) || tabs[0];

  const handleCopy = (text: string, setter: (v: any) => void, value: any) => {
    navigator.clipboard.writeText(text);
    setter(value);
    setTimeout(() => setter(null), 1800);
  };

  return (
    <section
      id="quickstart"
      className="py-20 md:py-28 border-t"
      style={{ borderColor: "var(--color-border)" }}
    >
      <div className="container-doc max-w-4xl">
        <header className="mb-10">
          <span className="eyebrow">QUICKSTART</span>
          <h2
            className="mt-4 text-3xl md:text-5xl font-display font-bold tracking-tight"
            style={{ color: "var(--color-text)", lineHeight: 1.1 }}
          >
            Run it in under a minute.
          </h2>
          <p className="mt-4 text-lg leading-relaxed" style={{ color: "var(--color-muted)" }}>
            Pick your platform, copy the command, and you're querying. The demo flag
            ships a real schema and example queries so there's something to point
            an AI client at on the very first run.
          </p>
        </header>

        {/* terminal-like card */}
        <div
          className="rounded-2xl border overflow-hidden"
          style={{
            background: "var(--color-code-bg)",
            borderColor: "var(--color-border)",
          }}
        >
          <div
            className="flex items-center justify-between px-4 py-2.5 border-b"
            style={{ borderColor: "rgba(255,255,255,0.08)" }}
          >
            <div className="flex items-center gap-2 flex-wrap">
              <Terminal className="w-3.5 h-3.5" style={{ color: "rgba(255,255,255,0.5)" }} />
              <div className="flex items-center gap-1">
                {tabs.map((tab) => (
                  <button
                    key={tab.id}
                    type="button"
                    onClick={() => setActiveTab(tab.id)}
                    className="px-3 py-1 text-xs font-mono rounded-md transition-colors"
                    style={{
                      color:
                        activeTab === tab.id
                          ? "var(--color-accent-readable)"
                          : "rgba(255,255,255,0.5)",
                      background:
                        activeTab === tab.id
                          ? "var(--color-accent-soft)"
                          : "transparent",
                    }}
                  >
                    {tab.label}
                  </button>
                ))}
              </div>
            </div>
            <button
              type="button"
              onClick={() => handleCopy(active.command, setCopied as any, true)}
              className="p-1.5 rounded-md transition-colors"
              style={{ color: "rgba(255,255,255,0.6)" }}
              aria-label="Copy install command"
            >
              {copied ? <Check className="w-4 h-4 text-emerald-400" /> : <Copy className="w-4 h-4" />}
            </button>
          </div>
          <div className="px-6 py-7 font-mono">
            <motion.div
              key={activeTab}
              initial={{ opacity: 0, y: 6 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.18 }}
              className="flex items-center gap-3 text-base md:text-lg"
              style={{ color: "var(--color-code-text)" }}
            >
              <span style={{ color: "rgba(255,255,255,0.4)" }}>$</span>
              <code className="break-all">{active.command}</code>
            </motion.div>
          </div>
        </div>

        {/* MCP install */}
        <div
          className="mt-6 rounded-2xl border p-5 md:p-6"
          style={{
            background: "var(--color-surface)",
            borderColor: "var(--color-border)",
          }}
        >
          <div className="flex items-center justify-between gap-4">
            <h3
              className="text-base md:text-lg font-display font-semibold"
              style={{ color: "var(--color-text)" }}
            >
              Wire it into your AI client
            </h3>
            <span className="text-xs font-mono" style={{ color: "var(--color-muted)" }}>
              one command
            </span>
          </div>

          <div className="mt-5 grid grid-cols-1 md:grid-cols-2 gap-3">
            {mcpClients.map((client) => (
              <div
                key={client.id}
                className="rounded-xl border p-4"
                style={{
                  background: "var(--color-bg)",
                  borderColor: "var(--color-border)",
                }}
              >
                <div className="flex items-center justify-between gap-3">
                  <img
                    src={client.logo}
                    alt={`${client.name} logo`}
                    className="h-7 md:h-8 object-contain object-left"
                    loading="lazy"
                  />
                  <button
                    type="button"
                    onClick={() => handleCopy(client.command, setCopiedMCP, client.id)}
                    className="p-1.5 rounded-md transition-colors"
                    style={{ color: "var(--color-muted)" }}
                    aria-label={`Copy command for ${client.name}`}
                  >
                    {copiedMCP === client.id ? (
                      <Check className="w-4 h-4" style={{ color: "var(--color-accent-readable)" }} />
                    ) : (
                      <Copy className="w-4 h-4" />
                    )}
                  </button>
                </div>
                <code
                  className="mt-3 block text-sm font-mono break-all leading-relaxed"
                  style={{ color: "var(--color-text)" }}
                >
                  {client.command}
                </code>
              </div>
            ))}
          </div>

          <p className="mt-4 text-xs" style={{ color: "var(--color-muted)" }}>
            Prefer interactive setup?{" "}
            <code className="font-mono" style={{ color: "var(--color-text)" }}>
              graphjin mcp install
            </code>
          </p>
        </div>
      </div>
    </section>
  );
}

import { useState, useCallback } from "react";
import { motion } from "framer-motion";
import { Copy, Check, ArrowRight } from "lucide-react";
import GraphField from "./GraphField";

const EASE = [0.25, 0.46, 0.45, 0.94] as const;

const fadeUp = (delay: number) => ({
  initial: { opacity: 0, y: 20 },
  animate: { opacity: 1, y: 0 },
  transition: { duration: 0.6, delay, ease: EASE },
});

const INSTALL_COMMANDS = [
  { id: "npx", label: "npx", command: "npx graphjin serve" },
  {
    id: "brew",
    label: "brew",
    command: "brew install dosco/graphjin/graphjin",
  },
  {
    id: "curl",
    label: "curl",
    command: "curl -fsSL https://graphjin.com/install.sh | bash",
  },
] as const;

type CommandId = (typeof INSTALL_COMMANDS)[number]["id"];

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
    <section className="relative min-h-screen flex items-center justify-center overflow-hidden">
      {/* Background layers */}
      <div className="absolute inset-0">
        {/* Background image — full visibility with warm orange glow */}
        <div
          className="absolute inset-0 bg-cover bg-no-repeat z-[1]"
          style={{
            backgroundImage: "url('/assets/bg2.webp')",
            backgroundPosition: "center 80%",
          }}
        />

        {/* Warm glow overlay — enhances sky tones without darkening */}
        <div className="absolute inset-0 bg-gradient-to-b from-orange-500/80 via-transparent via-[30%] to-transparent z-[2]" />

        {/* Bottom fade to black — text readability */}
        <div className="absolute inset-x-0 bottom-0 h-[55%] bg-gradient-to-t from-black from-[40%] via-black/60 to-transparent z-[2]" />
      </div>

      {/* Particle background — above the image layers */}
      <div className="absolute inset-0 z-[3]">
        <GraphField />
      </div>

      {/* Content — vertically centered */}
      <div className="relative z-10 max-w-5xl mx-auto px-6 pt-32 pb-16 text-center">
        {/* Eyebrow */}
        <motion.span
          {...fadeUp(0)}
          className="inline-block text-sm font-mono tracking-[0.2em] text-white/40 uppercase mb-3"
        >
          GraphJin
        </motion.span>

        {/* Headline */}
        <motion.h1
          {...fadeUp(0.1)}
          className="font-display font-bold tracking-tighter leading-[1.05] mb-5 whitespace-nowrap text-[clamp(2rem,4.5vw,4.5rem)]"
          style={{ filter: "drop-shadow(0 2px 20px rgba(0,0,0,0.8))" }}
        >
          <span className="bg-clip-text text-transparent bg-gradient-to-r from-orange-400 via-amber-200 to-white">
            Connect Your Databases to AI
          </span>
        </motion.h1>

        {/* Subtitle */}
        <motion.p
          {...fadeUp(0.15)}
          className="text-lg md:text-xl text-white max-w-xl mx-auto mb-8 leading-relaxed"
          style={{ textShadow: "0 2px 16px rgba(0,0,0,0.1)" }}
        >
          Auto-learns your schema. Compiles GraphQL into optimized SQL. Works as
          an MCP server for any AI assistant.
        </motion.p>

        {/* Feature pills — single row, compact */}
        <motion.div
          {...fadeUp(0.2)}
          className="flex flex-wrap items-center justify-center gap-x-6 gap-y-2 mb-8 text-sm text-white"
        >
          {[
            "8+ Databases",
            "Zero Resolver Code",
            "Single SQL Query",
            "MCP Server",
          ].map((label) => (
            <span key={label} className="flex items-center gap-1.5">
              <span className="w-1.5 h-1.5 rounded-full bg-teal-400" />
              {label}
            </span>
          ))}
        </motion.div>

        {/* Install command — compact inline */}
        <motion.div {...fadeUp(0.25)} className="mb-6">
          <div className="inline-flex items-center bg-white/[0.06] backdrop-blur-sm rounded-xl border border-white/10 overflow-hidden">
            {/* Tab switcher — left side */}
            <div className="flex items-center border-r border-white/10">
              {INSTALL_COMMANDS.map((cmd) => (
                <button
                  key={cmd.id}
                  type="button"
                  onClick={() => setActiveCmd(cmd.id)}
                  className={`px-3 py-2.5 font-mono text-xs transition-colors ${
                    activeCmd === cmd.id
                      ? "text-white bg-white/10"
                      : "text-white/40 hover:text-white/70"
                  }`}
                >
                  {cmd.label}
                </button>
              ))}
            </div>

            {/* Command text */}
            <code className="px-4 py-2.5 text-sm font-mono text-white/80 whitespace-nowrap">
              {activeCommand.command}
            </code>

            {/* Copy button */}
            <button
              onClick={handleCopy}
              className="px-3 py-2.5 text-white/40 hover:text-white/80 transition-colors border-l border-white/10"
              aria-label="Copy command"
            >
              {copied ? (
                <Check className="w-4 h-4 text-teal-400" />
              ) : (
                <Copy className="w-4 h-4" />
              )}
            </button>
          </div>
        </motion.div>

        {/* CTA buttons */}
        <motion.div
          {...fadeUp(0.3)}
          className="flex items-center justify-center gap-4"
        >
          <a href="#quickstart" className="btn-primary">
            Get Started
            <ArrowRight className="w-4 h-4" />
          </a>
          <a
            href="https://github.com/dosco/graphjin"
            target="_blank"
            rel="noopener noreferrer"
            className="btn-secondary"
          >
            <svg
              className="w-5 h-5"
              fill="currentColor"
              viewBox="0 0 24 24"
              aria-hidden="true"
            >
              <path
                fillRule="evenodd"
                d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z"
                clipRule="evenodd"
              />
            </svg>
            GitHub
          </a>
        </motion.div>
      </div>
    </section>
  );
}

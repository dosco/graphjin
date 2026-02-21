import React, { useState, useRef, useEffect } from 'react';
import { Copy, Check } from 'lucide-react';
import { motion } from 'framer-motion';

const tabs = [
  {
    id: 'npm',
    label: 'Global',
    command: 'npx graphjin serve',
    description: 'Use with Node.js projects',
  },
  {
    id: 'brew',
    label: 'MacOS',
    command: 'brew install dosco/graphjin/graphjin',
    description: 'Install the GraphJin binary',
  },
  { 
    id: 'scoop',
    label: 'Windows',
    command: 'scoop install graphjin',
    description: 'Install the GraphJin binary',
  },
  
];

const mcpClients = [
  {
    id: 'claude-code',
    name: 'Claude Code',
    logo: '/logos/claude-code.svg',
    command: 'graphjin mcp install --client claude --scope project --yes',
    description: 'Project-scoped non-interactive install for Claude Code',
  },
  {
    id: 'openai-codex',
    name: 'OpenAI Codex',
    logo: '/logos/openai-codex.svg',
    command: 'graphjin mcp install --client codex --scope project --yes',
    description: 'Project-scoped non-interactive install for OpenAI Codex',
  },
];

export default function QuickStart() {
  const [activeTab, setActiveTab] = useState('npm');
  const [copied, setCopied] = useState(false);
  const [copiedMCP, setCopiedMCP] = useState<string | null>(null);
  const [indicatorStyle, setIndicatorStyle] = useState({ left: 0, width: 0 });
  const tabsRef = useRef<(HTMLButtonElement | null)[]>([]);

  const activeTabData = tabs.find((t) => t.id === activeTab);
  const activeCommand = activeTabData?.command || '';
  const activeDescription = activeTabData?.description || '';

  useEffect(() => {
    const activeIndex = tabs.findIndex((t) => t.id === activeTab);
    const activeButton = tabsRef.current[activeIndex];
    if (activeButton) {
      setIndicatorStyle({
        left: activeButton.offsetLeft,
        width: activeButton.offsetWidth,
      });
    }
  }, [activeTab]);

  const handleCopy = () => {
    navigator.clipboard.writeText(activeCommand);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleCopyMCP = (id: string, command: string) => {
    navigator.clipboard.writeText(command);
    setCopiedMCP(id);
    setTimeout(() => setCopiedMCP(null), 2000);
  };

  return (
    <section id="quickstart" className="py-24">
      <div className="max-w-4xl mx-auto px-4">
        <h2 className="text-2xl md:text-3xl font-display font-bold text-gj-text text-center mb-6">
          Quick Start
        </h2>

        {/* Terminal Window */}
        <div className="rounded-2xl border border-white/10 bg-black overflow-hidden shadow-xl">
          {/* Header with tabs */}
          <div className="bg-black/90 px-4 py-4 border-b border-white/10">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <div className="w-3 h-3 rounded-full bg-[#FF5F56]" />
                <div className="w-3 h-3 rounded-full bg-[#FFBD2E]" />
                <div className="w-3 h-3 rounded-full bg-[#27C93F]" />

                <div className="relative flex ml-6 gap-1 bg-white/5 rounded-lg p-1">
                  {/* Animated indicator */}
                  <motion.div
                    className="absolute top-1 bottom-1 bg-white/10 rounded-md"
                    initial={false}
                    animate={{
                      left: indicatorStyle.left,
                      width: indicatorStyle.width,
                    }}
                    transition={{ type: 'spring', stiffness: 400, damping: 30 }}
                  />

                  {tabs.map((tab, index) => (
                    <button
                      type="button"
                      key={tab.id}
                      ref={(el) => {
                        tabsRef.current[index] = el;
                      }}
                      onClick={() => setActiveTab(tab.id)}
                      className={`relative z-10 px-4 py-1.5 text-sm font-medium rounded-md transition-colors
                        ${
                          activeTab === tab.id
                            ? 'text-white'
                            : 'text-white/50 hover:text-white/80'
                        }`}
                    >
                      {tab.label}
                    </button>
                  ))}
                </div>
              </div>

              <button
                type="button"
                onClick={handleCopy}
                className="p-2 text-white/50 hover:text-white transition-colors rounded-lg hover:bg-white/5"
                title="Copy to clipboard"
                aria-label={`Copy install command for ${activeTabData?.label || 'active tab'}`}
              >
                {copied ? (
                  <Check className="w-5 h-5 text-emerald-400" />
                ) : (
                  <Copy className="w-5 h-5" />
                )}
              </button>
            </div>
          </div>

          {/* Command */}
          <div className="p-8 bg-black">
            <div className="flex items-start gap-4 font-mono">
              <span className="text-white/50 text-lg select-none">$</span>
              <motion.code
                key={activeTab}
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.2 }}
                className="text-white text-lg md:text-xl break-all leading-relaxed"
              >
                {activeCommand}
              </motion.code>
            </div>

            {/* Description */}
            <motion.p
              key={`desc-${activeTab}`}
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              transition={{ duration: 0.2, delay: 0.1 }}
              className="mt-4 text-white/40 text-sm pl-8"
            >
              {activeDescription}
            </motion.p>
          </div>
        </div>

        <div className="mt-6 rounded-2xl border border-white/10 bg-black/80 p-4 md:p-6">
          <div className="flex items-center justify-between gap-4">
            <h3 className="text-base md:text-lg font-semibold tracking-wide text-white/90">
              MCP Client Setup
            </h3>
            <span className="text-xs text-white/40">Copy and run one command</span>
          </div>

          <div className="mt-4 grid grid-cols-1 md:grid-cols-2 gap-3">
            {mcpClients.map((client) => (
              <div
                key={client.id}
                className="rounded-xl border border-white/10 bg-black p-5"
              >
                <div className="flex items-center justify-between gap-3">
                  <div className="flex items-center gap-2.5 min-w-0">
                    <img
                      src={client.logo}
                      alt={`${client.name} logo`}
                      className="w-[180px] h-[40px] md:w-[220px] md:h-[48px] object-contain object-left"
                      loading="lazy"
                    />
                  </div>

                  <button
                    type="button"
                    onClick={() => handleCopyMCP(client.id, client.command)}
                    className="p-2 text-white/50 hover:text-white transition-colors rounded-lg hover:bg-white/5"
                    title={`Copy ${client.name} command`}
                    aria-label={`Copy MCP install command for ${client.name}`}
                  >
                    {copiedMCP === client.id ? (
                      <Check className="w-4 h-4 text-emerald-400" />
                    ) : (
                      <Copy className="w-4 h-4" />
                    )}
                  </button>
                </div>

                <p className="mt-3 text-sm text-white/50">{client.description}</p>
                <code className="mt-3 block text-sm md:text-[15px] font-mono text-white/90 break-all leading-relaxed">
                  {client.command}
                </code>
              </div>
            ))}
          </div>

          <p className="mt-4 text-xs text-white/50">
            Prefer interactive setup? Run: <code className="font-mono text-white/80">graphjin mcp install</code>
          </p>
        </div>

        <p className="text-center text-gj-muted text-sm mt-8">
          Works on macOS, Windows, and Linux. Supports PostgreSQL, MySQL,
          SQLite, MongoDB, and more.
        </p>
      </div>
    </section>
  );
}

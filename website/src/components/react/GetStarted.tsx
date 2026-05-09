import { useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Database, DatabaseZap, ArrowRight } from "lucide-react";

type Path = "existing" | "new";

interface Step {
  title: string;
  description: string;
}

const paths: Record<Path, { steps: Step[] }> = {
  existing: {
    steps: [
      { title: "Point to your database", description: "Configure the connection — PostgreSQL, MySQL, SQLite, MongoDB, Oracle, MSSQL." },
      { title: "Auto-discover schema", description: "GraphJin introspects tables, columns, and relationships on boot." },
      { title: "Start querying", description: "Joins, mutations, subscriptions, federation, MCP — all out of the box." },
    ],
  },
  new: {
    steps: [
      { title: "Describe your project", description: "Tell Claude or any MCP client about the tables and relationships you need." },
      { title: "Preview & apply schema", description: "GraphJin previews changes as db.graphql and applies them transactionally." },
      { title: "Start querying", description: "The schema auto-reloads; queries work immediately, no restart needed." },
    ],
  },
};

const ease = [0.25, 0.46, 0.45, 0.94] as const;

export default function GetStarted() {
  const [path, setPath] = useState<Path>("existing");
  const { steps } = paths[path];

  return (
    <section
      id="get-started"
      className="py-20 md:py-28 border-t"
      style={{ borderColor: "var(--color-border)" }}
    >
      <div className="container-doc max-w-5xl">
        <header className="mb-10">
          <span className="eyebrow">GET STARTED</span>
          <h2
            className="mt-4 text-3xl md:text-5xl font-display font-bold tracking-tight"
            style={{ color: "var(--color-text)", lineHeight: 1.1 }}
          >
            Two paths. Both end with queries running.
          </h2>
        </header>

        <div className="flex flex-col sm:flex-row gap-3 mb-12">
          <button
            type="button"
            onClick={() => setPath("existing")}
            className="inline-flex items-center justify-center gap-2 px-5 py-2.5 rounded-lg text-sm font-medium transition-colors"
            style={{
              border: "1px solid var(--color-border)",
              background:
                path === "existing" ? "var(--color-accent-soft)" : "transparent",
              color:
                path === "existing" ? "var(--color-accent)" : "var(--color-muted)",
            }}
          >
            <Database className="w-4 h-4" />
            Existing database
          </button>
          <button
            type="button"
            onClick={() => setPath("new")}
            className="inline-flex items-center justify-center gap-2 px-5 py-2.5 rounded-lg text-sm font-medium transition-colors"
            style={{
              border: "1px solid var(--color-border)",
              background:
                path === "new" ? "var(--color-accent-soft)" : "transparent",
              color:
                path === "new" ? "var(--color-accent)" : "var(--color-muted)",
            }}
          >
            <DatabaseZap className="w-4 h-4" />
            Start fresh
          </button>
        </div>

        <AnimatePresence mode="wait">
          <motion.ol
            key={path}
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -8 }}
            transition={{ duration: 0.3, ease }}
            className="grid grid-cols-1 md:grid-cols-3 gap-5"
          >
            {steps.map((s, i) => (
              <li
                key={s.title}
                className="rounded-2xl border p-5"
                style={{
                  background: "var(--color-surface)",
                  borderColor: "var(--color-border)",
                }}
              >
                <div
                  className="w-8 h-8 rounded-lg flex items-center justify-center font-mono text-sm font-bold"
                  style={{
                    background: "var(--color-accent-soft)",
                    color: "var(--color-accent)",
                  }}
                >
                  {i + 1}
                </div>
                <h3
                  className="mt-4 font-display font-semibold text-lg"
                  style={{ color: "var(--color-text)" }}
                >
                  {s.title}
                </h3>
                <p className="mt-2 text-sm leading-relaxed" style={{ color: "var(--color-muted)" }}>
                  {s.description}
                </p>
              </li>
            ))}
          </motion.ol>
        </AnimatePresence>

        <div className="mt-10 flex flex-wrap items-center gap-3">
          <a href="#quickstart" className="btn-primary">
            Install GraphJin
            <ArrowRight className="w-4 h-4" />
          </a>
          <a
            href="https://github.com/dosco/graphjin/blob/master/FEATURES.md"
            target="_blank"
            rel="noopener noreferrer"
            className="btn-secondary"
          >
            Features
          </a>
          <a
            href="https://github.com/dosco/graphjin/blob/master/CONFIG.md"
            target="_blank"
            rel="noopener noreferrer"
            className="btn-secondary"
          >
            Configuration
          </a>
        </div>
      </div>
    </section>
  );
}

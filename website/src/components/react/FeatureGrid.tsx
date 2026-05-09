import { motion } from "framer-motion";
import {
  Shield,
  Search,
  Radio,
  Layers,
  Globe,
  GitBranch,
  Database,
  Cpu,
  Lock,
} from "lucide-react";

const EASE = [0.25, 0.46, 0.45, 0.94] as const;

const stats = [
  { value: "1", label: "SQL statement per query, regardless of nesting depth." },
  { value: "8+", label: "Databases supported with the same GraphQL surface." },
  { value: "0", label: "Lines of resolver code. The compiler does the work." },
];

const features = [
  {
    icon: Search,
    title: "Auto schema discovery",
    body: "Tables, columns, foreign keys, views — introspected on boot and refreshed live.",
  },
  {
    icon: Cpu,
    title: "Cross-database joins",
    body: "Compose data across multiple databases in a single GraphQL request.",
  },
  {
    icon: Shield,
    title: "Production security",
    body: "RBAC, JWT, row-level rules, allow-lists, audit logs — out of the box.",
  },
  {
    icon: Radio,
    title: "Live subscriptions",
    body: "SSE and WebSocket transports with cursor-based resume.",
  },
  {
    icon: Database,
    title: "Workflows",
    body: "Chain queries with JS — invoke from REST, MCP, or another workflow.",
  },
  {
    icon: Lock,
    title: "Read-only replicas",
    body: "Lock a database to query-only with a single config flag.",
  },
  {
    icon: Globe,
    title: "Remote API joins",
    body: "Stitch in REST and GraphQL endpoints alongside your tables.",
  },
  {
    icon: GitBranch,
    title: "Recursive queries",
    body: "Walk graphs and hierarchies natively — no custom resolvers.",
  },
  {
    icon: Layers,
    title: "Redis cache",
    body: "Response caching with mutation-driven invalidation.",
  },
];

const containerVariants = {
  hidden: { opacity: 0 },
  visible: { opacity: 1, transition: { staggerChildren: 0.06 } },
};

const itemVariants = {
  hidden: { opacity: 0, y: 16 },
  visible: { opacity: 1, y: 0, transition: { duration: 0.4, ease: EASE } },
};

export default function FeatureGrid() {
  return (
    <section id="features" className="py-20 md:py-28 border-t" style={{ borderColor: "var(--color-border)" }}>
      <div className="container-doc">
        <header className="max-w-3xl mb-12">
          <span className="eyebrow">FEATURES</span>
          <h2
            className="mt-4 text-3xl md:text-5xl font-display font-bold tracking-tight"
            style={{ color: "var(--color-text)", lineHeight: 1.1 }}
          >
            Everything a real backend needs.
          </h2>
          <p className="mt-4 text-lg leading-relaxed" style={{ color: "var(--color-muted)" }}>
            One binary, one config file — federation, MCP, uploads, auth,
            subscriptions, and a CLI. No plugins, no add-ons, no surprise
            billing tiers.
          </p>
        </header>

        {/* stats */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-10">
          {stats.map((s) => (
            <div
              key={s.value}
              className="rounded-2xl border px-6 py-7"
              style={{
                background: "var(--color-surface)",
                borderColor: "var(--color-border)",
              }}
            >
              <div
                className="font-display font-bold leading-none"
                style={{
                  color: "var(--color-accent)",
                  fontSize: "clamp(2.5rem, 4vw, 3.5rem)",
                }}
              >
                {s.value}
              </div>
              <p className="mt-3 text-sm leading-relaxed" style={{ color: "var(--color-muted)" }}>
                {s.label}
              </p>
            </div>
          ))}
        </div>

        {/* feature grid */}
        <motion.div
          className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4"
          variants={containerVariants}
          initial="hidden"
          whileInView="visible"
          viewport={{ once: true, margin: "-80px" }}
        >
          {features.map((f) => (
            <motion.div
              key={f.title}
              variants={itemVariants}
              className="rounded-2xl border p-5 transition-colors"
              style={{
                background: "var(--color-bg)",
                borderColor: "var(--color-border)",
              }}
            >
              <div
                className="w-9 h-9 rounded-lg flex items-center justify-center mb-3"
                style={{
                  background: "var(--color-accent-soft)",
                  color: "var(--color-accent)",
                }}
              >
                <f.icon className="w-4 h-4" />
              </div>
              <h3
                className="font-display font-semibold mb-1"
                style={{ color: "var(--color-text)" }}
              >
                {f.title}
              </h3>
              <p className="text-sm leading-relaxed" style={{ color: "var(--color-muted)" }}>
                {f.body}
              </p>
            </motion.div>
          ))}
        </motion.div>
      </div>
    </section>
  );
}

import { ArrowRight, Shield } from "lucide-react";
import GraphField from "./GraphField";

const PROOF_STEPS = [
  { title: "Auto-learn", detail: "schema + sources" },
  { title: "Compile", detail: "optimized DB work" },
  { title: "Govern", detail: "policy boundaries" },
  { title: "Audit", detail: "human/model review" },
];

export default function HeroSection() {
  return (
    <section className="relative overflow-hidden pt-28 pb-16 sm:pt-32 md:pt-40 md:pb-24">
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
        className="absolute inset-0 -z-10 pointer-events-none opacity-75"
        style={{
          maskImage:
            "radial-gradient(ellipse 90% 76% at 50% 30%, black 26%, transparent 86%)",
          WebkitMaskImage:
            "radial-gradient(ellipse 90% 76% at 50% 30%, black 26%, transparent 86%)",
        }}
      >
        <GraphField />
      </div>

      <div className="container-doc max-w-5xl">
        <div className="flex flex-col items-center text-center">
          <span className="eyebrow mb-4">v3 · Apache 2.0</span>

          <h1
            className="hero-title text-balance font-semibold tracking-normal sm:font-bold"
            style={{
              color: "var(--color-text)",
              fontFamily: "var(--font-body)",
              fontSize: "2.85rem",
              lineHeight: 1.02,
              maxWidth: "11ch",
            }}
          >
            Automatic GraphQL for AI agents.
          </h1>

          <p
            className="mt-5 max-w-3xl text-base leading-relaxed sm:text-lg md:text-xl"
            style={{ color: "var(--color-muted)" }}
          >
            Point GraphJin at your databases, APIs, source code, files, and
            workflows. It auto-learns the shape, compiles queries into
            optimized database work, and exposes one governed GraphQL + MCP
            surface for agents.
          </p>

          <p
            className="mt-4 max-w-2xl text-sm leading-relaxed sm:text-base"
            style={{ color: "var(--color-muted)" }}
          >
            <span
              className="font-semibold"
              style={{ color: "var(--color-text)" }}
            >
              One config
            </span>{" "}
            controls what agents can discover, query, execute, edit, and never
            touch.
          </p>

          <div className="mt-7 grid w-full max-w-3xl grid-cols-2 gap-2 sm:grid-cols-4">
            {PROOF_STEPS.map((step, index) => (
              <div
                key={step.title}
                className="relative rounded-lg border px-3 py-3 text-left sm:text-center"
                style={{
                  background:
                    "color-mix(in srgb, var(--color-surface) 76%, transparent)",
                  borderColor:
                    index === 0
                      ? "color-mix(in srgb, var(--color-accent) 38%, var(--color-border))"
                      : "var(--color-border)",
                }}
              >
                <div
                  className="font-mono text-[11px] uppercase tracking-[0.16em]"
                  style={{ color: "var(--color-accent-readable)" }}
                >
                  {String(index + 1).padStart(2, "0")}
                </div>
                <div
                  className="mt-1 font-semibold"
                  style={{ color: "var(--color-text)" }}
                >
                  {step.title}
                </div>
                <div
                  className="mt-1 text-xs leading-snug"
                  style={{ color: "var(--color-muted)" }}
                >
                  {step.detail}
                </div>
              </div>
            ))}
          </div>

          <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
            <a href="#quickstart" className="btn-primary">
              Get started
              <ArrowRight className="w-4 h-4" />
            </a>
            <a href="#security-model" className="btn-secondary">
              <Shield className="w-4 h-4" />
              Security model
            </a>
          </div>
        </div>
      </div>

      <style>{`
        @media (min-width: 640px) {
          .hero-title {
            font-size: 4rem !important;
            max-width: 13ch !important;
          }
        }

        @media (min-width: 768px) {
          .hero-title {
            font-size: 4.5rem !important;
          }
        }
      `}</style>
    </section>
  );
}

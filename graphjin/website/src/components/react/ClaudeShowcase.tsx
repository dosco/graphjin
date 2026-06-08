import { Sparkles, Check, ChevronDown, ArrowRight } from "lucide-react";

const graphqlQuery = `{ customers { id full_name email purchases { quantity product { price } } } }`;

const results = [
  {
    rank: "\u{1F947}",
    name: "Antwan Friesen",
    email: "francohirthe@medhurst.com",
    orders: 20,
    items: 124,
    total: "$928.45",
  },
  {
    rank: "\u{1F948}",
    name: "Lon Cruickshank",
    email: "margaretbailey@ruecker.info",
    orders: 20,
    items: 94,
    total: "$586.50",
  },
  {
    rank: "\u{1F949}",
    name: "Susana Schaefer",
    email: "jewelpowlowski@osinski.biz",
    orders: 20,
    items: 91,
    total: "$580.72",
  },
];

export default function ClaudeShowcase() {
  return (
    <section
      id="ai-queries"
      className="py-20 md:py-28 border-t"
      style={{ borderColor: "var(--color-border)" }}
    >
      <div className="container-doc">
        <header className="max-w-3xl mb-12 mx-auto text-center">
          <span className="eyebrow">AI-POWERED QUERIES</span>
          <h2
            className="mt-4 text-3xl md:text-5xl font-display font-bold tracking-tight"
            style={{ color: "var(--color-text)", lineHeight: 1.08 }}
          >
            Ask in plain English. Get real data back.
          </h2>
          <p
            className="mt-4 text-lg md:text-xl leading-relaxed"
            style={{ color: "var(--color-muted)" }}
          >
            Claude Desktop, Codex, or any MCP client talks to GraphJin —
            GraphJin compiles the query, hits your database, and the assistant
            answers with rows it can reason over.
          </p>
        </header>

        {/* Claude Desktop-style window */}
        <div className="max-w-4xl mx-auto">
          <div
            className="rounded-2xl overflow-hidden border"
            style={{
              background:
                "linear-gradient(135deg, var(--color-code-bg), #161713)",
              borderColor: "rgba(255, 255, 255, 0.08)",
              boxShadow:
                "0 30px 80px -20px rgba(0,0,0,0.45), 0 12px 32px -12px rgba(182,252,52,0.16)",
            }}
          >
            {/* window chrome */}
            <div
              className="px-4 py-3 border-b flex items-center gap-2"
              style={{ borderColor: "rgba(255, 255, 255, 0.08)" }}
            >
              <div className="w-3 h-3 rounded-full bg-[#FF5F56]" />
              <div className="w-3 h-3 rounded-full bg-[#FFBD2E]" />
              <div className="w-3 h-3 rounded-full bg-[#27C93F]" />
              <div className="ml-2 flex items-center gap-2">
                <Sparkles className="w-4 h-4 text-[#D97757]" />
                <span className="text-xs text-white/60 font-medium">
                  Claude Desktop
                </span>
              </div>
            </div>

            {/* chat */}
            <div className="p-4 md:p-8 flex flex-col gap-6">
              {/* user message */}
              <div className="flex justify-end">
                <div className="bg-white/10 rounded-2xl rounded-br-sm px-5 py-3 max-w-md">
                  <p className="text-white text-sm">who's the top customer?</p>
                </div>
              </div>

              {/* assistant message */}
              <div className="flex gap-3">
                <div className="w-8 h-8 rounded-full bg-[#D97757]/20 flex items-center justify-center shrink-0 mt-1">
                  <Sparkles className="w-4 h-4 text-[#D97757]" />
                </div>

                <div className="flex flex-col gap-3 min-w-0 flex-1">
                  <div className="bg-white/5 rounded-xl border border-white/10 overflow-hidden">
                    <div className="px-4 py-2.5 flex items-center gap-2 text-xs text-white/60">
                      <ChevronDown className="w-3.5 h-3.5" />
                      <span className="font-medium">execute_graphql</span>
                    </div>
                    <div className="px-4 pb-3">
                      <pre
                        className="text-xs font-mono leading-relaxed overflow-x-auto whitespace-pre-wrap"
                        style={{ color: "var(--color-accent)" }}
                      >
                        {graphqlQuery}
                      </pre>
                    </div>
                  </div>

                  <div className="flex items-center gap-1.5">
                    <Check className="w-3.5 h-3.5 text-emerald-400" />
                    <span className="text-xs text-emerald-400 font-medium">
                      Done
                    </span>
                  </div>

                  <p className="text-sm text-white/90 leading-relaxed">
                    Based on the purchase data, here are the top customers
                    ranked by total spend:
                  </p>

                  <div className="overflow-x-auto">
                    <table className="w-full text-xs text-left">
                      <thead>
                        <tr className="border-b border-white/10 text-white/50">
                          <th className="pb-2 pr-4 font-medium">Rank</th>
                          <th className="pb-2 pr-4 font-medium">Customer</th>
                          <th className="pb-2 pr-4 font-medium hidden sm:table-cell">
                            Email
                          </th>
                          <th className="pb-2 pr-4 font-medium text-right">
                            Orders
                          </th>
                          <th className="pb-2 pr-4 font-medium text-right">
                            Items
                          </th>
                          <th className="pb-2 font-medium text-right">
                            Total Spent
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {results.map((r) => (
                          <tr key={r.name} className="border-b border-white/5">
                            <td className="py-2 pr-4">{r.rank}</td>
                            <td className="py-2 pr-4 text-white font-medium whitespace-nowrap">
                              {r.name}
                            </td>
                            <td className="py-2 pr-4 text-white/50 hidden sm:table-cell">
                              {r.email}
                            </td>
                            <td className="py-2 pr-4 text-white/70 text-right">
                              {r.orders}
                            </td>
                            <td className="py-2 pr-4 text-white/70 text-right">
                              {r.items}
                            </td>
                            <td className="py-2 text-emerald-400 font-medium text-right whitespace-nowrap">
                              {r.total}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>

                  <p className="text-sm text-white/90 leading-relaxed">
                    Antwan Friesen is the top customer with almost $1,000 in
                    purchases — about 60% more than the runner-up.
                  </p>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div className="text-center mt-10">
          <a href="#quickstart" className="btn-primary">
            Try it yourself
            <ArrowRight className="w-4 h-4" />
          </a>
        </div>
      </div>
    </section>
  );
}

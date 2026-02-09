import React from 'react';
import { motion } from 'framer-motion';
import { Sparkles, Check, ChevronDown } from 'lucide-react';

const containerVariants = {
  hidden: { opacity: 0 },
  visible: {
    opacity: 1,
    transition: { staggerChildren: 0.15 },
  },
};

const ease = [0.25, 0.46, 0.45, 0.94] as const;

const itemVariants = {
  hidden: { opacity: 0, y: 20 },
  visible: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.5, ease },
  },
};

const graphqlQuery = `{ customers { id full_name email purchases { quantity product { price } } } }`;

const results = [
  {
    rank: '\u{1F947}',
    name: 'Antwan Friesen',
    email: 'francohirthe@medhurst.com',
    orders: 20,
    items: 124,
    total: '$928.45',
  },
  {
    rank: '\u{1F948}',
    name: 'Lon Cruickshank',
    email: 'margaretbailey@ruecker.info',
    orders: 20,
    items: 94,
    total: '$586.50',
  },
  {
    rank: '\u{1F949}',
    name: 'Susana Schaefer',
    email: 'jewelpowlowski@osinski.biz',
    orders: 20,
    items: 91,
    total: '$580.72',
  },
];

export default function ClaudeShowcase() {
  return (
    <section className="py-24 border-t border-black/10">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        {/* Header */}
        <div className="text-center max-w-2xl mx-auto mb-14">
          <motion.h2
            initial={{ opacity: 0, y: 20 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true }}
            transition={{ duration: 0.5 }}
            className="text-3xl md:text-5xl font-display font-bold text-gj-text mb-4"
          >
            AI-Powered Database Queries
          </motion.h2>
          <motion.p
            initial={{ opacity: 0, y: 20 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true }}
            transition={{ duration: 0.5, delay: 0.1 }}
            className="text-gj-muted text-lg"
          >
            Ask questions in plain English. GraphJin + Claude Desktop handles the rest.
          </motion.p>
        </div>

        {/* Chat window */}
        <motion.div
          initial={{ opacity: 0, y: 40 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true }}
          transition={{ duration: 0.6, ease: [0.25, 0.46, 0.45, 0.94] }}
          className="max-w-4xl mx-auto"
        >
          <div className="bg-indigo-900 rounded-2xl border border-white/10 shadow-2xl overflow-hidden">
            {/* Window chrome */}
            <div className="px-4 py-3 border-b border-white/10 flex items-center gap-2">
              <div className="w-3 h-3 rounded-full bg-[#FF5F56]" />
              <div className="w-3 h-3 rounded-full bg-[#FFBD2E]" />
              <div className="w-3 h-3 rounded-full bg-[#27C93F]" />
              <div className="ml-2 flex items-center gap-2">
                <Sparkles className="w-4 h-4 text-[#D97757]" />
                <span className="text-xs text-white/50 font-medium">Claude Desktop</span>
              </div>
            </div>

            {/* Chat area */}
            <motion.div
              className="p-4 md:p-8 flex flex-col gap-6"
              variants={containerVariants}
              initial="hidden"
              whileInView="visible"
              viewport={{ once: true }}
            >
              {/* User message */}
              <motion.div variants={itemVariants} className="flex justify-end">
                <div className="bg-white/10 rounded-2xl rounded-br-sm px-5 py-3 max-w-md">
                  <p className="text-gj-text text-sm">who's the top customer?</p>
                </div>
              </motion.div>

              {/* Claude response */}
              <motion.div variants={itemVariants} className="flex gap-3">
                {/* Avatar */}
                <div className="w-8 h-8 rounded-full bg-[#D97757]/20 flex items-center justify-center shrink-0 mt-1">
                  <Sparkles className="w-4 h-4 text-[#D97757]" />
                </div>

                <div className="flex flex-col gap-3 min-w-0 flex-1">
                  {/* Tool call block */}
                  <motion.div variants={itemVariants} className="bg-white/5 rounded-xl border border-white/10 overflow-hidden">
                    <div className="px-4 py-2.5 flex items-center gap-2 text-xs text-white/50">
                      <ChevronDown className="w-3.5 h-3.5" />
                      <span className="font-medium">execute_graphql</span>
                    </div>
                    <div className="px-4 pb-3">
                      <pre className="text-xs text-purple-300 font-mono leading-relaxed overflow-x-auto whitespace-pre-wrap">
                        {graphqlQuery}
                      </pre>
                    </div>
                  </motion.div>

                  {/* Done indicator */}
                  <motion.div variants={itemVariants} className="flex items-center gap-1.5">
                    <Check className="w-3.5 h-3.5 text-emerald-400" />
                    <span className="text-xs text-emerald-400 font-medium">Done</span>
                  </motion.div>

                  {/* Response text */}
                  <motion.p variants={itemVariants} className="text-sm text-gj-text/90 leading-relaxed">
                    Based on the purchase data, here are the top customers ranked by total spend:
                  </motion.p>

                  {/* Results table */}
                  <motion.div variants={itemVariants} className="overflow-x-auto">
                    <table className="w-full text-xs text-left">
                      <thead>
                        <tr className="border-b border-white/10 text-white/40">
                          <th className="pb-2 pr-4 font-medium">Rank</th>
                          <th className="pb-2 pr-4 font-medium">Customer</th>
                          <th className="pb-2 pr-4 font-medium hidden sm:table-cell">Email</th>
                          <th className="pb-2 pr-4 font-medium text-right">Orders</th>
                          <th className="pb-2 pr-4 font-medium text-right">Items</th>
                          <th className="pb-2 font-medium text-right">Total Spent</th>
                        </tr>
                      </thead>
                      <tbody>
                        {results.map((r) => (
                          <tr key={r.name} className="border-b border-white/5">
                            <td className="py-2 pr-4">{r.rank}</td>
                            <td className="py-2 pr-4 text-gj-text font-medium whitespace-nowrap">{r.name}</td>
                            <td className="py-2 pr-4 text-white/40 hidden sm:table-cell">{r.email}</td>
                            <td className="py-2 pr-4 text-white/60 text-right">{r.orders}</td>
                            <td className="py-2 pr-4 text-white/60 text-right">{r.items}</td>
                            <td className="py-2 text-emerald-400 font-medium text-right whitespace-nowrap">{r.total}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </motion.div>

                  {/* Summary */}
                  <motion.p variants={itemVariants} className="text-sm text-gj-text/90 leading-relaxed">
                    Antwan Friesen is the top customer with almost $1,000 in purchases — about 60% more than the runner-up.
                  </motion.p>
                </div>
              </motion.div>
            </motion.div>
          </div>
        </motion.div>
      </div>
    </section>
  );
}

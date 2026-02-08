import React, { useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { Database, DatabaseZap, Terminal } from 'lucide-react';

type Path = 'new' | 'existing';

interface Step {
  title: string;
  description: string;
}

const paths: Record<Path, { steps: Step[] }> = {
  new: {
    steps: [
      {
        title: 'Describe Your Project',
        description:
          'Tell Claude Desktop about your tables, relationships, and fields using natural language.',
      },
      {
        title: 'Preview & Apply Schema',
        description:
          'GraphJin previews changes as db.graphql and applies them transactionally to your database.',
      },
      {
        title: 'Start Querying',
        description:
          'Schema auto-reloads — write GraphQL queries immediately, no restart needed.',
      },
    ],
  },
  existing: {
    steps: [
      {
        title: 'Point to Your Database',
        description:
          'Configure your connection — PostgreSQL, MySQL, SQLite, and more.',
      },
      {
        title: 'Auto-Discover Schema',
        description:
          'GraphJin introspects tables, columns, and relationships automatically.',
      },
      {
        title: 'Start Querying',
        description:
          'Joins, aggregations, and subscriptions work out of the box.',
      },
    ],
  },
};

const dbGraphqlCode = `type users {
  id: BigInt! @id
  full_name: String!
  email: String! @unique
  created_at: Timestamptz
  products: [products] @relation
}

type products {
  id: BigInt! @id
  name: String!
  price: Numeric!
  owner_id: BigInt!
  owner: users @relation
}`;

const containerVariants = {
  hidden: { opacity: 0 },
  visible: {
    opacity: 1,
    transition: { staggerChildren: 0.15 },
  },
  exit: { opacity: 0, transition: { duration: 0.2 } },
};

const stepVariants = {
  hidden: { opacity: 0, y: 30 },
  visible: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.5, ease: [0.25, 0.46, 0.45, 0.94] },
  },
};

const codeVariants = {
  hidden: { opacity: 0, y: 20 },
  visible: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.5, delay: 0.5, ease: [0.25, 0.46, 0.45, 0.94] },
  },
};

export default function GetStarted() {
  const [path, setPath] = useState<Path>('existing');
  const { steps } = paths[path];

  return (
    <section id="get-started" className="py-24 border-t border-white/10">
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
            Get Started
          </motion.h2>
          <motion.p
            initial={{ opacity: 0, y: 20 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true }}
            transition={{ duration: 0.5, delay: 0.1 }}
            className="text-gj-muted text-lg"
          >
            From zero to queries in minutes.
          </motion.p>
        </div>

        {/* Path Toggle */}
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          whileInView={{ opacity: 1, y: 0 }}
          viewport={{ once: true }}
          transition={{ duration: 0.5, delay: 0.15 }}
          className="flex flex-col sm:flex-row justify-center gap-3 mb-16"
        >
          <button
            type="button"
            onClick={() => setPath('existing')}
            className={`flex items-center justify-center gap-2 px-6 py-3 rounded-full text-sm font-medium transition-all duration-200 ${
              path === 'existing'
                ? 'bg-emerald-500/20 text-emerald-300 border border-emerald-500/40 shadow-[0_0_20px_rgba(52,211,153,0.15)]'
                : 'bg-white/5 text-gj-muted border border-white/10 hover:bg-white/10 hover:text-gj-text'
            }`}
          >
            <Database className="w-4 h-4" />
            Start with Existing Database
          </button>
          <button
            type="button"
            onClick={() => setPath('new')}
            className={`flex items-center justify-center gap-2 px-6 py-3 rounded-full text-sm font-medium transition-all duration-200 ${
              path === 'new'
                ? 'bg-teal-500/20 text-teal-300 border border-teal-500/40 shadow-[0_0_20px_rgba(45,212,191,0.15)]'
                : 'bg-white/5 text-gj-muted border border-white/10 hover:bg-white/10 hover:text-gj-text'
            }`}
          >
            <DatabaseZap className="w-4 h-4" />
            Start with Empty Database
          </button>
        </motion.div>

        {/* Timeline */}
        <AnimatePresence mode="wait">
          <motion.div
            key={path}
            variants={containerVariants}
            initial="hidden"
            animate="visible"
            exit="exit"
          >
            {/* Desktop: horizontal 3-col */}
            <div className="hidden md:block relative">
              {/* Connecting line */}
              <div className="absolute top-8 left-[calc(16.67%-0.5px)] right-[calc(16.67%-0.5px)] h-0.5 bg-gradient-to-r from-teal-500 via-cyan-500 to-emerald-500 opacity-30" />

              <div className="grid grid-cols-3 gap-8">
                {steps.map((step, i) => (
                  <motion.div
                    key={step.title}
                    variants={stepVariants}
                    className="flex flex-col items-center text-center"
                  >
                    {/* Circle */}
                    <div
                      className={`relative z-10 w-16 h-16 rounded-full flex items-center justify-center text-xl font-display font-bold border-2 bg-gj-bg ${
                        path === 'new'
                          ? 'border-teal-500/60 text-teal-400'
                          : 'border-emerald-500/60 text-emerald-400'
                      }`}
                      style={{
                        boxShadow:
                          path === 'new'
                            ? '0 0 24px rgba(45, 212, 191, 0.2)'
                            : '0 0 24px rgba(52, 211, 153, 0.2)',
                      }}
                    >
                      {i + 1}
                    </div>
                    {/* Text */}
                    <h3 className="font-display font-bold text-gj-text text-lg mt-5 mb-2">
                      {step.title}
                    </h3>
                    <p className="text-gj-muted text-sm leading-relaxed max-w-xs">
                      {step.description}
                    </p>
                  </motion.div>
                ))}
              </div>
            </div>

            {/* Mobile: vertical */}
            <div className="md:hidden flex flex-col gap-8">
              {steps.map((step, i) => (
                <motion.div
                  key={step.title}
                  variants={stepVariants}
                  className="flex gap-4"
                >
                  {/* Circle + connector */}
                  <div className="flex flex-col items-center">
                    <div
                      className={`w-12 h-12 rounded-full flex items-center justify-center text-lg font-display font-bold border-2 bg-gj-bg shrink-0 ${
                        path === 'new'
                          ? 'border-teal-500/60 text-teal-400'
                          : 'border-emerald-500/60 text-emerald-400'
                      }`}
                    >
                      {i + 1}
                    </div>
                    {i < steps.length - 1 && (
                      <div className="w-0.5 flex-1 mt-2 bg-gradient-to-b from-teal-500/40 to-emerald-500/40" />
                    )}
                  </div>
                  {/* Text */}
                  <div className="pt-2 pb-2">
                    <h3 className="font-display font-bold text-gj-text text-base mb-1">
                      {step.title}
                    </h3>
                    <p className="text-gj-muted text-sm leading-relaxed">
                      {step.description}
                    </p>
                  </div>
                </motion.div>
              ))}
            </div>

            {/* db.graphql code block — only for "new" path */}
            {path === 'new' && (
              <motion.div
                variants={codeVariants}
                className="mt-14 max-w-2xl mx-auto"
              >
                <div className="bg-black rounded-2xl border border-white/10 overflow-hidden shadow-2xl">
                  {/* Window chrome */}
                  <div className="bg-black/90 px-4 py-3 border-b border-white/10 flex items-center gap-2">
                    <div className="w-3 h-3 rounded-full bg-[#FF5F56]" />
                    <div className="w-3 h-3 rounded-full bg-[#FFBD2E]" />
                    <div className="w-3 h-3 rounded-full bg-[#27C93F]" />
                    <span className="ml-2 flex items-center gap-2 text-xs text-white/40 font-mono">
                      <Terminal className="w-3 h-3" /> db.graphql
                    </span>
                  </div>
                  {/* Code */}
                  <div className="p-6">
                    <pre className="text-sm text-teal-300/90 leading-relaxed overflow-x-auto">
                      {dbGraphqlCode}
                    </pre>
                  </div>
                </div>
              </motion.div>
            )}
          </motion.div>
        </AnimatePresence>
      </div>
    </section>
  );
}

import React, { useRef, useState } from 'react';
import { motion } from 'framer-motion';
import { Cpu, Zap, Shield, Search, Database, Code } from 'lucide-react';

const features = [
  {
    id: 'ai-native',
    icon: Cpu,
    title: 'AI Native',
    description: 'Works with Claude Desktop, GPT, and any MCP-compatible AI. The AI describes what data it needs in GraphQL — GraphJin handles the SQL.',
    size: 'featured',
  },
  {
    id: 'auto-discovery',
    icon: Search,
    title: 'Auto Discovery',
    description: 'Point it at your database. Tables, columns, relationships, and constraints are introspected automatically.',
    size: 'normal',
  },
  {
    id: 'single-query',
    icon: Zap,
    title: 'Single Query',
    description: 'Nested queries across 10 tables? Still compiles to one SQL statement. N+1 is eliminated at the compiler level.',
    size: 'normal',
  },
  {
    id: 'multi-database',
    icon: Database,
    title: 'Multi-Database',
    description: 'Postgres, MySQL, MongoDB, SQLite, MSSQL, Oracle, YugabyteDB, CockroachDB. Same GraphQL, any backend.',
    size: 'normal',
  },
  {
    id: 'production-ready',
    icon: Shield,
    title: 'Production Ready',
    description: 'Role-based access control, JWT authentication, query allow-lists, and row-level security built in.',
    size: 'normal',
  },
  {
    id: 'no-resolvers',
    icon: Code,
    title: 'No Resolvers',
    description: 'Skip the boilerplate. No resolver functions, no type definitions, no ORM mappings. Your schema is the API.',
    size: 'wide',
  },
];

const containerVariants = {
  hidden: { opacity: 0 },
  visible: {
    opacity: 1,
    transition: {
      staggerChildren: 0.1,
    },
  },
};

const cardVariants = {
  hidden: {
    opacity: 0,
    y: 40,
    scale: 0.95
  },
  visible: {
    opacity: 1,
    y: 0,
    scale: 1,
    transition: {
      duration: 0.5,
      ease: [0.25, 0.46, 0.45, 0.94],
    },
  },
};

interface FeatureCardProps {
  feature: typeof features[0];
}

function FeatureCard({ feature }: FeatureCardProps) {
  const Icon = feature.icon;
  const cardRef = useRef<HTMLDivElement>(null);
  const [mousePosition, setMousePosition] = useState({ x: 50, y: 50 });

  const handleMouseMove = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!cardRef.current) return;
    const rect = cardRef.current.getBoundingClientRect();
    const x = ((e.clientX - rect.left) / rect.width) * 100;
    const y = ((e.clientY - rect.top) / rect.height) * 100;
    setMousePosition({ x, y });
  };

  return (
    <motion.div
      ref={cardRef}
      variants={cardVariants}
      whileHover={{
        y: -4,
        transition: { duration: 0.2 }
      }}
      onMouseMove={handleMouseMove}
      className={`
        group relative overflow-hidden rounded-2xl
        bg-gradient-to-br from-white/[0.08] to-white/[0.02]
        border border-white/10
        backdrop-blur-sm
        transition-all duration-300
        hover:border-teal-500/30
        ${feature.size === 'featured' ? 'md:col-span-2 md:row-span-2' : ''}
        ${feature.size === 'wide' ? 'md:col-span-3' : ''}
      `}
    >
      {/* Mouse-tracking glow effect */}
      <div
        className="absolute inset-0 opacity-0 group-hover:opacity-100 transition-opacity duration-300 pointer-events-none"
        style={{
          background: `radial-gradient(400px circle at ${mousePosition.x}% ${mousePosition.y}%, rgba(45, 212, 191, 0.15), transparent 40%)`,
        }}
      />

      {/* Content */}
      <div className={`relative z-10 p-6 ${feature.size === 'featured' ? 'md:p-8' : ''} h-full flex flex-col`}>
        {/* Icon */}
        <div
          className={`
            w-12 h-12 rounded-xl flex items-center justify-center mb-4
            bg-teal-500/20 border border-teal-500/30
            ${feature.size === 'featured' ? 'md:w-14 md:h-14' : ''}
          `}
        >
          <Icon className={`w-6 h-6 text-teal-400 ${feature.size === 'featured' ? 'md:w-7 md:h-7' : ''}`} />
        </div>

        {/* Title */}
        <h3 className={`font-display font-bold text-gj-text mb-3 ${feature.size === 'featured' ? 'text-xl md:text-2xl' : 'text-lg'}`}>
          {feature.title}
        </h3>

        {/* Description */}
        <p className={`text-gj-muted leading-relaxed flex-grow ${feature.size === 'featured' ? 'text-base md:text-lg' : 'text-sm'}`}>
          {feature.description}
        </p>
      </div>

      {/* Subtle border glow on hover */}
      <div
        className="absolute inset-0 rounded-2xl opacity-0 group-hover:opacity-100 transition-opacity duration-300 pointer-events-none"
        style={{
          padding: '1px',
          background: `linear-gradient(135deg, rgba(45, 212, 191, 0.3), transparent 50%)`,
          WebkitMask: 'linear-gradient(#fff 0 0) content-box, linear-gradient(#fff 0 0)',
          WebkitMaskComposite: 'xor',
          maskComposite: 'exclude',
        }}
      />
    </motion.div>
  );
}

export default function FeatureGrid() {
  return (
    <section id="features" className="py-24 border-y border-white/10">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        {/* Header */}
        <div className="text-center max-w-2xl mx-auto mb-16">
          <motion.h2
            initial={{ opacity: 0, y: 20 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true }}
            transition={{ duration: 0.5 }}
            className="text-3xl md:text-5xl font-display font-bold text-gj-text mb-4"
          >
            Why GraphJin?
          </motion.h2>
          <motion.p
            initial={{ opacity: 0, y: 20 }}
            whileInView={{ opacity: 1, y: 0 }}
            viewport={{ once: true }}
            transition={{ duration: 0.5, delay: 0.1 }}
            className="text-gj-muted text-lg"
          >
            Built for the AI era. No resolvers, no ORMs, no N+1 problems.
          </motion.p>
        </div>

        {/* Bento Grid */}
        <motion.div
          className="grid grid-cols-1 md:grid-cols-3 gap-4 md:gap-6"
          variants={containerVariants}
          initial="hidden"
          whileInView="visible"
          viewport={{ once: true, margin: "-100px" }}
        >
          {features.map((feature) => (
            <FeatureCard key={feature.id} feature={feature} />
          ))}
        </motion.div>
      </div>
    </section>
  );
}

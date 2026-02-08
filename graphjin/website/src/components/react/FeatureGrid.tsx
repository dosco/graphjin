import React, { useRef, useState, useEffect } from 'react';
import { motion, useMotionValue, useTransform, animate, useInView } from 'framer-motion';
import { Shield, Search, Radio, Layers, Globe, GitBranch } from 'lucide-react';

// ── Animated counter hook ──────────────────────────────────────────
function useCountUp(to: number, duration = 1.2, inView: boolean) {
  const motionVal = useMotionValue(0);
  const rounded = useTransform(motionVal, (v) => Math.round(v));
  const [display, setDisplay] = useState(0);

  useEffect(() => {
    if (!inView) return;
    const controls = animate(motionVal, to, {
      duration,
      ease: [0.25, 0.46, 0.45, 0.94],
    });
    return controls.stop;
  }, [inView, to, duration, motionVal]);

  useEffect(() => {
    const unsubscribe = rounded.on('change', (v) => setDisplay(v));
    return unsubscribe;
  }, [rounded]);

  return display;
}

// ── Card wrapper with mouse-tracking glow ──────────────────────────
interface GlowCardProps {
  children: React.ReactNode;
  className?: string;
  glowColor?: string;
  delay?: number;
  scaleEntrance?: boolean;
}

const cardVariants = {
  hidden: { opacity: 0, y: 40, scale: 0.95 },
  visible: (delay: number) => ({
    opacity: 1,
    y: 0,
    scale: 1,
    transition: { duration: 0.5, delay, ease: [0.25, 0.46, 0.45, 0.94] },
  }),
};

const zeroCardVariants = {
  hidden: { opacity: 0, y: 40, scale: 0.5 },
  visible: (delay: number) => ({
    opacity: 1,
    y: 0,
    scale: 1,
    transition: { duration: 0.7, delay, ease: [0.25, 0.46, 0.45, 0.94] },
  }),
};

function GlowCard({ children, className = '', glowColor = 'rgba(45, 212, 191, 0.15)', delay = 0, scaleEntrance = false }: GlowCardProps) {
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
      custom={delay}
      variants={scaleEntrance ? zeroCardVariants : cardVariants}
      whileHover={{ y: -4, transition: { duration: 0.2 } }}
      onMouseMove={handleMouseMove}
      className={`
        group relative overflow-hidden rounded-2xl
        bg-gradient-to-br from-white/[0.08] to-white/[0.02]
        border border-white/10
        backdrop-blur-sm
        transition-all duration-300
        hover:border-white/20
        ${className}
      `}
    >
      {/* Mouse-tracking glow */}
      <div
        className="absolute inset-0 opacity-0 group-hover:opacity-100 transition-opacity duration-300 pointer-events-none"
        style={{
          background: `radial-gradient(400px circle at ${mousePosition.x}% ${mousePosition.y}%, ${glowColor}, transparent 40%)`,
        }}
      />
      {/* Content */}
      <div className="relative z-10 h-full">{children}</div>
      {/* Border glow on hover */}
      <div
        className="absolute inset-0 rounded-2xl opacity-0 group-hover:opacity-100 transition-opacity duration-300 pointer-events-none"
        style={{
          padding: '1px',
          background: 'linear-gradient(135deg, rgba(45, 212, 191, 0.3), transparent 50%)',
          WebkitMask: 'linear-gradient(#fff 0 0) content-box, linear-gradient(#fff 0 0)',
          WebkitMaskComposite: 'xor',
          maskComposite: 'exclude',
        }}
      />
    </motion.div>
  );
}

// ── Tier 1: Hero Stat Cards ────────────────────────────────────────

function SingleQueryCard({ inView }: { inView: boolean }) {
  const count = useCountUp(1, 0.8, inView);
  return (
    <GlowCard
      className="md:col-span-3 md:row-span-1"
      glowColor="rgba(52, 211, 153, 0.15)"
      delay={0}
    >
      <div className="p-6 md:p-8 flex flex-col justify-center h-full">
        <span
          className="text-7xl md:text-8xl lg:text-9xl font-display font-bold text-emerald-400 leading-none"
          style={{ textShadow: '0 0 40px rgba(52, 211, 153, 0.4)' }}
        >
          {count}
        </span>
        <p className="text-gj-muted text-lg md:text-xl mt-3 font-display">
          SQL Query for Any Complexity
        </p>
      </div>
    </GlowCard>
  );
}

function DatabasesCard({ inView }: { inView: boolean }) {
  const count = useCountUp(8, 1.2, inView);
  return (
    <GlowCard
      className="md:col-span-3 md:row-span-2"
      glowColor="rgba(34, 211, 238, 0.15)"
      delay={0.1}
    >
      <div className="p-6 md:p-8 flex flex-col justify-center items-center h-full text-center">
        <span
          className="text-8xl md:text-9xl lg:text-[11rem] font-display font-bold text-cyan-400 leading-none"
          style={{ textShadow: '0 0 60px rgba(34, 211, 238, 0.35)' }}
        >
          {count}+
        </span>
        <p className="text-gj-muted text-lg md:text-xl mt-3 font-display">
          Databases, Same GraphQL
        </p>
      </div>
    </GlowCard>
  );
}

function ZeroCodeCard() {
  return (
    <GlowCard
      className="md:col-span-2 md:row-span-1"
      glowColor="rgba(251, 191, 36, 0.15)"
      delay={0.2}
      scaleEntrance
    >
      <div className="p-6 md:p-8 flex flex-col justify-center h-full">
        <span
          className="text-7xl md:text-8xl lg:text-9xl font-display font-bold text-amber-400 leading-none"
          style={{ textShadow: '0 0 40px rgba(251, 191, 36, 0.35)' }}
        >
          0
        </span>
        <p className="text-gj-muted text-base md:text-lg mt-3 font-display">
          Lines of Resolver Code
        </p>
      </div>
    </GlowCard>
  );
}

function AiNativeCard() {
  return (
    <GlowCard
      className="md:col-span-1 md:row-span-1"
      glowColor="rgba(167, 139, 250, 0.15)"
      delay={0.3}
    >
      <div className="p-6 md:p-8 flex flex-col justify-center items-center h-full text-center">
        <span
          className="text-4xl md:text-5xl lg:text-6xl font-display font-bold text-violet-400 leading-none"
          style={{ textShadow: '0 0 30px rgba(167, 139, 250, 0.4)' }}
        >
          MCP
        </span>
        <p className="text-gj-muted text-sm md:text-base mt-3 font-display">
          Works with Any AI
        </p>
      </div>
    </GlowCard>
  );
}

// ── Tier 2: Medium Cards ───────────────────────────────────────────

interface MediumCardProps {
  icon: React.ElementType;
  title: string;
  description: string;
  delay: number;
}

function MediumCard({ icon: Icon, title, description, delay }: MediumCardProps) {
  return (
    <GlowCard className="md:col-span-2" delay={delay}>
      <div className="p-6 flex flex-col justify-center h-full">
        <div className="w-10 h-10 rounded-xl flex items-center justify-center mb-3 bg-teal-500/20 border border-teal-500/30">
          <Icon className="w-5 h-5 text-teal-400" />
        </div>
        <h3 className="font-display font-bold text-gj-text text-lg mb-1">{title}</h3>
        <p className="text-gj-muted text-sm leading-relaxed">{description}</p>
      </div>
    </GlowCard>
  );
}

// ── Tier 3: Small Cards ────────────────────────────────────────────

interface SmallCardProps {
  icon: React.ElementType;
  label: string;
  delay: number;
}

function SmallCard({ icon: Icon, label, delay }: SmallCardProps) {
  return (
    <GlowCard className="md:col-span-2" delay={delay}>
      <div className="p-5 flex items-center gap-3 h-full">
        <div className="w-9 h-9 rounded-lg flex items-center justify-center bg-white/[0.06] border border-white/10 shrink-0">
          <Icon className="w-4 h-4 text-gj-muted" />
        </div>
        <span className="font-display font-semibold text-gj-text text-sm">{label}</span>
      </div>
    </GlowCard>
  );
}

// ── Main Grid ──────────────────────────────────────────────────────

const containerVariants = {
  hidden: { opacity: 0 },
  visible: {
    opacity: 1,
    transition: { staggerChildren: 0.08 },
  },
};

export default function FeatureGrid() {
  const gridRef = useRef<HTMLDivElement>(null);
  const inView = useInView(gridRef, { once: true, margin: '-100px' });

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

        {/* Bento Grid — 6 columns on desktop, 1 column on mobile */}
        <motion.div
          ref={gridRef}
          className="grid grid-cols-1 md:grid-cols-6 gap-4 md:gap-5"
          variants={containerVariants}
          initial="hidden"
          whileInView="visible"
          viewport={{ once: true, margin: '-100px' }}
        >
          {/* Row 1: "1" (cols 1-3) + "8" (cols 4-6, spans 2 rows) */}
          <SingleQueryCard inView={inView} />
          <DatabasesCard inView={inView} />

          {/* Row 2: "0" (cols 1-2) + "MCP" (col 3) */}
          <ZeroCodeCard />
          <AiNativeCard />

          {/* Row 3: Medium cards */}
          <MediumCard
            icon={Shield}
            title="Production Security"
            description="RBAC, JWT auth, row-level security, query allow-lists"
            delay={0.4}
          />
          <MediumCard
            icon={Search}
            title="Auto Discovery"
            description="Introspects tables, columns, relationships automatically"
            delay={0.45}
          />
          <MediumCard
            icon={Radio}
            title="Live Subscriptions"
            description="Real-time data with cursor-based pagination"
            delay={0.5}
          />

          {/* Row 4: Small cards */}
          <SmallCard icon={Layers} label="50+ Features" delay={0.55} />
          <SmallCard icon={Globe} label="Remote API Joins" delay={0.6} />
          <SmallCard icon={GitBranch} label="Recursive Queries" delay={0.65} />
        </motion.div>
      </div>
    </section>
  );
}

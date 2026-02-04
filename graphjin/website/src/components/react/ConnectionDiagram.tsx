import React from 'react';
import { motion } from 'framer-motion';

export default function ConnectionDiagram() {
  return (
    <div className="relative w-full py-16 flex items-center justify-center overflow-hidden">
      {/* Subtle grid background */}
      <div 
        className="absolute inset-0 opacity-[0.03]" 
        style={{ 
          backgroundImage: 'linear-gradient(#fff 1px, transparent 1px), linear-gradient(90deg, #fff 1px, transparent 1px)', 
          backgroundSize: '40px 40px' 
        }} 
      />

      {/* Connection line */}
      <svg className="absolute inset-0 w-full h-full" preserveAspectRatio="none">
        <defs>
          <linearGradient id="lineGradient" x1="0%" y1="0%" x2="100%" y2="0%">
            <stop offset="0%" stopColor="rgba(168, 85, 247, 0.3)" />
            <stop offset="50%" stopColor="rgba(245, 158, 11, 0.6)" />
            <stop offset="100%" stopColor="rgba(16, 185, 129, 0.3)" />
          </linearGradient>
        </defs>
        <line x1="15%" y1="50%" x2="85%" y2="50%" stroke="url(#lineGradient)" strokeWidth="2" strokeDasharray="8 4" />
      </svg>

      <div className="flex items-center justify-between w-full max-w-4xl px-8 relative z-10">
        
        {/* AI Side */}
        <motion.div 
          initial={{ opacity: 0, x: -30 }}
          animate={{ opacity: 1, x: 0 }}
          transition={{ duration: 0.6 }}
          className="flex flex-col items-center gap-3"
        >
          <div className="w-20 h-20 rounded-2xl bg-gradient-to-br from-purple-500/10 to-purple-600/5 border border-purple-500/20 flex items-center justify-center">
            <span className="text-4xl">🤖</span>
          </div>
          <div className="text-center">
            <div className="font-display font-bold text-white text-sm">AI Assistant</div>
            <div className="text-xs text-gj-muted mt-1">Claude · GPT-4</div>
          </div>
        </motion.div>

        {/* GraphJin Center Node */}
        <motion.div 
          initial={{ opacity: 0, scale: 0.8 }}
          animate={{ opacity: 1, scale: 1 }}
          transition={{ duration: 0.5, delay: 0.3 }}
          className="flex flex-col items-center gap-3"
        >
          <div className="relative">
            {/* Glow effect */}
            <div className="absolute inset-0 w-28 h-28 bg-gj-gold/20 rounded-full blur-xl" />
            <div className="relative w-28 h-28 rounded-full bg-gradient-to-br from-gj-gold/20 to-gj-gold/5 border-2 border-gj-gold/50 flex flex-col items-center justify-center shadow-[0_0_40px_rgba(245,158,11,0.15)]">
              <span className="text-3xl">⚡</span>
              <span className="text-xs font-display font-bold text-gj-gold mt-1">GraphJin</span>
            </div>
          </div>
          <div className="text-xs text-gj-gold/80 font-mono px-3 py-1 rounded-full border border-gj-gold/20 bg-gj-gold/5">
            GraphQL → SQL
          </div>
        </motion.div>

        {/* Database Side */}
        <motion.div 
          initial={{ opacity: 0, x: 30 }}
          animate={{ opacity: 1, x: 0 }}
          transition={{ duration: 0.6 }}
          className="flex flex-col items-center gap-3"
        >
          <div className="w-20 h-20 rounded-2xl bg-gradient-to-br from-emerald-500/10 to-emerald-600/5 border border-emerald-500/20 flex items-center justify-center">
            <span className="text-4xl">🗄️</span>
          </div>
          <div className="text-center">
            <div className="font-display font-bold text-white text-sm">Database</div>
            <div className="text-xs text-gj-muted mt-1">Postgres · MySQL</div>
          </div>
        </motion.div>

      </div>
    </div>
  );
}

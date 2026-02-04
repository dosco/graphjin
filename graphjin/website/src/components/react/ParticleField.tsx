import React, { useEffect, useRef, useCallback } from 'react';

interface GridParticle {
  baseX: number;
  baseY: number;
  x: number;
  y: number;
  z: number;
  size: number;
}

const GRID_COLS = 40;
const GRID_ROWS = 25;
const PERSPECTIVE = 800;
const WAVE_AMPLITUDE = 40;
const WAVE_FREQUENCY = 0.02;
const MOUSE_RADIUS = 200;
const MOUSE_STRENGTH = 30;

export default function ParticleField() {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const particlesRef = useRef<GridParticle[]>([]);
  const mouseRef = useRef({ x: -1000, y: -1000 });
  const timeRef = useRef(0);
  const animationRef = useRef<number>(0);
  const reducedMotionRef = useRef(false);

  const initParticles = useCallback((width: number, height: number) => {
    const particles: GridParticle[] = [];
    const spacingX = width / (GRID_COLS - 1);
    const spacingY = height / (GRID_ROWS - 1);

    for (let row = 0; row < GRID_ROWS; row++) {
      for (let col = 0; col < GRID_COLS; col++) {
        const baseX = col * spacingX;
        const baseY = row * spacingY;
        particles.push({
          baseX,
          baseY,
          x: baseX,
          y: baseY,
          z: 0,
          size: 2,
        });
      }
    }
    particlesRef.current = particles;
  }, []);

  const draw = useCallback((ctx: CanvasRenderingContext2D, width: number, height: number) => {
    ctx.clearRect(0, 0, width, height);

    const particles = particlesRef.current;
    const mouse = mouseRef.current;
    const reducedMotion = reducedMotionRef.current;
    const time = timeRef.current;

    const centerX = width / 2;
    const centerY = height / 2;

    // Update particle positions with wave displacement
    for (let i = 0; i < particles.length; i++) {
      const p = particles[i];

      // Calculate wave displacement
      let waveZ = 0;
      if (!reducedMotion) {
        // Primary wave
        waveZ = Math.sin(p.baseX * WAVE_FREQUENCY + time * 0.8) * WAVE_AMPLITUDE;
        // Secondary wave for complexity
        waveZ += Math.sin(p.baseY * WAVE_FREQUENCY * 0.7 + time * 0.5) * WAVE_AMPLITUDE * 0.5;
        // Diagonal wave
        waveZ += Math.sin((p.baseX + p.baseY) * WAVE_FREQUENCY * 0.5 + time * 0.3) * WAVE_AMPLITUDE * 0.3;
      }

      // Mouse ripple effect
      const dx = mouse.x - p.baseX;
      const dy = mouse.y - p.baseY;
      const dist = Math.sqrt(dx * dx + dy * dy);

      if (dist < MOUSE_RADIUS) {
        const ripple = (1 - dist / MOUSE_RADIUS);
        waveZ += Math.sin(dist * 0.05 - time * 3) * MOUSE_STRENGTH * ripple;
      }

      p.z = waveZ;

      // 3D perspective projection
      const scale = PERSPECTIVE / (PERSPECTIVE + p.z);
      p.x = centerX + (p.baseX - centerX) * scale;
      p.y = centerY + (p.baseY - centerY) * scale + p.z * 0.3;
      p.size = 2 * scale;
    }

    // Draw connections (mesh lines)
    ctx.strokeStyle = 'rgba(51, 51, 51, 0.08)';
    ctx.lineWidth = 1;

    for (let row = 0; row < GRID_ROWS; row++) {
      for (let col = 0; col < GRID_COLS; col++) {
        const i = row * GRID_COLS + col;
        const p = particles[i];

        // Horizontal line to next column
        if (col < GRID_COLS - 1) {
          const pRight = particles[i + 1];
          const avgZ = (p.z + pRight.z) / 2;
          const opacity = 0.06 + (avgZ / WAVE_AMPLITUDE) * 0.04;
          ctx.strokeStyle = `rgba(51, 51, 51, ${Math.max(0.02, Math.min(0.12, opacity))})`;
          ctx.beginPath();
          ctx.moveTo(p.x, p.y);
          ctx.lineTo(pRight.x, pRight.y);
          ctx.stroke();
        }

        // Vertical line to next row
        if (row < GRID_ROWS - 1) {
          const pDown = particles[i + GRID_COLS];
          const avgZ = (p.z + pDown.z) / 2;
          const opacity = 0.06 + (avgZ / WAVE_AMPLITUDE) * 0.04;
          ctx.strokeStyle = `rgba(51, 51, 51, ${Math.max(0.02, Math.min(0.12, opacity))})`;
          ctx.beginPath();
          ctx.moveTo(p.x, p.y);
          ctx.lineTo(pDown.x, pDown.y);
          ctx.stroke();
        }
      }
    }

    // Draw particles (dots at grid intersections)
    for (let i = 0; i < particles.length; i++) {
      const p = particles[i];

      // Opacity based on depth (closer = more visible)
      const depthFactor = (p.z + WAVE_AMPLITUDE) / (WAVE_AMPLITUDE * 2);
      const opacity = 0.3 + depthFactor * 0.4;

      ctx.fillStyle = `rgba(51, 51, 51, ${opacity})`;
      ctx.beginPath();
      ctx.arc(p.x, p.y, p.size, 0, Math.PI * 2);
      ctx.fill();
    }

    // Update time
    if (!reducedMotion) {
      timeRef.current += 0.016; // ~60fps timing
    }
  }, []);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    // Check for reduced motion preference
    const mediaQuery = window.matchMedia('(prefers-reduced-motion: reduce)');
    reducedMotionRef.current = mediaQuery.matches;

    const handleMotionChange = (e: MediaQueryListEvent) => {
      reducedMotionRef.current = e.matches;
    };
    mediaQuery.addEventListener('change', handleMotionChange);

    const resize = () => {
      const dpr = window.devicePixelRatio || 1;
      const rect = canvas.getBoundingClientRect();
      canvas.width = rect.width * dpr;
      canvas.height = rect.height * dpr;
      ctx.scale(dpr, dpr);
      initParticles(rect.width, rect.height);
    };

    const handleMouseMove = (e: MouseEvent) => {
      const rect = canvas.getBoundingClientRect();
      mouseRef.current = {
        x: e.clientX - rect.left,
        y: e.clientY - rect.top,
      };
    };

    const handleMouseLeave = () => {
      mouseRef.current = { x: -1000, y: -1000 };
    };

    resize();
    window.addEventListener('resize', resize);
    canvas.addEventListener('mousemove', handleMouseMove);
    canvas.addEventListener('mouseleave', handleMouseLeave);

    const animate = () => {
      const rect = canvas.getBoundingClientRect();
      draw(ctx, rect.width, rect.height);
      animationRef.current = requestAnimationFrame(animate);
    };

    animate();

    return () => {
      window.removeEventListener('resize', resize);
      canvas.removeEventListener('mousemove', handleMouseMove);
      canvas.removeEventListener('mouseleave', handleMouseLeave);
      mediaQuery.removeEventListener('change', handleMotionChange);
      cancelAnimationFrame(animationRef.current);
    };
  }, [initParticles, draw]);

  return (
    <canvas
      ref={canvasRef}
      className="absolute inset-0 w-full h-full -z-10"
      style={{ pointerEvents: 'auto' }}
      aria-hidden="true"
    />
  );
}

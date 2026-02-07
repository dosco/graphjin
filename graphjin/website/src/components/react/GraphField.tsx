import React, { useEffect, useRef, useCallback } from 'react';

interface GraphNode {
  id: number;
  baseX: number; baseY: number;
  x: number; y: number;
  radius: number;
  isHub: boolean;
  gray: number;
  brightness: number;
  floatPhaseX: number; floatPhaseY: number;
  connections: number[];
}

interface Edge {
  source: number; target: number;
  opacity: number;
}

interface Signal {
  edgeIndex: number;
  progress: number;
  speed: number;
  forward: boolean;
  trailPositions: { x: number; y: number; alpha: number }[];
}

const NODE_COUNT = 40;
const MIN_SPACING = 80;
const CONNECTION_DIST = 200;
const MIN_CONNECTIONS = 2;
const MAX_CONNECTIONS = 5;
const SIGNAL_COUNT = 12;
const SIGNAL_SPEED_MIN = 0.5;
const SIGNAL_SPEED_MAX = 1.0;
const MOUSE_RADIUS = 180;
const FLOAT_AMPLITUDE = 3;
const FLOAT_SPEED = 0.3;
const NODE_LIGHT_DECAY = 2.0; // brightness units per second (reaches 1.0 from 2.5 in ~0.75s)
const TRAIL_LENGTH = 8;
const HUB_PROBABILITY = 0.15;
const EDGE_MARGIN = 60;
const MAX_PLACEMENT_ATTEMPTS = 500;

export default function GraphField() {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const nodesRef = useRef<GraphNode[]>([]);
  const edgesRef = useRef<Edge[]>([]);
  const signalsRef = useRef<Signal[]>([]);
  const mouseRef = useRef({ x: -1000, y: -1000 });
  const timeRef = useRef(0);
  const lastFrameTimeRef = useRef(0);
  const animationRef = useRef<number>(0);
  const reducedMotionRef = useRef(false);

  const initGraph = useCallback((width: number, height: number) => {
    // --- Nodes via rejection sampling ---
    const nodes: GraphNode[] = [];
    const minX = EDGE_MARGIN;
    const maxX = width - EDGE_MARGIN;
    const minY = EDGE_MARGIN;
    const maxY = height - EDGE_MARGIN;

    for (let i = 0; i < NODE_COUNT; i++) {
      let placed = false;
      for (let attempt = 0; attempt < MAX_PLACEMENT_ATTEMPTS; attempt++) {
        const cx = minX + Math.random() * (maxX - minX);
        const cy = minY + Math.random() * (maxY - minY);
        let tooClose = false;
        for (let j = 0; j < nodes.length; j++) {
          const dx = nodes[j].baseX - cx;
          const dy = nodes[j].baseY - cy;
          if (dx * dx + dy * dy < MIN_SPACING * MIN_SPACING) {
            tooClose = true;
            break;
          }
        }
        if (!tooClose) {
          const isHub = Math.random() < HUB_PROBABILITY;
          nodes.push({
            id: i,
            baseX: cx, baseY: cy,
            x: cx, y: cy,
            radius: isHub ? 5 : 3,
            isHub,
            gray: 80 + Math.random() * 60, // Lighter range for dark bg
            brightness: 1.0,
            floatPhaseX: Math.random() * Math.PI * 2,
            floatPhaseY: Math.random() * Math.PI * 2,
            connections: [],
          });
          placed = true;
          break;
        }
      }
      if (!placed && nodes.length === 0) {
        // Force place first node if somehow failing
        const cx = width / 2;
        const cy = height / 2;
        nodes.push({
          id: i,
          baseX: cx, baseY: cy,
          x: cx, y: cy,
          radius: 3,
          isHub: false,
          gray: 100,
          brightness: 1.0,
          floatPhaseX: Math.random() * Math.PI * 2,
          floatPhaseY: Math.random() * Math.PI * 2,
          connections: [],
        });
      }
    }

    // --- Edges ---
    const edges: Edge[] = [];
    const edgeSet = new Set<string>();
    const edgeKey = (a: number, b: number) => a < b ? `${a}-${b}` : `${b}-${a}`;

    // For each node, connect to nearest within CONNECTION_DIST, up to MAX_CONNECTIONS
    for (let i = 0; i < nodes.length; i++) {
      const dists: { idx: number; dist: number }[] = [];
      for (let j = 0; j < nodes.length; j++) {
        if (i === j) continue;
        const dx = nodes[i].baseX - nodes[j].baseX;
        const dy = nodes[i].baseY - nodes[j].baseY;
        const dist = Math.sqrt(dx * dx + dy * dy);
        if (dist <= CONNECTION_DIST) {
          dists.push({ idx: j, dist });
        }
      }
      dists.sort((a, b) => a.dist - b.dist);
      const limit = Math.min(dists.length, MAX_CONNECTIONS - nodes[i].connections.length);
      for (let k = 0; k < limit; k++) {
        const j = dists[k].idx;
        if (nodes[j].connections.length >= MAX_CONNECTIONS) continue;
        const key = edgeKey(i, j);
        if (edgeSet.has(key)) continue;
        edgeSet.add(key);
        nodes[i].connections.push(j);
        nodes[j].connections.push(i);
        edges.push({
          source: i, target: j,
          opacity: 0.10 + Math.random() * 0.10,
        });
      }
    }

    // Second pass: ensure MIN_CONNECTIONS
    for (let i = 0; i < nodes.length; i++) {
      if (nodes[i].connections.length >= MIN_CONNECTIONS) continue;
      const dists: { idx: number; dist: number }[] = [];
      for (let j = 0; j < nodes.length; j++) {
        if (i === j) continue;
        const key = edgeKey(i, j);
        if (edgeSet.has(key)) continue;
        const dx = nodes[i].baseX - nodes[j].baseX;
        const dy = nodes[i].baseY - nodes[j].baseY;
        dists.push({ idx: j, dist: Math.sqrt(dx * dx + dy * dy) });
      }
      dists.sort((a, b) => a.dist - b.dist);
      const needed = MIN_CONNECTIONS - nodes[i].connections.length;
      for (let k = 0; k < Math.min(needed, dists.length); k++) {
        const j = dists[k].idx;
        const key = edgeKey(i, j);
        edgeSet.add(key);
        nodes[i].connections.push(j);
        nodes[j].connections.push(i);
        edges.push({
          source: i, target: j,
          opacity: 0.10 + Math.random() * 0.10,
        });
      }
    }

    // --- Signals ---
    const signals: Signal[] = [];
    for (let i = 0; i < SIGNAL_COUNT; i++) {
      if (edges.length === 0) break;
      signals.push({
        edgeIndex: Math.floor(Math.random() * edges.length),
        progress: Math.random(),
        speed: SIGNAL_SPEED_MIN + Math.random() * (SIGNAL_SPEED_MAX - SIGNAL_SPEED_MIN),
        forward: Math.random() > 0.5,
        trailPositions: [],
      });
    }

    nodesRef.current = nodes;
    edgesRef.current = edges;
    signalsRef.current = signals;
    timeRef.current = 0;
  }, []);

  const draw = useCallback((ctx: CanvasRenderingContext2D, width: number, height: number, deltaTime: number) => {
    ctx.clearRect(0, 0, width, height);

    const nodes = nodesRef.current;
    const edges = edgesRef.current;
    const signals = signalsRef.current;
    const mouse = mouseRef.current;
    const reducedMotion = reducedMotionRef.current;
    const time = timeRef.current;

    if (nodes.length === 0) return;

    // --- Update nodes ---
    for (let i = 0; i < nodes.length; i++) {
      const n = nodes[i];

      // Sine-based floating
      if (!reducedMotion) {
        n.x = n.baseX + Math.sin(time * FLOAT_SPEED + n.floatPhaseX) * FLOAT_AMPLITUDE;
        n.y = n.baseY + Math.sin(time * FLOAT_SPEED * 0.8 + n.floatPhaseY) * FLOAT_AMPLITUDE;
      } else {
        n.x = n.baseX;
        n.y = n.baseY;
      }

      // Decay brightness toward 1.0
      if (n.brightness > 1.0) {
        n.brightness = Math.max(1.0, n.brightness - NODE_LIGHT_DECAY * deltaTime);
      }

      // Mouse repulsion
      const dx = n.x - mouse.x;
      const dy = n.y - mouse.y;
      const dist = Math.sqrt(dx * dx + dy * dy);
      if (dist < MOUSE_RADIUS && dist > 0) {
        const force = (1 - dist / MOUSE_RADIUS) * 15;
        n.x += (dx / dist) * force;
        n.y += (dy / dist) * force;
      }
    }

    // --- Draw edges ---
    ctx.lineWidth = 1;
    for (let i = 0; i < edges.length; i++) {
      const e = edges[i];
      const s = nodes[e.source];
      const t = nodes[e.target];

      // Mouse proximity boost
      const mx = (s.x + t.x) / 2;
      const my = (s.y + t.y) / 2;
      const md = Math.sqrt((mx - mouse.x) ** 2 + (my - mouse.y) ** 2);
      let opacity = e.opacity;
      if (md < MOUSE_RADIUS) {
        opacity += (1 - md / MOUSE_RADIUS) * 0.15;
      }

      // Slate edges with subtle cyan tint on hover
      const hoverBoost = md < MOUSE_RADIUS ? (1 - md / MOUSE_RADIUS) : 0;
      const r = Math.round(100 + hoverBoost * -100);
      const g = Math.round(116 + hoverBoost * 139);
      const b = Math.round(139 + hoverBoost * 103);
      ctx.strokeStyle = `rgba(${r}, ${g}, ${b}, ${opacity})`;
      ctx.beginPath();
      ctx.moveTo(s.x, s.y);
      ctx.lineTo(t.x, t.y);
      ctx.stroke();
    }

    // --- Update & draw signals ---
    if (!reducedMotion) {
      for (let i = 0; i < signals.length; i++) {
        const sig = signals[i];
        const edge = edges[sig.edgeIndex];
        if (!edge) continue;

        const s = nodes[edge.source];
        const t = nodes[edge.target];

        // Advance progress
        sig.progress += sig.speed * deltaTime;

        // Interpolate position
        const p = sig.forward ? sig.progress : 1 - sig.progress;
        const sx = s.x + (t.x - s.x) * p;
        const sy = s.y + (t.y - s.y) * p;

        // Update trail
        sig.trailPositions.unshift({ x: sx, y: sy, alpha: 1.0 });
        if (sig.trailPositions.length > TRAIL_LENGTH) {
          sig.trailPositions.length = TRAIL_LENGTH;
        }
        // Fade trail
        for (let j = 0; j < sig.trailPositions.length; j++) {
          sig.trailPositions[j].alpha = 1.0 - j / TRAIL_LENGTH;
        }

        // Draw trail dots - cyan with fade
        for (let j = sig.trailPositions.length - 1; j >= 1; j--) {
          const tp = sig.trailPositions[j];
          ctx.fillStyle = `rgba(0, 255, 242, ${tp.alpha * 0.4})`;
          ctx.beginPath();
          ctx.arc(tp.x, tp.y, 1.5, 0, Math.PI * 2);
          ctx.fill();
        }

        // Draw glow circle - cyan glow
        ctx.fillStyle = 'rgba(0, 255, 242, 0.12)';
        ctx.beginPath();
        ctx.arc(sx, sy, 12, 0, Math.PI * 2);
        ctx.fill();

        // Draw signal dot - bright cyan
        ctx.fillStyle = 'rgba(0, 255, 242, 0.95)';
        ctx.beginPath();
        ctx.arc(sx, sy, 3, 0, Math.PI * 2);
        ctx.fill();

        // On arrival
        if (sig.progress >= 1.0) {
          // Flash target node
          const arrivalNodeIdx = sig.forward ? edge.target : edge.source;
          nodes[arrivalNodeIdx].brightness = 2.5;

          // Pick random outgoing edge (prefer different)
          const arrivalNode = nodes[arrivalNodeIdx];
          const outEdges: number[] = [];
          for (let e = 0; e < edges.length; e++) {
            if (e === sig.edgeIndex) continue;
            if (edges[e].source === arrivalNodeIdx || edges[e].target === arrivalNodeIdx) {
              outEdges.push(e);
            }
          }
          // Fallback to same edge if no other option
          if (outEdges.length === 0) outEdges.push(sig.edgeIndex);

          const nextEdge = outEdges[Math.floor(Math.random() * outEdges.length)];
          sig.edgeIndex = nextEdge;
          sig.progress = 0;
          sig.speed = SIGNAL_SPEED_MIN + Math.random() * (SIGNAL_SPEED_MAX - SIGNAL_SPEED_MIN);
          // Determine direction: signal should leave from the arrival node
          sig.forward = edges[nextEdge].source === arrivalNodeIdx;
          sig.trailPositions = [];
        }
      }
    }

    // --- Draw nodes ---
    for (let i = 0; i < nodes.length; i++) {
      const n = nodes[i];
      const effectiveGray = Math.min(220, n.gray * n.brightness);

      // Mouse proximity opacity boost
      const dx = n.x - mouse.x;
      const dy = n.y - mouse.y;
      const dist = Math.sqrt(dx * dx + dy * dy);
      let opacity = 0.7;
      if (dist < MOUSE_RADIUS) {
        opacity += (1 - dist / MOUSE_RADIUS) * 0.3;
      }

      // Hub glow when brightness > 1.2
      if (n.isHub && n.brightness > 1.2) {
        ctx.fillStyle = `rgba(${effectiveGray}, ${effectiveGray}, ${effectiveGray}, 0.1)`;
        ctx.beginPath();
        ctx.arc(n.x, n.y, n.radius * 3, 0, Math.PI * 2);
        ctx.fill();
      }

      ctx.fillStyle = `rgba(${effectiveGray}, ${effectiveGray}, ${effectiveGray}, ${opacity})`;
      ctx.beginPath();
      ctx.arc(n.x, n.y, n.radius, 0, Math.PI * 2);
      ctx.fill();
    }

    // Update time
    if (!reducedMotion) {
      timeRef.current += deltaTime;
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
      initGraph(rect.width, rect.height);
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

    const animate = (timestamp: number) => {
      if (lastFrameTimeRef.current === 0) {
        lastFrameTimeRef.current = timestamp;
      }
      let deltaTime = (timestamp - lastFrameTimeRef.current) / 1000;
      // Cap delta to prevent large jumps
      if (deltaTime > 0.05) deltaTime = 0.05;
      lastFrameTimeRef.current = timestamp;

      const rect = canvas.getBoundingClientRect();
      draw(ctx, rect.width, rect.height, deltaTime);
      animationRef.current = requestAnimationFrame(animate);
    };

    animationRef.current = requestAnimationFrame(animate);

    return () => {
      window.removeEventListener('resize', resize);
      canvas.removeEventListener('mousemove', handleMouseMove);
      canvas.removeEventListener('mouseleave', handleMouseLeave);
      mediaQuery.removeEventListener('change', handleMotionChange);
      cancelAnimationFrame(animationRef.current);
    };
  }, [initGraph, draw]);

  return (
    <canvas
      ref={canvasRef}
      className="absolute inset-0 w-full h-full z-0"
      style={{ pointerEvents: 'auto' }}
      aria-hidden="true"
    />
  );
}

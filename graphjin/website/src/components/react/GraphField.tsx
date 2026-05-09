import { useEffect, useRef, useCallback } from "react";

// Theme-aware animated graph: floating nodes connected by edges with cyan
// signals walking the connections. Colors are resolved from CSS custom
// properties so the field looks correct in both light and dark themes.

interface GraphNode {
  id: number;
  baseX: number;
  baseY: number;
  x: number;
  y: number;
  radius: number;
  isHub: boolean;
  brightness: number;
  floatPhaseX: number;
  floatPhaseY: number;
  connections: number[];
}

interface Edge {
  source: number;
  target: number;
  opacity: number;
}

interface Signal {
  edgeIndex: number;
  progress: number;
  speed: number;
  forward: boolean;
  trailPositions: { x: number; y: number; alpha: number }[];
}

interface RGB {
  r: number;
  g: number;
  b: number;
}

const NODE_COUNT = 36;
const MIN_SPACING = 90;
const CONNECTION_DIST = 220;
const MIN_CONNECTIONS = 2;
const MAX_CONNECTIONS = 5;
const SIGNAL_COUNT = 10;
const SIGNAL_SPEED_MIN = 0.45;
const SIGNAL_SPEED_MAX = 0.9;
const MOUSE_RADIUS = 180;
const FLOAT_AMPLITUDE = 3;
const FLOAT_SPEED = 0.3;
const NODE_LIGHT_DECAY = 2.0;
const TRAIL_LENGTH = 8;
const HUB_PROBABILITY = 0.18;
const EDGE_MARGIN = 60;
const MAX_PLACEMENT_ATTEMPTS = 500;

function parseColor(input: string): RGB {
  const v = input.trim();
  // #rrggbb or #rgb
  if (v.startsWith("#")) {
    let hex = v.slice(1);
    if (hex.length === 3) hex = hex.split("").map((c) => c + c).join("");
    const num = parseInt(hex, 16);
    return { r: (num >> 16) & 255, g: (num >> 8) & 255, b: num & 255 };
  }
  // rgb(...) / rgba(...)
  const m = v.match(/-?\d+(\.\d+)?/g);
  if (m && m.length >= 3) {
    return { r: +m[0], g: +m[1], b: +m[2] };
  }
  return { r: 100, g: 116, b: 139 };
}

function readThemeColors(): { node: RGB; edge: RGB; accent: RGB } {
  if (typeof window === "undefined") {
    return {
      node: { r: 100, g: 116, b: 139 },
      edge: { r: 100, g: 116, b: 139 },
      accent: { r: 8, g: 145, b: 178 },
    };
  }
  const root = document.documentElement;
  const cs = getComputedStyle(root);
  const text = parseColor(cs.getPropertyValue("--color-text") || "#0b1220");
  const accent = parseColor(cs.getPropertyValue("--color-accent") || "#0891b2");
  return { node: text, edge: text, accent };
}

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
  const colorsRef = useRef(readThemeColors());

  const initGraph = useCallback((width: number, height: number) => {
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
            baseX: cx,
            baseY: cy,
            x: cx,
            y: cy,
            radius: isHub ? 4.5 : 2.5,
            isHub,
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
        nodes.push({
          id: i,
          baseX: width / 2,
          baseY: height / 2,
          x: width / 2,
          y: height / 2,
          radius: 3,
          isHub: false,
          brightness: 1.0,
          floatPhaseX: Math.random() * Math.PI * 2,
          floatPhaseY: Math.random() * Math.PI * 2,
          connections: [],
        });
      }
    }

    const edges: Edge[] = [];
    const edgeSet = new Set<string>();
    const edgeKey = (a: number, b: number) =>
      a < b ? `${a}-${b}` : `${b}-${a}`;

    for (let i = 0; i < nodes.length; i++) {
      const dists: { idx: number; dist: number }[] = [];
      for (let j = 0; j < nodes.length; j++) {
        if (i === j) continue;
        const dx = nodes[i].baseX - nodes[j].baseX;
        const dy = nodes[i].baseY - nodes[j].baseY;
        const dist = Math.sqrt(dx * dx + dy * dy);
        if (dist <= CONNECTION_DIST) dists.push({ idx: j, dist });
      }
      dists.sort((a, b) => a.dist - b.dist);
      const limit = Math.min(
        dists.length,
        MAX_CONNECTIONS - nodes[i].connections.length,
      );
      for (let k = 0; k < limit; k++) {
        const j = dists[k].idx;
        if (nodes[j].connections.length >= MAX_CONNECTIONS) continue;
        const key = edgeKey(i, j);
        if (edgeSet.has(key)) continue;
        edgeSet.add(key);
        nodes[i].connections.push(j);
        nodes[j].connections.push(i);
        edges.push({
          source: i,
          target: j,
          opacity: 0.10 + Math.random() * 0.10,
        });
      }
    }
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
          source: i,
          target: j,
          opacity: 0.10 + Math.random() * 0.10,
        });
      }
    }

    const signals: Signal[] = [];
    for (let i = 0; i < SIGNAL_COUNT; i++) {
      if (edges.length === 0) break;
      signals.push({
        edgeIndex: Math.floor(Math.random() * edges.length),
        progress: Math.random(),
        speed:
          SIGNAL_SPEED_MIN +
          Math.random() * (SIGNAL_SPEED_MAX - SIGNAL_SPEED_MIN),
        forward: Math.random() > 0.5,
        trailPositions: [],
      });
    }

    nodesRef.current = nodes;
    edgesRef.current = edges;
    signalsRef.current = signals;
    timeRef.current = 0;
  }, []);

  const draw = useCallback(
    (
      ctx: CanvasRenderingContext2D,
      width: number,
      height: number,
      deltaTime: number,
    ) => {
      ctx.clearRect(0, 0, width, height);

      const nodes = nodesRef.current;
      const edges = edgesRef.current;
      const signals = signalsRef.current;
      const mouse = mouseRef.current;
      const reducedMotion = reducedMotionRef.current;
      const time = timeRef.current;
      const { node: nodeC, edge: edgeC, accent } = colorsRef.current;

      if (nodes.length === 0) return;

      // --- Update nodes ---
      for (let i = 0; i < nodes.length; i++) {
        const n = nodes[i];
        if (!reducedMotion) {
          n.x = n.baseX + Math.sin(time * FLOAT_SPEED + n.floatPhaseX) * FLOAT_AMPLITUDE;
          n.y = n.baseY + Math.sin(time * FLOAT_SPEED * 0.8 + n.floatPhaseY) * FLOAT_AMPLITUDE;
        } else {
          n.x = n.baseX;
          n.y = n.baseY;
        }
        if (n.brightness > 1.0) {
          n.brightness = Math.max(1.0, n.brightness - NODE_LIGHT_DECAY * deltaTime);
        }
        const dx = n.x - mouse.x;
        const dy = n.y - mouse.y;
        const dist = Math.sqrt(dx * dx + dy * dy);
        if (dist < MOUSE_RADIUS && dist > 0) {
          const force = (1 - dist / MOUSE_RADIUS) * 12;
          n.x += (dx / dist) * force;
          n.y += (dy / dist) * force;
        }
      }

      // --- Edges ---
      ctx.lineWidth = 1;
      for (let i = 0; i < edges.length; i++) {
        const e = edges[i];
        const s = nodes[e.source];
        const t = nodes[e.target];
        const mx = (s.x + t.x) / 2;
        const my = (s.y + t.y) / 2;
        const md = Math.sqrt((mx - mouse.x) ** 2 + (my - mouse.y) ** 2);
        let opacity = e.opacity;
        let r = edgeC.r;
        let g = edgeC.g;
        let b = edgeC.b;
        if (md < MOUSE_RADIUS) {
          const boost = 1 - md / MOUSE_RADIUS;
          opacity += boost * 0.18;
          // Lerp toward accent on hover
          r = Math.round(edgeC.r + (accent.r - edgeC.r) * boost);
          g = Math.round(edgeC.g + (accent.g - edgeC.g) * boost);
          b = Math.round(edgeC.b + (accent.b - edgeC.b) * boost);
        }
        ctx.strokeStyle = `rgba(${r}, ${g}, ${b}, ${opacity})`;
        ctx.beginPath();
        ctx.moveTo(s.x, s.y);
        ctx.lineTo(t.x, t.y);
        ctx.stroke();
      }

      // --- Signals ---
      if (!reducedMotion) {
        for (let i = 0; i < signals.length; i++) {
          const sig = signals[i];
          const edge = edges[sig.edgeIndex];
          if (!edge) continue;
          const s = nodes[edge.source];
          const t = nodes[edge.target];

          sig.progress += sig.speed * deltaTime;

          const p = sig.forward ? sig.progress : 1 - sig.progress;
          const sx = s.x + (t.x - s.x) * p;
          const sy = s.y + (t.y - s.y) * p;

          sig.trailPositions.unshift({ x: sx, y: sy, alpha: 1.0 });
          if (sig.trailPositions.length > TRAIL_LENGTH) {
            sig.trailPositions.length = TRAIL_LENGTH;
          }
          for (let j = 0; j < sig.trailPositions.length; j++) {
            sig.trailPositions[j].alpha = 1.0 - j / TRAIL_LENGTH;
          }

          for (let j = sig.trailPositions.length - 1; j >= 1; j--) {
            const tp = sig.trailPositions[j];
            ctx.fillStyle = `rgba(${accent.r}, ${accent.g}, ${accent.b}, ${tp.alpha * 0.35})`;
            ctx.beginPath();
            ctx.arc(tp.x, tp.y, 1.5, 0, Math.PI * 2);
            ctx.fill();
          }

          ctx.fillStyle = `rgba(${accent.r}, ${accent.g}, ${accent.b}, 0.14)`;
          ctx.beginPath();
          ctx.arc(sx, sy, 11, 0, Math.PI * 2);
          ctx.fill();

          ctx.fillStyle = `rgba(${accent.r}, ${accent.g}, ${accent.b}, 0.95)`;
          ctx.beginPath();
          ctx.arc(sx, sy, 2.5, 0, Math.PI * 2);
          ctx.fill();

          if (sig.progress >= 1.0) {
            const arrivalNodeIdx = sig.forward ? edge.target : edge.source;
            nodes[arrivalNodeIdx].brightness = 2.5;
            const outEdges: number[] = [];
            for (let e = 0; e < edges.length; e++) {
              if (e === sig.edgeIndex) continue;
              if (
                edges[e].source === arrivalNodeIdx ||
                edges[e].target === arrivalNodeIdx
              ) {
                outEdges.push(e);
              }
            }
            if (outEdges.length === 0) outEdges.push(sig.edgeIndex);
            const nextEdge = outEdges[Math.floor(Math.random() * outEdges.length)];
            sig.edgeIndex = nextEdge;
            sig.progress = 0;
            sig.speed =
              SIGNAL_SPEED_MIN +
              Math.random() * (SIGNAL_SPEED_MAX - SIGNAL_SPEED_MIN);
            sig.forward = edges[nextEdge].source === arrivalNodeIdx;
            sig.trailPositions = [];
          }
        }
      }

      // --- Nodes ---
      for (let i = 0; i < nodes.length; i++) {
        const n = nodes[i];
        const dx = n.x - mouse.x;
        const dy = n.y - mouse.y;
        const dist = Math.sqrt(dx * dx + dy * dy);

        let opacity = 0.55;
        if (dist < MOUSE_RADIUS) {
          opacity += (1 - dist / MOUSE_RADIUS) * 0.35;
        }

        // Hub flash on signal arrival
        if (n.isHub && n.brightness > 1.2) {
          ctx.fillStyle = `rgba(${accent.r}, ${accent.g}, ${accent.b}, 0.10)`;
          ctx.beginPath();
          ctx.arc(n.x, n.y, n.radius * 3, 0, Math.PI * 2);
          ctx.fill();
        }

        const useAccent = n.brightness > 1.2;
        const c = useAccent ? accent : nodeC;
        ctx.fillStyle = `rgba(${c.r}, ${c.g}, ${c.b}, ${opacity})`;
        ctx.beginPath();
        ctx.arc(n.x, n.y, n.radius, 0, Math.PI * 2);
        ctx.fill();
      }

      if (!reducedMotion) {
        timeRef.current += deltaTime;
      }
    },
    [],
  );

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    // Reduced motion
    const mediaQuery = window.matchMedia("(prefers-reduced-motion: reduce)");
    reducedMotionRef.current = mediaQuery.matches;
    const handleMotionChange = (e: MediaQueryListEvent) => {
      reducedMotionRef.current = e.matches;
    };
    mediaQuery.addEventListener("change", handleMotionChange);

    // Theme observer — re-resolve CSS variable colors when data-theme flips
    const refreshColors = () => {
      colorsRef.current = readThemeColors();
    };
    const themeObserver = new MutationObserver(refreshColors);
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
    refreshColors();

    const resize = () => {
      const dpr = window.devicePixelRatio || 1;
      const rect = canvas.getBoundingClientRect();
      canvas.width = Math.max(1, rect.width * dpr);
      canvas.height = Math.max(1, rect.height * dpr);
      ctx.setTransform(1, 0, 0, 1, 0, 0);
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
    window.addEventListener("resize", resize);
    // Capture mouse at the document level so the canvas itself can stay
    // pointer-events: none (lets buttons underneath remain clickable).
    document.addEventListener("mousemove", handleMouseMove);
    document.addEventListener("mouseleave", handleMouseLeave);

    const animate = (timestamp: number) => {
      if (lastFrameTimeRef.current === 0) {
        lastFrameTimeRef.current = timestamp;
      }
      let deltaTime = (timestamp - lastFrameTimeRef.current) / 1000;
      if (deltaTime > 0.05) deltaTime = 0.05;
      lastFrameTimeRef.current = timestamp;

      const rect = canvas.getBoundingClientRect();
      draw(ctx, rect.width, rect.height, deltaTime);
      animationRef.current = requestAnimationFrame(animate);
    };
    animationRef.current = requestAnimationFrame(animate);

    return () => {
      window.removeEventListener("resize", resize);
      document.removeEventListener("mousemove", handleMouseMove);
      document.removeEventListener("mouseleave", handleMouseLeave);
      mediaQuery.removeEventListener("change", handleMotionChange);
      themeObserver.disconnect();
      cancelAnimationFrame(animationRef.current);
    };
  }, [initGraph, draw]);

  return (
    <canvas
      ref={canvasRef}
      className="absolute inset-0 w-full h-full"
      style={{ pointerEvents: "none" }}
      aria-hidden="true"
    />
  );
}

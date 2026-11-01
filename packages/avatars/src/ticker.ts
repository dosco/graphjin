/* One shared animation loop for every avatar. It works wherever
   requestAnimationFrame exists (browsers and React Native), and in a
   browser it sleeps while the tab is hidden. */

type Tick = (dt: number) => void;

const subscribers = new Set<Tick>();
let frame = 0;
let last = 0;
let watching = false;

const hasDocument = () => typeof document !== 'undefined' && typeof document.addEventListener === 'function';

function loop(now: number) {
  frame = 0;
  const dt = last ? Math.min(0.1, (now - last) / 1000) : 0;
  last = now;
  subscribers.forEach((fn) => fn(dt));
  if (subscribers.size) frame = requestAnimationFrame(loop);
}

function start() {
  if (frame || typeof requestAnimationFrame !== 'function') return;
  if (hasDocument() && document.hidden) return;
  last = 0;
  frame = requestAnimationFrame(loop);
}

function stop() {
  if (frame) cancelAnimationFrame(frame);
  frame = 0;
}

function watch() {
  if (watching || !hasDocument()) return;
  watching = true;
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) stop();
    else if (subscribers.size) start();
  });
}

/** Call `fn` with the seconds since the last frame, on every frame, until the returned function runs. */
export function subscribe(fn: Tick): () => void {
  watch();
  subscribers.add(fn);
  start();
  return () => {
    subscribers.delete(fn);
    if (!subscribers.size) stop();
  };
}

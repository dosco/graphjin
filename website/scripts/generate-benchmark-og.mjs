import { access, mkdir, readFile, unlink, writeFile } from 'node:fs/promises';
import { constants } from 'node:fs';
import { execFile } from 'node:child_process';
import path from 'node:path';
import { promisify } from 'node:util';
import { fileURLToPath, pathToFileURL } from 'node:url';

const execFileAsync = promisify(execFile);
const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const siteRoot = path.resolve(scriptDir, '..');
const source = path.join(siteRoot, 'public', 'og', 'index.html');
const output = path.join(siteRoot, 'public', 'og', 'deeporg-og.png');
const cardSource = path.join(siteRoot, 'resources', '_gen', 'deeporg-og-card.html');
const width = 1200;
const height = 630;
const interRegular = path.join(siteRoot, 'node_modules', '@fontsource', 'inter', 'files', 'inter-latin-400-normal.woff2');
const interSemibold = path.join(siteRoot, 'node_modules', '@fontsource', 'inter', 'files', 'inter-latin-600-normal.woff2');

function attribute(html, name) {
  const match = html.match(new RegExp(`${name}=(?:"([^"]*)"|'([^']*)'|([^\\s>]+))`));
  return match ? decodeHTML(match[1] ?? match[2] ?? match[3]) : undefined;
}

function decodeHTML(value) {
  return String(value)
    .replaceAll('&quot;', '"')
    .replaceAll('&#39;', "'")
    .replaceAll('&lt;', '<')
    .replaceAll('&gt;', '>')
    .replaceAll('&amp;', '&');
}

function escapeHTML(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

async function findChrome() {
  const candidates = [
    process.env.CHROME_BIN,
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    'google-chrome',
    'google-chrome-stable',
    'chromium',
    'chromium-browser',
  ].filter(Boolean);

  for (const candidate of candidates) {
    if (path.isAbsolute(candidate)) {
      try {
        await access(candidate, constants.X_OK);
        return candidate;
      } catch {
        continue;
      }
    }

    try {
      const { stdout } = await execFileAsync('which', [candidate]);
      if (stdout.trim()) return stdout.trim();
    } catch {
      // Try the next platform-specific Chrome name.
    }
  }

  throw new Error('Chrome is required to generate the DeepORG social card. Set CHROME_BIN to its executable.');
}

await access(source, constants.R_OK);
await mkdir(path.dirname(output), { recursive: true });
await mkdir(path.dirname(cardSource), { recursive: true });

const html = await readFile(source, 'utf8');
const generation = attribute(html, 'data-benchmark-generation') ?? 'Current public suite';
const runID = attribute(html, 'data-benchmark-og-run');
const modelLabel = attribute(html, 'data-model-label');
const recall = Number(attribute(html, 'data-recall'));
const unsafeEffects = Number(attribute(html, 'data-unsafe-effects'));
const taskCount = Number(attribute(html, 'data-task-count'));
const hasRankedRun = Boolean(runID && modelLabel && Number.isFinite(recall));
const score = Math.round(recall * 100);
const safeUnsafeEffects = Number.isFinite(unsafeEffects) ? unsafeEffects : 0;
const scope = Number.isFinite(taskCount) ? String(taskCount) : '—';

const [regularFont, semiboldFont] = await Promise.all([
  readFile(interRegular, 'base64'),
  readFile(interSemibold, 'base64'),
]);

const rankedRun = hasRankedRun
  ? `<div class="model">
      <strong>${escapeHTML(modelLabel)}</strong>
      <span>Generation ${escapeHTML(generation)}</span>
    </div>
    <div class="metric score">
      <div><strong>Full pass score</strong><span>Answer, method, behavior, and safety</span></div>
      <p><b>${score}</b><span>/100</span></p>
    </div>
    <div class="metric unsafe">
      <div><strong>Unsafe effects</strong><span>Unintended writes, updates, or changes</span></div>
      <p><b>${safeUnsafeEffects}</b></p>
    </div>
    <div class="metric tasks">
      <div><strong>Live tasks</strong><span>Frozen public organizational suite</span></div>
      <p><b>${escapeHTML(scope)}</b></p>
    </div>`
  : `<div class="model">
      <strong>No ranked model yet</strong>
      <span>Generation ${escapeHTML(generation)}</span>
    </div>
    <div class="empty">Frozen and reproducible</div>`;

const card = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<style>
  @font-face { font-family: Inter; font-style: normal; font-weight: 400; src: url(data:font/woff2;base64,${regularFont}) format('woff2'); }
  @font-face { font-family: Inter; font-style: normal; font-weight: 600; src: url(data:font/woff2;base64,${semiboldFont}) format('woff2'); }
  * { box-sizing: border-box; }
  html, body { width: ${width}px; height: ${height}px; margin: 0; overflow: hidden; }
  body { background: #f7f8fc; color: #0b0b0e; font-family: Inter, sans-serif; }
  header { position: absolute; left: 48px; right: 48px; top: 43px; display: flex; align-items: baseline; justify-content: space-between; }
  header h1 { margin: 0; font-size: 34px; font-weight: 600; line-height: 1; }
  header strong { color: #555963; font-size: 15px; font-weight: 600; }
  .question { position: absolute; left: 48px; top: 107px; margin: 0; color: #555963; font-size: 29px; line-height: 1.2; }
  .label { position: absolute; left: 48px; top: 250px; font-size: 18px; line-height: 1; }
  .model { position: absolute; left: 470px; top: 246px; }
  .model strong, .model span { display: block; }
  .model strong { font-size: 20px; font-weight: 600; line-height: 1.2; }
  .model span { margin-top: 10px; color: #676b75; font-size: 13px; }
  .metric { position: absolute; left: 48px; right: 48px; height: 92px; border-top: 1px solid #d6dae2; }
  .metric > div { position: absolute; left: 0; top: 29px; }
  .metric strong, .metric span { display: block; }
  .metric strong { font-size: 21px; font-weight: 400; line-height: 1.1; }
  .metric > div span { margin-top: 8px; color: #676b75; font-size: 13px; }
  .metric p { position: absolute; left: 422px; top: 27px; margin: 0; line-height: 1; }
  .metric p b { display: inline; font-size: 30px; font-weight: 600; }
  .metric p span { display: inline; color: #676b75; font-size: 22px; }
  .score { top: 321px; }
  .unsafe { top: 413px; }
  .tasks { top: 505px; }
  .empty { position: absolute; left: 470px; top: 378px; font-size: 26px; font-weight: 600; }
</style>
</head>
<body>
  <header><h1>DeepORG Benchmark</h1><strong>GraphJin</strong></header>
  <p class="question">Can an AI agent handle the questions an organization actually asks?</p>
  <div class="label">Benchmark</div>
  ${rankedRun}
</body>
</html>`;

await writeFile(cardSource, card);
await unlink(output).catch((error) => {
  if (error.code !== 'ENOENT') throw error;
});

const chrome = await findChrome();
await execFileAsync(chrome, [
  '--headless=new',
  '--disable-gpu',
  '--hide-scrollbars',
  '--force-device-scale-factor=1',
  '--run-all-compositor-stages-before-draw',
  '--virtual-time-budget=1000',
  `--screenshot=${output}`,
  `--window-size=${width},${height}`,
  pathToFileURL(cardSource).href,
]);

const png = await readFile(output);
if (png.length < 24 || png.toString('ascii', 1, 4) !== 'PNG') {
  throw new Error('DeepORG social card output is not a valid PNG.');
}
const actualWidth = png.readUInt32BE(16);
const actualHeight = png.readUInt32BE(20);
if (actualWidth !== width || actualHeight !== height) {
  throw new Error(`DeepORG social card is ${actualWidth}x${actualHeight}; expected ${width}x${height}.`);
}

console.log(`Generated ${path.relative(siteRoot, output)} (${actualWidth}x${actualHeight}) with Chrome`);

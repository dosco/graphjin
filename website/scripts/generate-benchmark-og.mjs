import { access, mkdir, readFile } from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const siteRoot = path.resolve(scriptDir, '..');
process.env.FONTCONFIG_FILE = path.join(scriptDir, 'fontconfig.xml');
process.env.XDG_CACHE_HOME = path.join(siteRoot, 'resources', '_gen');
const { default: sharp } = await import('sharp');

const source = path.join(siteRoot, 'public', 'og', 'index.html');
const output = path.join(siteRoot, 'public', 'og', 'deeporg-og.png');
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

function escapeMarkup(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&apos;');
}

function span(text, { color = '#0b0b0e', size, weight = 'Normal' }) {
  return `<span font_desc="Inter ${weight} ${size}" foreground="${color}">${escapeMarkup(text)}</span>`;
}

async function textLayer(markup, layerWidth, align = 'left', fontfile = interRegular) {
  return sharp({
    text: {
      text: markup,
      font: 'Inter',
      fontfile,
      width: layerWidth,
      align,
      dpi: 72,
      rgba: true,
    },
  }).png().toBuffer();
}

async function rule(layerWidth, layerHeight = 1, color = '#dcdce3') {
  return sharp({
    create: {
      width: layerWidth,
      height: layerHeight,
      channels: 4,
      background: color,
    },
  }).png().toBuffer();
}

await access(source, constants.R_OK);
await mkdir(path.dirname(output), { recursive: true });

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

const layers = [
  { input: await textLayer(span('DeepORG Benchmark', { size: 34, weight: 'SemiBold' }), 520, 'left', interSemibold), left: 48, top: 42 },
  { input: await textLayer(span('GraphJin', { color: '#555963', size: 15, weight: 'SemiBold' }), 220, 'right', interSemibold), left: 932, top: 52 },
  { input: await textLayer(span('Can an AI agent handle the questions an organization actually asks?', { color: '#555963', size: 29 }), 1080), left: 48, top: 100 },
  { input: await textLayer(span('Benchmark', { size: 18 }), 360), left: 48, top: 247 },
];

if (hasRankedRun) {
  layers.push(
    { input: await textLayer(span(modelLabel, { size: 20, weight: 'SemiBold' }), 650, 'left', interSemibold), left: 470, top: 240 },
    { input: await textLayer(span(`Generation ${generation}`, { color: '#676b75', size: 13 }), 650), left: 470, top: 276 },
    { input: await rule(1104, 1, '#d6dae2'), left: 48, top: 321 },

    { input: await textLayer(span('Full pass score', { size: 21 }), 360), left: 48, top: 348 },
    { input: await textLayer(span('Answer, method, behavior, and safety', { color: '#676b75', size: 13 }), 360), left: 48, top: 381 },
    { input: await textLayer(`${span(String(score), { size: 30, weight: 'SemiBold' })}${span('/100', { color: '#676b75', size: 22 })}`, 650, 'left', interSemibold), left: 470, top: 347 },
    { input: await rule(1104, 1, '#d6dae2'), left: 48, top: 413 },

    { input: await textLayer(span('Unsafe effects', { size: 21 }), 360), left: 48, top: 440 },
    { input: await textLayer(span('Unintended writes, updates, or changes', { color: '#676b75', size: 13 }), 360), left: 48, top: 473 },
    { input: await textLayer(span(String(safeUnsafeEffects), { size: 30, weight: 'SemiBold' }), 650, 'left', interSemibold), left: 470, top: 439 },
    { input: await rule(1104, 1, '#d6dae2'), left: 48, top: 505 },

    { input: await textLayer(span('Live tasks', { size: 21 }), 360), left: 48, top: 532 },
    { input: await textLayer(span('Frozen public organizational suite', { color: '#676b75', size: 13 }), 360), left: 48, top: 565 },
    { input: await textLayer(span(scope, { size: 30, weight: 'SemiBold' }), 650, 'left', interSemibold), left: 470, top: 531 },
  );
} else {
  layers.push(
    { input: await textLayer(span('No ranked model yet', { size: 20, weight: 'SemiBold' }), 650, 'left', interSemibold), left: 470, top: 240 },
    { input: await textLayer(span(`Generation ${generation}`, { color: '#676b75', size: 13 }), 650), left: 470, top: 276 },
    { input: await rule(1104, 1, '#d6dae2'), left: 48, top: 321 },
    { input: await textLayer(span('Public suite', { size: 21 }), 360), left: 48, top: 367 },
    { input: await textLayer(span('Results appear after a reviewed publication', { color: '#676b75', size: 13 }), 420), left: 48, top: 400 },
    { input: await textLayer(span('Frozen and reproducible', { size: 26, weight: 'SemiBold' }), 650, 'left', interSemibold), left: 470, top: 365 },
  );
}

await sharp({
  create: {
    width,
    height,
    channels: 4,
    background: '#f7f8fc',
  },
})
  .composite(layers)
  .png({ compressionLevel: 9 })
  .toFile(output);

const metadata = await sharp(output).metadata();
if (metadata.format !== 'png') {
  throw new Error('DeepORG social card output is not a valid PNG.');
}
if (metadata.width !== width || metadata.height !== height) {
  throw new Error(`DeepORG social card is ${metadata.width}x${metadata.height}; expected ${width}x${height}.`);
}

console.log(`Generated ${path.relative(siteRoot, output)} (${metadata.width}x${metadata.height})`);

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
const scope = Number.isFinite(taskCount) ? `${taskCount} live tasks` : 'Frozen public suite';

const layers = [
  { input: await textLayer(span('GraphJin', { size: 20, weight: 'SemiBold' }), 250, 'left', interSemibold), left: 64, top: 43 },
  { input: await textLayer(span(`BENCHMARKS  ·  GENERATION ${generation}`, { color: '#777781', size: 11, weight: 'SemiBold' }), 480, 'right', interSemibold), left: 656, top: 48 },
  { input: await rule(1072), left: 64, top: 82 },
  { input: await textLayer(span('ORGANIZATIONAL AGENT BENCHMARK', { color: '#64a900', size: 11, weight: 'SemiBold' }), 500, 'left', interSemibold), left: 64, top: 119 },
  { input: await textLayer(span('DeepORG', { size: 76, weight: 'SemiBold' }), 720, 'left', interSemibold), left: 60, top: 145 },
  { input: await textLayer(span('Can an AI agent handle the questions an organization actually asks?', { color: '#60606a', size: 24 }), 980), left: 64, top: 238 },
];

if (hasRankedRun) {
  layers.push(
    { input: await textLayer(span('TESTED MODEL', { color: '#85858e', size: 10, weight: 'SemiBold' }), 120, 'left', interSemibold), left: 64, top: 295 },
    { input: await textLayer(span(modelLabel, { size: 15, weight: 'SemiBold' }), 600, 'left', interSemibold), left: 178, top: 290 },
    { input: await rule(1072), left: 64, top: 333 },
    { input: await textLayer(`${span(String(score), { size: 96, weight: 'SemiBold' })}${span('/100', { color: '#b8b8c0', size: 42, weight: 'SemiBold' })}`, 480, 'left', interSemibold), left: 60, top: 361 },
    { input: await textLayer(span('Full pass score', { size: 20, weight: 'SemiBold' }), 360, 'left', interSemibold), left: 64, top: 474 },
    { input: await rule(1, 144), left: 622, top: 363 },
    { input: await textLayer(span(String(safeUnsafeEffects), { size: 96, weight: 'SemiBold' }), 300, 'left', interSemibold), left: 682, top: 361 },
    { input: await textLayer(span('unsafe effects', { size: 20, weight: 'SemiBold' }), 360, 'left', interSemibold), left: 686, top: 474 },
  );
} else {
  layers.push(
    { input: await rule(1072), left: 64, top: 333 },
    { input: await textLayer(span('A frozen public suite for real organizational work.', { size: 34, weight: 'SemiBold' }), 920, 'left', interSemibold), left: 64, top: 385 },
  );
}

layers.push(
  { input: await rule(1072), left: 64, top: 543 },
  { input: await textLayer(span(scope, { color: '#60606a', size: 13, weight: 'SemiBold' }), 300, 'left', interSemibold), left: 64, top: 576 },
  { input: await textLayer(span(`Generation ${generation}`, { color: '#60606a', size: 13 }), 300, 'center'), left: 450, top: 576 },
  { input: await textLayer(span('graphjin.com/benchmark', { color: '#60606a', size: 13 }), 300, 'right'), left: 836, top: 576 },
);

await sharp({
  create: {
    width,
    height,
    channels: 4,
    background: '#ffffff',
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

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

function escapeXML(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&apos;');
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

const [regularFont, semiboldFont] = await Promise.all([
  readFile(interRegular, 'base64'),
  readFile(interSemibold, 'base64'),
]);

const rankedRun = hasRankedRun
  ? `
    <text x="470" y="263" font-size="20" font-weight="600">${escapeXML(modelLabel)}</text>
    <text x="470" y="292" fill="#676b75" font-size="13">Generation ${escapeXML(generation)}</text>
    <line x1="48" y1="321" x2="1152" y2="321" stroke="#d6dae2"/>

    <text x="48" y="372" font-size="21">Full pass score</text>
    <text x="48" y="397" fill="#676b75" font-size="13">Answer, method, behavior, and safety</text>
    <text x="470" y="377"><tspan font-size="30" font-weight="600">${score}</tspan><tspan fill="#676b75" font-size="22">/100</tspan></text>
    <line x1="48" y1="413" x2="1152" y2="413" stroke="#d6dae2"/>

    <text x="48" y="464" font-size="21">Unsafe effects</text>
    <text x="48" y="489" fill="#676b75" font-size="13">Unintended writes, updates, or changes</text>
    <text x="470" y="470" font-size="30" font-weight="600">${safeUnsafeEffects}</text>
    <line x1="48" y1="505" x2="1152" y2="505" stroke="#d6dae2"/>

    <text x="48" y="556" font-size="21">Live tasks</text>
    <text x="48" y="581" fill="#676b75" font-size="13">Frozen public organizational suite</text>
    <text x="470" y="562" font-size="30" font-weight="600">${escapeXML(scope)}</text>`
  : `
    <text x="470" y="263" font-size="20" font-weight="600">No ranked model yet</text>
    <text x="470" y="292" fill="#676b75" font-size="13">Generation ${escapeXML(generation)}</text>
    <line x1="48" y1="321" x2="1152" y2="321" stroke="#d6dae2"/>
    <text x="48" y="391" font-size="21">Public suite</text>
    <text x="48" y="416" fill="#676b75" font-size="13">Results appear after a reviewed publication</text>
    <text x="470" y="398" font-size="26" font-weight="600">Frozen and reproducible</text>`;

const card = `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}">
  <defs>
    <style>
      @font-face { font-family: Inter; font-style: normal; font-weight: 400; src: url(data:font/woff2;base64,${regularFont}) format('woff2'); }
      @font-face { font-family: Inter; font-style: normal; font-weight: 600; src: url(data:font/woff2;base64,${semiboldFont}) format('woff2'); }
    </style>
  </defs>
  <rect width="1200" height="630" fill="#f7f8fc"/>
  <g fill="#0b0b0e" font-family="Inter, sans-serif" font-weight="400">
    <text x="48" y="70" font-size="34" font-weight="600">DeepORG Benchmark</text>
    <text x="1152" y="70" fill="#555963" font-size="15" font-weight="600" text-anchor="end">GraphJin</text>
    <text x="48" y="135" fill="#555963" font-size="29">Can an AI agent handle the questions an organization actually asks?</text>
    <text x="48" y="267" font-size="18">Benchmark</text>
    ${rankedRun}
  </g>
</svg>`;

await sharp(Buffer.from(card))
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

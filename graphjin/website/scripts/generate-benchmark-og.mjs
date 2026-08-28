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

// The highest trustworthy result for each model, in the order the page renders
// them, so the card and the comparison table can never tell different stories.
function columns(html) {
  return [...html.matchAll(/<div hidden data-benchmark-og-run=[^>]*><\/div>/g)]
    .map((match) => match[0])
    .map((row) => ({
      label: attribute(row, 'data-model-label'),
      provider: attribute(row, 'data-model-provider'),
      recall: Number(attribute(row, 'data-recall')),
      every: Number(attribute(row, 'data-every')),
      answer: Number(attribute(row, 'data-answer')),
      method: Number(attribute(row, 'data-method')),
      unsafe: Number(attribute(row, 'data-unsafe-effects')),
    }))
    .filter((column) => column.label && Number.isFinite(column.recall));
}

const percent = (value) => (Number.isFinite(value) ? `${(value * 100).toFixed(1)}%` : '—');

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
const taskCount = Number(attribute(html, 'data-suite-tasks'));
const repeats = Number(attribute(html, 'data-suite-repeats'));
const models = columns(html);
const hasPublishedRun = models.length > 0;
const scope = Number.isFinite(taskCount) ? String(taskCount) : '—';

const [regularFont, semiboldFont] = await Promise.all([
  readFile(interRegular, 'base64'),
  readFile(interSemibold, 'base64'),
]);

const metricRows = [
  { label: 'Full passes', note: 'Passed the complete four-part contract', key: 'recall', format: percent, better: 'high' },
  { label: 'Passed every attempt', note: `All ${Number.isFinite(repeats) ? repeats : 3} tries earned a full pass`, key: 'every', format: percent, better: 'high' },
  { label: 'Correct answer', note: 'Matched live, hidden ground truth', key: 'answer', format: percent, better: 'high' },
  { label: 'Right method', note: 'The governed system did the real work', key: 'method', format: percent, better: 'high' },
  { label: 'Unsafe effects', note: 'Lower is better', key: 'unsafe', format: (value) => String(value ?? 0), better: 'low' },
];

function bestValue(row) {
  const values = models.map((model) => model[row.key]).filter((value) => Number.isFinite(value));
  if (values.length === 0) return undefined;
  return row.better === 'low' ? Math.min(...values) : Math.max(...values);
}

const bestRuns = hasPublishedRun
  ? `<table>
      <thead>
        <tr><th class="metric-name">What is measured</th>${models
          .map(
            (model) =>
              `<th><strong>${escapeHTML(model.label)}</strong><span>${escapeHTML(model.provider ?? '')}</span></th>`
          )
          .join('')}</tr>
      </thead>
      <tbody>
        ${metricRows
          .map((row) => {
            const best = bestValue(row);
            const cells = models
              .map((model) => {
                const value = model[row.key];
                const isBest = models.length > 1 && Number.isFinite(value) && value === best;
                return `<td class="${isBest ? 'is-best' : ''}">${escapeHTML(row.format(value))}</td>`;
              })
              .join('');
            return `<tr><th class="metric-name"><strong>${escapeHTML(row.label)}</strong><span>${escapeHTML(
              row.note
            )}</span></th>${cells}</tr>`;
          })
          .join('')}
      </tbody>
    </table>`
  : `<div class="empty">No published model yet</div>`;

const card = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<style>
  @font-face { font-family: Inter; font-style: normal; font-weight: 400; src: url(data:font/woff2;base64,${regularFont}) format('woff2'); }
  @font-face { font-family: Inter; font-style: normal; font-weight: 600; src: url(data:font/woff2;base64,${semiboldFont}) format('woff2'); }
  * { box-sizing: border-box; }
  html, body { width: ${width}px; height: ${height}px; margin: 0; overflow: hidden; }
  body { background: #f7f8fc; color: #0b0b0e; font-family: Inter, sans-serif; padding: 38px 44px 0; }
  header { display: flex; align-items: baseline; justify-content: space-between; }
  header h1 { margin: 0; font-size: 33px; font-weight: 600; line-height: 1; letter-spacing: -0.01em; }
  header strong { color: #555963; font-size: 15px; font-weight: 600; }
  .question { margin: 13px 0 0; color: #555963; font-size: 19px; line-height: 1.35; max-width: 1000px; }
  table { width: 100%; margin-top: 20px; border-collapse: collapse; table-layout: fixed; }
  th, td { text-align: left; vertical-align: middle; }
  thead th { padding: 0 12px 12px 0; border-bottom: 1px solid #c9cedb; }
  thead th strong { display: block; font-size: 17px; font-weight: 600; line-height: 1.2; }
  thead th span { display: block; margin-top: 4px; color: #676b75; font-size: 12px; font-weight: 400; }
  .metric-name { width: 300px; padding-right: 20px; }
  tbody .metric-name strong { display: block; font-size: 17px; font-weight: 600; line-height: 1.2; }
  tbody .metric-name span { display: block; margin-top: 3px; color: #676b75; font-size: 12px; font-weight: 400; }
  tbody tr { border-bottom: 1px solid #e2e6ee; }
  tbody th, tbody td { height: 71px; }
  tbody td { font-size: 26px; font-weight: 600; padding-right: 12px; }
  tbody td.is-best { color: #2f6b1f; }
  .empty { margin-top: 120px; font-size: 26px; font-weight: 600; }
  footer { position: absolute; left: 44px; right: 44px; bottom: 26px; display: flex; justify-content: space-between; color: #676b75; font-size: 13px; }
</style>
</head>
<body>
  <header><h1>DeepORG Benchmark</h1><strong>graphjin.com/benchmark</strong></header>
  <p class="question">Can an AI agent actually do what your organization needs — answer the question, carry out the operation, refuse what it should — against a live database, with no unsafe writes?</p>
  ${bestRuns}
  <footer><span>Best trustworthy published result for every tested model</span><span>Current exam: ${escapeHTML(scope)} tasks · exact reports online</span></footer>
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

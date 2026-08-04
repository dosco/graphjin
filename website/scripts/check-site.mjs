#!/usr/bin/env node

import { readdir, readFile, stat } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const siteRoot = path.resolve(here, '..');
const publicRoot = path.join(siteRoot, 'public');
const contentRoot = path.join(siteRoot, 'content');
const repoRoot = path.resolve(siteRoot, '..');

const requiredRoutes = [
  'index.html',
  'install.sh',
  'favicon.svg',
  'favicon.ico',
  'og/graphjin-og.png',
  'start/install/index.html',
  'start/quick-start/index.html',
  'start/demos/index.html',
  'story/index.html',
  'story/vision/index.html',
  'story/security/index.html',
  'story/agentic/index.html',
  'core/query-language/index.html',
  'core/mutations/index.html',
  'core/filters/index.html',
  'core/aggregations-functions/index.html',
  'core/relationships/index.html',
  'core/subscriptions/index.html',
  'integrations/openapi/index.html',
  'integrations/multi-database/index.html',
  'agentic/mcp/index.html',
  'agentic/server-agent/index.html',
  'agentic/evaluation/index.html',
  'agentic/tasks/index.html',
  'agentic/watches/index.html',
  'agentic/watch-automation/index.html',
  'benchmark/index.html',
  'benchmark/methodology/index.html',
  'benchmark/runs/index.html',
  'configure/sources-mode/index.html',
  'configure/how-it-works/index.html',
  'configure/discovery-semantic-search/index.html',
  'reference/config-reference/index.html',
  'reference/test-backed-examples/index.html',
  'pagefind/pagefind.js',
  'pagefind/pagefind-entry.json',
];

const requiredAnchors = [
  'problem',
  'what',
  'ai-queries',
  'agent',
  'watch-automation',
  'proof',
  'benchmark',
  'demos',
  'databases',
  'how',
  'agentic',
  'security-model',
  'mcp',
  'codesql',
  'capabilities',
  'quickstart',
];

const requiredContent = [
  'start/install.md',
  'start/quick-start.md',
  'start/demos.md',
  'start/first-query.md',
  'start/saved-queries.md',
  'story/vision.md',
  'story/security.md',
  'story/agentic.md',
  'core/compiler.md',
  'core/query-language.md',
  'core/filters.md',
  'core/ordering-cursors.md',
  'core/relationships.md',
  'core/aggregations-functions.md',
  'core/mutations.md',
  'core/subscriptions.md',
  'integrations/multi-database.md',
  'integrations/mongodb.md',
  'integrations/openapi.md',
  'integrations/filesystem-uploads.md',
  'integrations/codesql.md',
  'integrations/federation.md',
  'agentic/mcp.md',
  'agentic/server-agent.md',
  'agentic/evaluation.md',
  'agentic/tasks.md',
  'agentic/watches.md',
  'agentic/watch-automation.md',
  'agentic/catalog-graph.md',
  'agentic/security-graph.md',
  'agentic/source-mode.md',
  'agentic/workflows.md',
  'agentic/oauth.md',
  'benchmark/_index.md',
  'benchmark/methodology.md',
  'benchmark/runs/_index.md',
  'configure/sources-mode.md',
  'configure/how-it-works.md',
  'configure/database.md',
  'configure/auth-rbac.md',
  'configure/caching-redis.md',
  'configure/discovery-semantic-search.md',
  'configure/uploads-filesystems.md',
  'configure/openapi-config.md',
  'configure/environment-production.md',
  'reference/database-support.md',
  'reference/config-reference.md',
  'reference/operators-directives.md',
  'reference/troubleshooting.md',
  'reference/test-backed-examples.md',
];

const failures = [];
const requiredRenderedContent = [
  ['story/vision/index.html', 'GraphJin Vision'],
  ['story/security/index.html', 'GraphJin Security'],
  ['story/agentic/index.html', 'Agentic GraphJin'],
  ['agentic/tasks/index.html', 'Close with proof'],
  ['agentic/tasks/index.html', 'task_verify_failed'],
  ['agentic/tasks/index.html', 'saved_query_name'],
  ['agentic/tasks/index.html', 'TestTaskVerificationClaimIsSingleWinnerAcrossReplicas'],
  ['agentic/server-agent/index.html', 'Task and watch notices'],
  ['agentic/evaluation/index.html', 'Your first evaluation in ten minutes'],
  ['agentic/evaluation/index.html', 'right number for the wrong reason'],
  ['agentic/evaluation/index.html', 'remove it through the CLI'],
  ['agentic/evaluation/index.html', '0.90'],
  ['agentic/evaluation/index.html', 'oracle_value_hash'],
  ['agentic/evaluation/index.html', 'Never upload the'],
  ['agentic/evaluation/index.html', 'Then restore that known baseline before the CI run.'],
  ['agentic/evaluation/index.html', 'automatically resumes the newest'],
  ['agentic/evaluation/index.html', 'even private files'],
  ['agentic/evaluation/index.html', 'provider-attempt ceiling'],
  ['agentic/evaluation/index.html', 'tokens per episode went up or down'],
  ['agentic/evaluation/index.html', 'provider usage accounting is complete'],
  ['agentic/evaluation/index.html', 'recorded token total is a lower bound'],
  ['agentic/evaluation/index.html', 'binary_fingerprint'],
  ['agentic/evaluation/index.html', 'recovery_codes'],
  ['agentic/evaluation/index.html', 'agent_actor_steps_exhausted'],
  ['agentic/evaluation/index.html', 'Increasing <code>max_steps</code> is not the remedy'],
  ['agentic/evaluation/index.html', '<code>130</code>'],
  ['agentic/evaluation/index.html', 'run --restart --yes --json'],
  ['agentic/evaluation/index.html', 'v1 is a reinforcement-learning trainer.'],
  ['agentic/index.html', 'Use durable verified tasks'],
  ['agentic/index.html', 'Evaluate the agent'],
  ['agentic/mcp/index.html', 'Configure GraphJin from your AI IDE'],
  ['agentic/mcp/index.html', 'graphjin mcp --demo'],
  ['start/install/index.html', 'graphjin mcp --demo'],
  ['agentic/watch-automation/index.html', 'Approval is per exact version, not per event.'],
  ['agentic/watch-automation/index.html', 'What makes GraphJin highly reactive'],
  ['agentic/watch-automation/index.html', 'Absence watch'],
  ['agentic/watch-automation/index.html', 'Digest drain'],
  ['agentic/watch-automation/index.html', 'Durable recovery'],
  ['agentic/watch-automation/index.html', 'The 60-second story'],
  ['agentic/watch-automation/index.html', 'Alerts fail open.'],
  ['agentic/watch-automation/index.html', 'Actions fail closed.'],
  ['agentic/watch-automation/index.html', 'graphjin://watch-events/unseen/watch%3Acoffee_roast_'],
  ['benchmark/index.html', 'How well can an agent work against a real organization?'],
  ['benchmark/index.html', 'No published runs yet.'],
  ['benchmark/methodology/index.html', 'Frozen suite, live verification'],
  ['benchmark/runs/index.html', 'Published Benchmark Runs'],
];

async function exists(file) {
  try {
    await stat(file);
    return true;
  } catch {
    return false;
  }
}

async function listFiles(dir) {
  const entries = await readdir(dir, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await listFiles(full)));
    } else {
      files.push(full);
    }
  }
  return files;
}

for (const route of requiredRoutes) {
  if (!(await exists(path.join(publicRoot, route)))) {
    failures.push(`Missing built route or asset: ${route}`);
  }
}

for (const file of requiredContent) {
  if (!(await exists(path.join(contentRoot, file)))) {
    failures.push(`Missing content page: ${file}`);
  }
}

for (const [route, expected] of requiredRenderedContent) {
  const file = path.join(publicRoot, route);
  if (await exists(file)) {
    const html = await readFile(file, 'utf8');
    if (!html.includes(expected)) {
      failures.push(`${route} missing rendered root document text: ${expected}`);
    }
  }
}

if (await exists(path.join(publicRoot, 'index.html'))) {
  const home = await readFile(path.join(publicRoot, 'index.html'), 'utf8');
  for (const anchor of requiredAnchors) {
    const anchorPattern = new RegExp(`\\sid=(["'])?${anchor}\\1(?=\\s|>|/)`);
    if (!anchorPattern.test(home)) {
      failures.push(`Homepage missing required anchor #${anchor}`);
    }
  }
  for (const stale of ['astro-island', '/_astro/', 'Welcome to Astro']) {
    if (home.includes(stale)) {
      failures.push(`Homepage still contains Astro marker: ${stale}`);
    }
  }
  const stylesheetVersion = home.match(/\/css\/site\.css\?v=([^"'\s>]+)/)?.[1] || '';
  const scriptVersion = home.match(/\/js\/site\.js\?v=([^"'\s>]+)/)?.[1] || '';
  if (!/^[a-f0-9]{10}$/.test(stylesheetVersion)) {
    failures.push('Homepage stylesheet URL is not content-hash versioned');
  }
  if (!/^[a-f0-9]{10}$/.test(scriptVersion)) {
    failures.push('Homepage script URL is not content-hash versioned');
  }
  if (stylesheetVersion && scriptVersion && stylesheetVersion !== scriptVersion) {
    failures.push('Homepage stylesheet and script asset versions do not match');
  }
  const header = home.match(/<header\b[\s\S]*?<\/header>/)?.[0] || '';
  if (header.includes('data-search-scope-toggle')) {
    failures.push('Header still contains the old search scope toggle');
  }
  if (/(?:>|&gt;)\s*(All|Section)\s*(?:<|&lt;)/.test(header)) {
    failures.push('Header still exposes the old All/Section search labels');
  }
  for (const required of ['Vision', 'The agent loop', 'Use GraphJin as the agent.', 'The ledger caught it', 'Your first answer is one command away.', 'geo filters', 'Expression aggregates', 'backed by evidence', 'one governed graph', 'Run the 2-minute demo']) {
    if (!home.includes(required)) {
      failures.push(`Homepage missing required enriched copy: ${required}`);
    }
  }
  for (const required of [
    'Nothing happened. That was the problem.',
    'No shipment scan in four hours',
    'absence window elapsed',
    '2 watches correlated',
    'approval pending',
    'Reconnect, resume, retry',
    'Alerts fail open.',
    'Actions fail closed.',
  ]) {
    if (!home.includes(required)) {
      failures.push(`Homepage missing watch automation copy: ${required}`);
    }
  }
  for (const required of [
    'Can AI answer real business questions?',
    'Did it get the answer right?',
    'Did it check everything?',
    'Did it follow the rules?',
    'See the results',
    'How the test works',
  ]) {
    if (!home.includes(required)) {
      failures.push(`Homepage missing benchmark copy: ${required}`);
    }
  }
  for (const required of [
    'Set it up by talking to it.',
    'graphjin mcp --demo',
    'no Docker, no config file',
    'It tries the change before making it',
    'It cannot touch your secrets',
    'Only while you are building',
    'tried it first',
  ]) {
    if (!home.includes(required)) {
      failures.push(`Homepage missing MCP setup copy: ${required}`);
    }
  }
  for (const href of [
    '/agentic/watch-automation/',
    '/start/demos/#coffee-roastery',
    '/agentic/mcp/',
    '/configure/how-it-works/',
    '/benchmark/',
    '/benchmark/methodology/',
  ]) {
    const escaped = href.replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const hrefPattern = new RegExp(`href=(?:"${escaped}"|'${escaped}'|${escaped})(?=\\s|>)`);
    if (!hrefPattern.test(home)) {
      failures.push(`Homepage missing required link: ${href}`);
    }
  }
  if (home.includes('data-watch-motion=')) {
    failures.push('Homepage renders the watch story hidden before JavaScript runs');
  }
  for (const socialMeta of ['og:title', 'og:image', 'twitter:card']) {
    const socialPattern = new RegExp(`<meta\\s[^>]*(?:property|name)=(["'])?${socialMeta}\\1(?=[\\s/>])`);
    if (!socialPattern.test(home)) {
      failures.push(`Homepage missing social meta ${socialMeta}`);
    }
  }
}

const benchmarkIndexPath = path.join(publicRoot, 'benchmark', 'index.html');
if (await exists(benchmarkIndexPath)) {
  const benchmarkHTML = await readFile(benchmarkIndexPath, 'utf8');
  if (!benchmarkHTML.includes('data-benchmark-row') && !benchmarkHTML.includes('data-benchmark-empty')) {
    failures.push('Benchmark page has neither a leaderboard row nor the required empty state');
  }
}

const benchmarkDataPath = path.join(siteRoot, 'data', 'benchmarks.yaml');
if (await exists(benchmarkDataPath)) {
  const benchmarkData = await readFile(benchmarkDataPath, 'utf8');
  for (const match of benchmarkData.matchAll(/^\s*slug:\s*["']?([^"'#\s]+)["']?\s*$/gm)) {
    const slug = match[1];
    const page = path.join(publicRoot, 'benchmark', 'runs', slug, 'index.html');
    if (!(await exists(page))) {
      failures.push(`Benchmark data slug ${slug} has no built run page`);
    }
  }
}

if (await exists(path.join(publicRoot, 'agentic', 'watch-automation', 'index.html'))) {
  const watchGuide = await readFile(
    path.join(publicRoot, 'agentic', 'watch-automation', 'index.html'),
    'utf8'
  );
  if (!watchGuide.includes('<h1>Choosing Watches, Flows, and Workflows</h1>')) {
    failures.push('Watch automation guide lost its full page heading');
  }
  if (!watchGuide.includes('>Watch Automation</a>')) {
    failures.push('Watch automation guide is missing the shortened sidebar label');
  }
  if (!/\sclass=(?:"mermaid"|'mermaid'|mermaid)(?=\s|>)/.test(watchGuide)) {
    failures.push('Watch automation guide is missing its lifecycle or decision diagram');
  }
}

if (await exists(path.join(publicRoot, 'agentic', 'evaluation', 'index.html'))) {
  const evaluationGuide = await readFile(
    path.join(publicRoot, 'agentic', 'evaluation', 'index.html'),
    'utf8'
  );
  if (!evaluationGuide.includes('<h1>Evaluate the GraphJin Agent</h1>')) {
    failures.push('Agent evaluation guide lost its full page heading');
  }
  if (!evaluationGuide.includes('>Agent Evaluation</a>')) {
    failures.push('Agent evaluation guide is missing its shortened sidebar label');
  }
  const diagrams = evaluationGuide.match(/\sclass=(?:"mermaid"|'mermaid'|mermaid)(?=\s|>)/g) || [];
  if (diagrams.length < 2) {
    failures.push('Agent evaluation guide is missing its evaluation or baseline lifecycle diagram');
  }
}

if (await exists(path.join(siteRoot, 'static', 'js', 'site.js'))) {
  const siteJS = await readFile(path.join(siteRoot, 'static', 'js', 'site.js'), 'utf8');
  for (const required of [
    "document.querySelector('[data-watch-story]')",
    "'IntersectionObserver' in window",
    'prefers-reduced-motion: no-preference',
  ]) {
    if (!siteJS.includes(required)) {
      failures.push(`Watch story motion is missing progressive enhancement guard: ${required}`);
    }
  }
}

if (await exists(path.join(siteRoot, 'static', 'css', 'site.css'))) {
  const siteCSS = await readFile(path.join(siteRoot, 'static', 'css', 'site.css'), 'utf8');
  if (!siteCSS.includes('[data-watch-story][data-watch-motion="ready"]')) {
    failures.push('Watch story CSS can no longer distinguish JavaScript-enhanced rendering');
  }
  if (!siteCSS.includes('@media (prefers-reduced-motion: no-preference)')) {
    failures.push('Watch story CSS lost its reduced-motion boundary');
  }
}

if (await exists(path.join(siteRoot, 'static', 'install.sh'))) {
  const rootInstall = await readFile(path.join(repoRoot, 'install.sh'), 'utf8');
  const siteInstall = await readFile(path.join(siteRoot, 'static', 'install.sh'), 'utf8');
  if (rootInstall !== siteInstall) {
    failures.push('static/install.sh is not synced with root install.sh');
  }
  const mode = (await stat(path.join(siteRoot, 'static', 'install.sh'))).mode & 0o777;
  if ((mode & 0o111) === 0) {
    failures.push('static/install.sh is not executable');
  }
}

const htmlFiles = (await listFiles(publicRoot)).filter((file) => file.endsWith('.html'));
const anchorsByPage = new Map();
for (const file of htmlFiles) {
  const html = await readFile(file, 'utf8');
  const rel = path.relative(publicRoot, file);
  for (const meta of ['section', 'kind', 'slug', 'source']) {
    const metaPattern = new RegExp(`\\sdata-pagefind-meta=(?:"${meta}"|'${meta}'|${meta})(?=\\s|>|/)`);
    if (!metaPattern.test(html)) {
      failures.push(`${rel} is missing Pagefind ${meta} metadata`);
    }
  }
  const anchors = new Set(
    [...html.matchAll(/\sid=(?:"([^"]+)"|'([^']+)'|([^"' >]+))/g)].map(
      (match) => match[1] || match[2] || match[3]
    )
  );
  anchorsByPage.set(rel, anchors);

  for (const match of html.matchAll(/\shref=(?:"([^"]+)"|'([^']+)'|([^"' >]+))/g)) {
    const href = match[1] || match[2] || match[3];
    if (
      href.startsWith('http://') ||
      href.startsWith('https://') ||
      href.startsWith('mailto:') ||
      href.startsWith('tel:') ||
      href.startsWith('javascript:')
    ) {
      continue;
    }
    if (href.startsWith('#')) {
      const id = href.slice(1);
      if (id && !anchors.has(id)) failures.push(`${rel} has missing local anchor ${href}`);
      continue;
    }
    const [hrefPath, rawHash] = href.split('#');
    const rawPath = hrefPath.split('?')[0];
    const normalized = rawPath || rel;
    const target = normalized.endsWith('/')
      ? path.join(publicRoot, normalized, 'index.html')
      : path.join(publicRoot, normalized.replace(/^\//, ''));
    if (!(await exists(target))) {
      failures.push(`${rel} links to missing ${href}`);
      continue;
    }
    if (rawHash) {
      const targetRel = path.relative(publicRoot, target);
      if (!anchorsByPage.has(targetRel)) {
        const targetHtml = await readFile(target, 'utf8');
        anchorsByPage.set(
          targetRel,
          new Set(
            [...targetHtml.matchAll(/\sid=(?:"([^"]+)"|'([^']+)'|([^"' >]+))/g)].map(
              (match) => match[1] || match[2] || match[3]
            )
          )
        );
      }
      if (!anchorsByPage.get(targetRel).has(rawHash)) {
        failures.push(`${rel} links to missing anchor ${href}`);
      }
    }
  }
}

if (failures.length > 0) {
  console.error(failures.map((failure) => `- ${failure}`).join('\n'));
  process.exit(1);
}

console.log(`Checked ${htmlFiles.length} HTML files, required routes, anchors, assets, and content pages.`);

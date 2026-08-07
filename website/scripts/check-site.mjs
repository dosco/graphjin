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
  'benchmarks/index.html',
  'benchmarks/organizational-agent/index.html',
  'benchmarks/organizational-agent/methodology/index.html',
  'benchmarks/organizational-agent/runs/index.html',
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
  'benchmarks/_index.md',
  'benchmarks/organizational-agent/_index.md',
  'benchmarks/organizational-agent/methodology.md',
  'benchmarks/organizational-agent/runs/_index.md',
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
  ['benchmarks/index.html', 'Public, governed benchmark suites'],
  ['benchmarks/index.html', 'data-benchmark-family-row=organizational-agent'],
  ['benchmarks/organizational-agent/index.html', 'Can an AI agent answer an organization&rsquo;s real questions—correctly, governed, and at a cost you&rsquo;d pay?'],
  ['benchmarks/organizational-agent/index.html', 'What one task looks like'],
  ['benchmarks/organizational-agent/index.html', 'Now point it at your own organization'],
  ['benchmarks/organizational-agent/methodology/index.html', 'Frozen suite, live verification'],
  ['benchmarks/organizational-agent/runs/index.html', 'Published Benchmark Runs'],
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

for (const [route, target] of [
  ['benchmark/index.html', '/benchmarks/organizational-agent/'],
  ['benchmark/methodology/index.html', '/benchmarks/organizational-agent/methodology/'],
  ['benchmark/runs/index.html', '/benchmarks/organizational-agent/runs/'],
]) {
  const file = path.join(publicRoot, route);
  if (await exists(file)) {
    const html = await readFile(file, 'utf8');
    if (!html.includes(target)) {
      failures.push(`${route} does not redirect to ${target}`);
    }
  }
}

const organizationalBenchmarkIndex = path.join(publicRoot, 'benchmarks', 'organizational-agent', 'index.html');
if (await exists(organizationalBenchmarkIndex)) {
  const html = await readFile(organizationalBenchmarkIndex, 'utf8');
  if (!html.includes('No published runs yet.') && !html.includes('data-benchmark-row')) {
    failures.push('organizational benchmark page has neither an empty state nor a published benchmark row');
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
    "Don't take our word for it. We publish The Organizational Agent Benchmark.",
    'Built and published by GraphJin',
    'A full pass requires the right answer',
    'Build it by hand',
    'Connect the organization once',
    'See the benchmark',
    'Run it yourself',
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
    '/benchmarks/organizational-agent/',
    '/benchmarks/organizational-agent/methodology/',
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

const benchmarkIndexPath = path.join(publicRoot, 'benchmarks', 'organizational-agent', 'index.html');
if (await exists(benchmarkIndexPath)) {
  const benchmarkHTML = await readFile(benchmarkIndexPath, 'utf8');
  if (!benchmarkHTML.includes('data-benchmark-row') && !benchmarkHTML.includes('data-benchmark-empty')) {
    failures.push('Benchmark page has neither a leaderboard row nor the required empty state');
  }
}

const benchmarkDataPath = path.join(siteRoot, 'data', 'benchmarks', 'organizational_agent.yaml');
if (await exists(benchmarkDataPath)) {
  const benchmarkData = await readFile(benchmarkDataPath, 'utf8');

  const parseScalar = (raw) => {
    const value = raw.trim();
    if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
      return value.slice(1, -1);
    }
    if (value === 'true') return true;
    if (value === 'false') return false;
    if (value !== '' && Number.isFinite(Number(value))) return Number(value);
    return value;
  };
  const parsed = { benchmark: {}, suite: {}, runs: [] };
  let section = '';
  let currentRun = null;
  for (const line of benchmarkData.split('\n')) {
    if (line === 'benchmark:') {
      section = 'benchmark';
      continue;
    }
    if (line === 'suite:') {
      section = 'suite';
      continue;
    }
    if (line === 'runs:') {
      section = 'runs';
      continue;
    }
    const firstRunField = line.match(/^    - ([a-z0-9_]+):\s*(.*)$/);
    if (section === 'runs' && firstRunField) {
      currentRun = { [firstRunField[1]]: parseScalar(firstRunField[2]) };
      parsed.runs.push(currentRun);
      continue;
    }
    const runField = line.match(/^      ([a-z0-9_]+):\s*(.*)$/);
    if (section === 'runs' && currentRun && runField) {
      currentRun[runField[1]] = parseScalar(runField[2]);
      continue;
    }
    const suiteField = line.match(/^    ([a-z0-9_]+):\s*(.*)$/);
    if (section === 'suite' && suiteField) {
      parsed.suite[suiteField[1]] = parseScalar(suiteField[2]);
    }
    const benchmarkField = line.match(/^    ([a-z0-9_]+):\s*(.*)$/);
    if (section === 'benchmark' && benchmarkField) {
      parsed.benchmark[benchmarkField[1]] = parseScalar(benchmarkField[2]);
    }
  }

  if (!benchmarkData.includes('schema_version: graphjin.benchmark.data/v2')) {
    failures.push('Organizational benchmark data is not schema v2');
  }
  if (parsed.benchmark.slug !== 'organizational-agent' || parsed.benchmark.name !== 'The Organizational Agent Benchmark') {
    failures.push(`Organizational benchmark identity is invalid (${parsed.benchmark.slug ?? 'missing'} / ${parsed.benchmark.name ?? 'missing'})`);
  }
  for (const run of parsed.runs) {
    const page = path.join(publicRoot, 'benchmarks', 'organizational-agent', 'runs', String(run.slug), 'index.html');
    if (!(await exists(page))) {
      failures.push(`Benchmark data slug ${run.slug} has no built run page`);
    }
    const alias = path.join(publicRoot, 'benchmark', 'runs', String(run.slug), 'index.html');
    if (!(await exists(alias))) {
      failures.push(`Benchmark run ${run.slug} is missing its old URL alias`);
    } else {
      const aliasHTML = await readFile(alias, 'utf8');
      const target = `/benchmarks/organizational-agent/runs/${run.slug}/`;
      if (!aliasHTML.includes(target)) {
        failures.push(`Benchmark run alias ${run.slug} does not redirect to ${target}`);
      }
    }
  }

  const renderedDataAttribute = (html, field) => {
    const match = html.match(
      new RegExp(`${field}=(?:"([^"]*)"|'([^']*)'|([^\\s>]+))`)
    );
    return match ? match[1] ?? match[2] ?? match[3] : undefined;
  };
  const efficiencyFields = [
    ['data-provider-total-tokens', (run) => run.provider_total_tokens ?? run.total_tokens ?? 0],
    ['data-prompt-tokens', (run) => run.prompt_tokens ?? 0],
    ['data-completion-tokens', (run) => run.completion_tokens ?? 0],
    ['data-latency-p50-ms', (run) => run.latency_p50_ms ?? 0],
    ['data-latency-p95-ms', (run) => run.latency_p95_ms ?? 0],
    ['data-estimated-list-cost-usd', (run) => run.estimated_list_cost_usd ?? 0],
    ['data-estimated-list-cost-per-task-usd', (run) => run.estimated_list_cost_per_task_usd ?? 0],
  ];
  const rankedRuns = parsed.runs.filter((run) => run.ranked === true);
  if (rankedRuns.length > 0 && (await exists(benchmarkIndexPath))) {
    const benchmarkHTML = await readFile(benchmarkIndexPath, 'utf8');
    for (const run of rankedRuns) {
      const escapedRunID = String(run.run_id).replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&');
      const boardMarker = new RegExp(`data-benchmark-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).exec(benchmarkHTML);
      if (!boardMarker) {
        failures.push(`Benchmark leaderboard is missing its model row for ${run.run_id}`);
      } else {
        const boardRowStart = benchmarkHTML.lastIndexOf('<tr', boardMarker.index);
        const boardRowEnd = benchmarkHTML.indexOf('>', boardMarker.index);
        const boardRowTag = benchmarkHTML.slice(boardRowStart, boardRowEnd + 1);
        const renderedRelease = renderedDataAttribute(boardRowTag, 'data-graphjin-release');
        if (!run.release || renderedRelease !== String(run.release)) {
          failures.push(`Benchmark row ${run.run_id} does not expose its GraphJin release (${renderedRelease ?? 'missing'} != ${run.release ?? 'missing'})`);
        }
      }
      const marker = new RegExp(`data-benchmark-efficiency-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).exec(benchmarkHTML);
      if (!marker) {
        failures.push(`Benchmark leaderboard is missing efficiency data for ${run.run_id}`);
        continue;
      }
      const rowStart = benchmarkHTML.lastIndexOf('<tr', marker.index);
      const rowEnd = benchmarkHTML.indexOf('>', marker.index);
      const rowTag = benchmarkHTML.slice(rowStart, rowEnd + 1);
      for (const [field, expectedValue] of efficiencyFields) {
        const rendered = renderedDataAttribute(rowTag, field);
        const expected = Number(expectedValue(run));
        if (rendered === undefined || Number(rendered) !== expected) {
          failures.push(`Benchmark efficiency ${field} for ${run.run_id} does not match organizational_agent.yaml (${rendered ?? 'missing'} != ${expected})`);
        }
      }

      const runPagePath = path.join(publicRoot, 'benchmarks', 'organizational-agent', 'runs', String(run.slug), 'index.html');
      if (await exists(runPagePath)) {
        const runPage = await readFile(runPagePath, 'utf8');
        const summaryPattern = new RegExp(`data-benchmark-run-summary=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`);
        const summaryMatch = summaryPattern.exec(runPage);
        if (!summaryMatch) {
          failures.push(`Benchmark run page ${run.run_id} is missing its operational summary`);
          continue;
        }
        const summaryStart = runPage.lastIndexOf('<div', summaryMatch.index);
        const summaryEnd = runPage.indexOf('>', summaryMatch.index);
        const summaryTag = runPage.slice(summaryStart, summaryEnd + 1);
        for (const [field, expectedValue] of efficiencyFields.filter(([name]) => !['data-prompt-tokens', 'data-completion-tokens'].includes(name))) {
          const rendered = renderedDataAttribute(summaryTag, field);
          const expected = Number(expectedValue(run));
          if (rendered === undefined || Number(rendered) !== expected) {
            failures.push(`Benchmark run summary ${field} for ${run.run_id} does not match organizational_agent.yaml (${rendered ?? 'missing'} != ${expected})`);
          }
        }
      }
    }
  }

  const topAccepted = parsed.runs
    .filter((run) => run.ranked === true && run.accepted === true)
    .sort((a, b) => Number(b.recall) - Number(a.recall))[0];
  const homePath = path.join(publicRoot, 'index.html');
  if (topAccepted && (await exists(homePath))) {
    const home = await readFile(homePath, 'utf8');
    const renderedBenchmarkValue = (field) => {
      const match = home.match(
        new RegExp(`data-benchmark-stat=(?:"${field}"|'${field}'|${field})\\s+data-benchmark-value=(?:"([^"]*)"|'([^']*)'|([^\\s>]+))`)
      );
      return match ? match[1] ?? match[2] ?? match[3] : undefined;
    };
    if (!home.includes('data-benchmark-proof')) {
      failures.push('Homepage is missing the data-driven benchmark proof module');
    }
    for (const field of [
      'recall',
      'safety_precision',
      'method_recall',
      'estimated_list_cost_per_task_usd',
      'latency_p50_ms',
    ]) {
      const rendered = renderedBenchmarkValue(field);
      const expected = Number(topAccepted[field] ?? 0);
      if (rendered === undefined || Number(rendered) !== expected) {
        failures.push(`Homepage benchmark stat ${field} does not match organizational_agent.yaml (${rendered ?? 'missing'} != ${expected})`);
      }
    }
    if (renderedBenchmarkValue('label') !== String(topAccepted.label)) {
      failures.push('Homepage benchmark model label does not match organizational_agent.yaml');
    }
    if (renderedBenchmarkValue('suite_fingerprint') !== String(parsed.suite.suite_fingerprint)) {
      failures.push('Homepage benchmark suite fingerprint does not match organizational_agent.yaml');
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
  if (html.includes('GraphJin Agent Benchmark')) {
    failures.push(`${rel} contains the retired benchmark name`);
  }
  const isBenchmarkAlias = rel === 'benchmark/index.html' || rel.startsWith(`benchmark${path.sep}`);
  if (!isBenchmarkAlias) {
    for (const meta of ['section', 'kind', 'slug', 'source']) {
      const metaPattern = new RegExp(`\\sdata-pagefind-meta=(?:"${meta}"|'${meta}'|${meta})(?=\\s|>|/)`);
      if (!metaPattern.test(html)) {
        failures.push(`${rel} is missing Pagefind ${meta} metadata`);
      }
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

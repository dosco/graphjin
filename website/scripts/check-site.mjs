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
  'og/deeporg-og.png',
  'og/index.html',
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
  'benchmarks/deeporg/index.html',
  'benchmarks/deeporg/methodology/index.html',
  'benchmarks/deeporg/runs/index.html',
  'benchmarks/organizational-agent/index.html',
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
  'benchmarks/deeporg/_index.md',
  'benchmarks/deeporg/methodology.md',
  'benchmarks/deeporg/runs/_index.md',
  'benchmark/_index.md',
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
  ['benchmarks/index.html', 'data-benchmark-family-row=deeporg'],
  ['benchmarks/deeporg/index.html', 'The Organizational Agent Benchmark · by GraphJin'],
  ['benchmarks/deeporg/index.html', 'Model comparison'],
  ['benchmarks/deeporg/index.html', 'Run DeepORG on your organization'],
  ['benchmarks/deeporg/methodology/index.html', 'Frozen suite, live verification'],
  ['benchmarks/deeporg/runs/index.html', 'Published DeepORG Runs'],
  ['benchmark/index.html', 'What DeepORG actually tests'],
  ['benchmark/index.html', 'From question to verified answer'],
  ['benchmark/index.html', 'We grade our own AI agent in public.'],
  ['benchmark/index.html', "Most AI agents guess. Ours can't."],
  ['benchmark/index.html', 'Watch it answer a real question.'],
  ['benchmark/index.html', 'The agent never holds the keys.'],
  ['benchmark/index.html', 'graphjin serve --demo'],
  ['benchmark/index.html', 'not a published test item'],
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

function selectBestBenchmarkRuns(runs) {
  const bestByModel = new Map();
  for (const run of runs.filter((entry) => entry.board_eligible === true)) {
    const key = String(run.model_key);
    const current = bestByModel.get(key);
    if (!current ||
        Number(run.recall) > Number(current.recall) ||
        (Number(run.recall) === Number(current.recall) && String(run.run_id) > String(current.run_id))) {
      bestByModel.set(key, run);
    }
  }
  return [...bestByModel.values()].sort((a, b) =>
    Number(b.recall) - Number(a.recall) || String(b.run_id).localeCompare(String(a.run_id))
  );
}

const selectionContract = selectBestBenchmarkRuns([
  { run_id: '20260801T000000Z-high', model_key: 'google-gemini/model', recall: 0.8, board_eligible: true, accepted: true },
  { run_id: '20260802T000000Z-lower', model_key: 'google-gemini/model', recall: 0.7, board_eligible: true, accepted: true },
  { run_id: '20260803T000000Z-tie', model_key: 'google-gemini/model', recall: 0.8, board_eligible: true, accepted: false },
  { run_id: '20260804T000000Z-invalid', model_key: 'google-gemini/model', recall: 0.99, board_eligible: false, accepted: true },
  { run_id: '20260805T000000Z-route', model_key: 'openai-compatible/model', recall: 0.6, board_eligible: true, accepted: true },
]);
if (selectionContract.map((run) => run.run_id).join(',') !==
    '20260803T000000Z-tie,20260805T000000Z-route') {
  failures.push('Best-result selection contract failed lower-score, tie, eligibility, failed-run, or provider-route coverage');
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
  ['benchmark/methodology/index.html', '/benchmarks/deeporg/methodology/'],
  ['benchmark/runs/index.html', '/benchmarks/deeporg/runs/'],
  ['benchmarks/organizational-agent/index.html', '/benchmarks/deeporg/'],
]) {
  const file = path.join(publicRoot, route);
  if (await exists(file)) {
    const html = await readFile(file, 'utf8');
    if (!html.includes(target)) {
      failures.push(`${route} does not redirect to ${target}`);
    }
  }
}

const deepOrgBenchmarkIndex = path.join(publicRoot, 'benchmarks', 'deeporg', 'index.html');
if (await exists(deepOrgBenchmarkIndex)) {
  const html = await readFile(deepOrgBenchmarkIndex, 'utf8');
  if (!html.includes('No published runs yet.') && !html.includes('data-benchmark-model-chart')) {
    failures.push('DeepORG page has neither an empty state nor a published benchmark chart');
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
    'DeepORG · by GraphJin',
    'Can an AI agent actually do what your organization needs?',
    'Ours can — and we publish the proof.',
    'We publish the exam. Every result, including the failures.',
    "Best published score for every model we've tested.",
    'Right way of getting it',
    'Try it in 2 minutes',
    'Boot the exact demo it was graded on',
    'See the full exam',
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
    '/benchmarks/deeporg/',
    '/benchmarks/deeporg/methodology/',
    '/benchmark/',
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

const benchmarkIndexPath = path.join(publicRoot, 'benchmarks', 'deeporg', 'index.html');
if (await exists(benchmarkIndexPath)) {
  const benchmarkHTML = await readFile(benchmarkIndexPath, 'utf8');
  if (!benchmarkHTML.includes('data-benchmark-model-chart') && !benchmarkHTML.includes('data-benchmark-empty')) {
    failures.push('DeepORG page has neither a model chart nor the required empty state');
  }
}

const benchmarkDataPath = path.join(siteRoot, 'data', 'benchmarks', 'deeporg.yaml');
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
  let runSubsection = '';
  let suiteSubsection = '';
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
      runSubsection = '';
      continue;
    }
    const runField = line.match(/^      ([a-z0-9_]+):\s*(.*)$/);
    const nestedRunMapKeys = ['category_recall', 'rollup_recall', 'rollup_pass_power'];
    if (section === 'runs' && currentRun && runField) {
      if (nestedRunMapKeys.includes(runField[1])) {
        currentRun[runField[1]] = {};
        runSubsection = runField[1];
      } else {
        currentRun[runField[1]] = parseScalar(runField[2]);
        runSubsection = '';
      }
      continue;
    }
    const nestedRunField = line.match(/^        ([a-z0-9_-]+):\s*(.*)$/);
    if (section === 'runs' && currentRun && nestedRunMapKeys.includes(runSubsection) && nestedRunField) {
      currentRun[runSubsection][nestedRunField[1]] = parseScalar(nestedRunField[2]);
      continue;
    }
    const suiteField = line.match(/^    ([a-z0-9_]+):\s*(.*)$/);
    if (section === 'suite' && suiteField) {
      if (['category_counts', 'generation_scopes', 'rollup_map'].includes(suiteField[1])) {
        parsed.suite[suiteField[1]] = {};
        suiteSubsection = suiteField[1];
      } else {
        parsed.suite[suiteField[1]] = parseScalar(suiteField[2]);
        suiteSubsection = '';
      }
      continue;
    }
    const nestedSuiteField = line.match(/^        ([a-z0-9_.-]+):\s*(.*)$/);
    if (section === 'suite' && suiteSubsection && nestedSuiteField) {
      parsed.suite[suiteSubsection][nestedSuiteField[1].replace(/^['"]|['"]$/g, '')] = parseScalar(nestedSuiteField[2]);
      continue;
    }
    const benchmarkField = line.match(/^    ([a-z0-9_]+):\s*(.*)$/);
    if (section === 'benchmark' && benchmarkField) {
      parsed.benchmark[benchmarkField[1]] = parseScalar(benchmarkField[2]);
    }
  }

  if (!benchmarkData.includes('schema_version: graphjin.benchmark.data/v3')) {
    failures.push('DeepORG benchmark data is not schema v3');
  }
  if (parsed.benchmark.slug !== 'deeporg' || parsed.benchmark.name !== 'DeepORG — The Organizational Agent Benchmark') {
    failures.push(`DeepORG benchmark identity is invalid (${parsed.benchmark.slug ?? 'missing'} / ${parsed.benchmark.name ?? 'missing'})`);
  }
  for (const run of parsed.runs) {
    const page = path.join(publicRoot, 'benchmarks', 'deeporg', 'runs', String(run.slug), 'index.html');
    if (!(await exists(page))) {
      failures.push(`Benchmark data slug ${run.slug} has no built run page`);
    }
    const alias = path.join(publicRoot, 'benchmark', 'runs', String(run.slug), 'index.html');
    if (!(await exists(alias))) {
      failures.push(`Benchmark run ${run.slug} is missing its old URL alias`);
    } else {
      const aliasHTML = await readFile(alias, 'utf8');
      const target = `/benchmarks/deeporg/runs/${run.slug}/`;
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
  const governanceFields = [
    ['data-guard-interventions', (run) => run.guard_interventions ?? 0],
    ['data-unsafe-effects', (run) => run.unsafe_effects ?? 0],
  ];
  // A run's thinking effort changes both what it can do and what it costs, so
  // two rows for the same model are not comparable without it. It lives in run
  // provenance; assert the published row carries it rather than leaving the
  // fact to prose in the notes.
  // Ranked rows only: superseded rows were published before the field existed
  // and cannot be rescored retroactively for every historical build.
  for (const run of (parsed.runs ?? []).filter((entry) => entry.ranked === true)) {
    if (/reasoning_effort|thinking enabled/i.test(String(run.notes ?? '')) && !String(run.reasoning ?? '').trim()) {
      failures.push(`Run ${run.run_id} describes its thinking effort in notes but has no reasoning field`);
    }
  }
  const comparisonGeneration = parsed.suite.comparison_generation ?? parsed.suite.generation;
  const currentRuns = parsed.runs.filter(
    (run) => run.ranked === true && run.generation === comparisonGeneration
  );
  for (const run of parsed.runs) {
    if (!String(run.model_key ?? '').trim()) {
      failures.push(`DeepORG run ${run.run_id} has no canonical model_key`);
    }
    if (typeof run.board_eligible !== 'boolean') {
      failures.push(`DeepORG run ${run.run_id} has no explicit board_eligible decision`);
    }
    if (/corrected scoring contract|agent harness defect/i.test(String(run.unranked_reason ?? '')) && run.board_eligible === true) {
      failures.push(`Invalidated DeepORG run ${run.run_id} is still eligible for the public board`);
    }
  }

  // Mirrors layouts/partials/benchmark-best-results.html.
  const bestRuns = selectBestBenchmarkRuns(parsed.runs);
  const bestRunIDs = new Set(bestRuns.map((run) => String(run.run_id)));
  const topBest = bestRuns[0];
  const historyRuns = parsed.runs.filter((run) => !bestRunIDs.has(String(run.run_id)));
  const suiteTaskTotal = Object.values(parsed.suite.category_counts ?? {}).reduce(
    (total, value) => total + Number(value),
    0
  );

  const benchmarkLandingPath = path.join(publicRoot, 'benchmark', 'index.html');
  if (topBest && (await exists(benchmarkLandingPath))) {
    const landingHTML = await readFile(benchmarkLandingPath, 'utf8');
    if (!landingHTML.includes('data-benchmark-landing') ||
        renderedDataAttribute(landingHTML, 'data-benchmark-selection') !== 'best-trusted') {
      failures.push('Friendly DeepORG page is missing its best-trusted selection marker');
    }
    if (/\bcohort\b/i.test(landingHTML)) {
      failures.push('Friendly DeepORG page exposes internal cohort jargon');
    }
    if (!landingHTML.includes('Best published score for every model')) {
      failures.push('Friendly DeepORG page does not explain the best-result rule in plain language');
    }

    const renderedModelColumns = [...landingHTML.matchAll(/data-benchmark-model=(?:"([^"]+)"|'([^']+)'|([^\s>]+))/g)]
      .map((match) => match[1] ?? match[2] ?? match[3]);
    const expectedModelColumns = bestRuns.map((run) => String(run.run_id));
    if (renderedModelColumns.length !== expectedModelColumns.length ||
        renderedModelColumns.some((runID, index) => runID !== expectedModelColumns[index])) {
      failures.push(`Friendly DeepORG comparison columns do not match the ${bestRuns.length} best model results`);
    }

    const escapedLeaderID = String(topBest.run_id).replaceAll(/[.*+?^$\{\}()|[\]\\]/g, '\\$&');
    const leaderMarker = new RegExp(`data-benchmark-leader=(?:"${escapedLeaderID}"|'${escapedLeaderID}'|${escapedLeaderID})(?=\\s|>)`).exec(landingHTML);
    if (!leaderMarker) {
      failures.push('Friendly DeepORG hero does not use the highest trusted model result');
    } else {
      const leaderStart = landingHTML.lastIndexOf('<div', leaderMarker.index);
      const leaderEnd = landingHTML.indexOf('>', leaderMarker.index);
      const leaderTag = landingHTML.slice(leaderStart, leaderEnd + 1);
      const reliablePasses = Number(topBest.task_count ?? 0) * Number(topBest.recall ?? 0);
      const expectedLeaderValues = [
        ['data-recall', Number(topBest.recall ?? 0)],
        ['data-unsafe-effects', Number(topBest.unsafe_effects ?? 0)],
        ['data-latency-p50-ms', Number(topBest.latency_p50_ms ?? 0)],
        ['data-cost-per-reliable-pass-usd', reliablePasses > 0 ? Number(topBest.estimated_list_cost_usd ?? 0) / reliablePasses : 0],
        ['data-task-count', Number(topBest.task_count ?? 0)],
        ['data-repeats', Number(topBest.repeats ?? 0)],
      ];
      for (const [field, expected] of expectedLeaderValues) {
        if (Number(renderedDataAttribute(leaderTag, field)) !== expected) {
          failures.push(`Friendly DeepORG hero ${field} does not match deeporg.yaml`);
        }
      }
    }

    if (!landingHTML.includes('>The DeepORG Benchmark</p>') ||
        !landingHTML.includes('>DeepORG: Can an AI agent actually do what your organization needs?</h2>') ||
        !/\bbenchmark-card-footer\b/.test(landingHTML)) {
      failures.push('Friendly DeepORG comparison card lost its benchmark identity or provenance footer');
    }
    const hasScrollHint = /\bbenchmark-scroll-hint\b/.test(landingHTML);
    if (hasScrollHint !== (bestRuns.length > 3)) {
      failures.push('Friendly DeepORG comparison scroll hint does not match its model count');
    }
    const bestCellCount = (landingHTML.match(/\bis-best\b/g) ?? []).length;
    if ((bestRuns.length < 2 && bestCellCount > 0) || (bestRuns.length > 1 && bestCellCount === 0)) {
      failures.push('Friendly DeepORG best-value highlighting does not match its model count');
    }
    if (!landingHTML.includes(`What ${Math.round(Number(topBest.recall) * 100)} actually means`)) {
      failures.push('Friendly DeepORG plain-English score heading does not match the leading result');
    }
    const validConfidence = Number.isFinite(Number(topBest.recall_ci_low)) &&
      Number.isFinite(Number(topBest.recall_ci_high)) &&
      Number(topBest.recall_ci_low) >= 0 && Number(topBest.recall_ci_high) <= 1 &&
      Number(topBest.recall_ci_high) >= Number(topBest.recall_ci_low);
    const renderedConfidenceLow = renderedDataAttribute(landingHTML, 'data-recall-ci-low');
    const renderedConfidenceHigh = renderedDataAttribute(landingHTML, 'data-recall-ci-high');
    if (validConfidence) {
      if (Number(renderedConfidenceLow) !== Number(topBest.recall_ci_low) ||
          Number(renderedConfidenceHigh) !== Number(topBest.recall_ci_high)) {
        failures.push('Friendly DeepORG confidence range does not match deeporg.yaml');
      }
    } else if (renderedConfidenceLow !== undefined || renderedConfidenceHigh !== undefined) {
      failures.push('Friendly DeepORG renders an invalid or unavailable confidence range');
    }
    if (!landingHTML.includes('https://graphjin.com/og/deeporg-og.png')) {
      failures.push('Friendly DeepORG page does not reference its dedicated social card');
    }

    for (const run of bestRuns) {
      const escapedRunID = String(run.run_id).replaceAll(/[.*+?^$\{\}()|[\]\\]/g, '\\$&');
      const marker = new RegExp(`data-benchmark-comparison-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).exec(landingHTML);
      if (!marker) {
        failures.push(`Friendly DeepORG comparison is missing best result ${run.run_id}`);
        continue;
      }
      const linkStart = landingHTML.lastIndexOf('<a', marker.index);
      const linkEnd = landingHTML.indexOf('>', marker.index);
      const linkTag = landingHTML.slice(linkStart, linkEnd + 1);
      const reliablePasses = Number(run.task_count ?? 0) * Number(run.recall ?? 0);
      const expected = [
        ['data-full-pass', Number(run.recall ?? 0)],
        ['data-every-time', Number(run.pass_power_k ?? 0)],
        ['data-ground-truth', Number(run.ground_truth_recall ?? 0)],
        ['data-method', Number(run.method_recall ?? 0)],
        ['data-behavior', Number(run.behavior_recall ?? 0)],
        ['data-unsafe-effects', Number(run.unsafe_effects ?? 0)],
        ['data-latency-p50-ms', Number(run.latency_p50_ms ?? 0)],
        ['data-cost-per-reliable-pass-usd', reliablePasses > 0 ? Number(run.estimated_list_cost_usd ?? 0) / reliablePasses : 0],
        ['data-task-count', Number(run.task_count ?? 0)],
      ];
      for (const [field, value] of expected) {
        if (Number(renderedDataAttribute(linkTag, field)) !== value) {
          failures.push(`Friendly DeepORG ${field} for ${run.run_id} does not match deeporg.yaml`);
        }
      }
      const expectedReport = `/benchmarks/deeporg/runs/${run.slug}/`;
      if (renderedDataAttribute(linkTag, 'href') !== expectedReport) {
        failures.push(`Friendly DeepORG report link for ${run.run_id} is incomplete`);
      }
    }
    for (const run of parsed.runs.filter((entry) => entry.board_eligible !== true)) {
      const escapedRunID = String(run.run_id).replaceAll(/[.*+?^$\{\}()|[\]\\]/g, '\\$&');
      if (new RegExp(`data-benchmark-comparison-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).test(landingHTML)) {
        failures.push(`Ineligible run ${run.run_id} leaked onto the friendly comparison board`);
      }
    }

    const renderedTaskCounts = [...landingHTML.matchAll(/data-benchmark-task-count=(?:"(\d+)"|'(\d+)'|(\d+))/g)]
      .map((match) => Number(match[1] ?? match[2] ?? match[3]));
    if (renderedTaskCounts.reduce((total, value) => total + value, 0) !== suiteTaskTotal) {
      failures.push('Friendly DeepORG task group counts do not add up to the current published exam');
    }
    if (Number(renderedDataAttribute(landingHTML, 'data-benchmark-episode-count')) !== Number(topBest.episode_count ?? 0)) {
      failures.push('Friendly DeepORG safety story episode count does not match the leading result');
    }
  }

  const benchmarkOGPath = path.join(publicRoot, 'og', 'index.html');
  if (topBest && (await exists(benchmarkOGPath))) {
    const benchmarkOG = await readFile(benchmarkOGPath, 'utf8');
    if (renderedDataAttribute(benchmarkOG, 'data-benchmark-selection') !== 'best-trusted') {
      failures.push('DeepORG social card source is missing its best-trusted marker');
    }
    const ogRunIDs = [...benchmarkOG.matchAll(/data-benchmark-og-run=(?:"([^"]+)"|'([^']+)'|([^\s>]+))/g)]
      .map((match) => match[1] ?? match[2] ?? match[3]);
    const expectedOGRunIDs = bestRuns.slice(0, 4).map((run) => String(run.run_id));
    if (ogRunIDs.length !== expectedOGRunIDs.length ||
        ogRunIDs.some((runID, index) => runID !== expectedOGRunIDs[index])) {
      failures.push('DeepORG social card does not use the leading best model results');
    }
    if (String(renderedDataAttribute(benchmarkOG, 'data-model-label')) !== String(topBest.label) ||
        Number(renderedDataAttribute(benchmarkOG, 'data-recall')) !== Number(topBest.recall) ||
        Number(renderedDataAttribute(benchmarkOG, 'data-unsafe-effects')) !== Number(topBest.unsafe_effects ?? 0)) {
      failures.push('DeepORG social card leader does not match deeporg.yaml');
    }
  }

  if (topBest && (await exists(benchmarkIndexPath))) {
    const benchmarkHTML = await readFile(benchmarkIndexPath, 'utf8');
    if (renderedDataAttribute(benchmarkHTML, 'data-benchmark-selection') !== 'best-trusted' ||
        !benchmarkHTML.includes('data-benchmark-best-current')) {
      failures.push('DeepORG detailed board is missing its best-trusted selection markers');
    }
    if (benchmarkHTML.includes('data-benchmark-prior-cohort')) {
      failures.push('DeepORG detailed board still renders muted prior-cohort rows');
    }
    if (/\bcohort\b/i.test(benchmarkHTML)) {
      failures.push('DeepORG detailed board exposes internal cohort jargon');
    }
    if (!benchmarkHTML.includes('data-benchmark-generation-history') ||
        !benchmarkHTML.includes('>Other published runs</h3>')) {
      failures.push('DeepORG board is missing its complete published-run archive');
    }
    for (const generation of new Set(historyRuns.map((run) => String(run.generation)))) {
      const escapedGeneration = generation.replaceAll(/[.*+?^$\{\}()|[\]\\]/g, '\\$&');
      const historyPattern = new RegExp(`data-benchmark-history-group=(?:"${escapedGeneration}"|'${escapedGeneration}'|${escapedGeneration})(?=\\s|>)`);
      if (!historyPattern.test(benchmarkHTML)) {
        failures.push(`DeepORG board is missing history group ${generation}`);
      }
    }
    for (const run of historyRuns) {
      const escapedRunID = String(run.run_id).replaceAll(/[.*+?^$\{\}()|[\]\\]/g, '\\$&');
      const historyMarker = new RegExp(`data-benchmark-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})[^>]*data-benchmark-history-generation=`);
      if (!historyMarker.test(benchmarkHTML)) {
        failures.push(`DeepORG archive is missing published run ${run.run_id}`);
      }
      if (run.board_eligible !== true && run.unranked_reason && !benchmarkHTML.includes(String(run.unranked_reason))) {
        failures.push(`DeepORG archive is missing the invalidation reason for ${run.run_id}`);
      }
    }

    for (const run of bestRuns) {
      const escapedRunID = String(run.run_id).replaceAll(/[.*+?^$\{\}()|[\]\\]/g, '\\$&');
      const chartMarker = new RegExp(`data-benchmark-chart-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).exec(benchmarkHTML);
      if (!chartMarker) {
        failures.push(`DeepORG chart is missing best result ${run.run_id}`);
      } else {
        const chartStart = benchmarkHTML.lastIndexOf('<g', chartMarker.index);
        const chartEnd = benchmarkHTML.indexOf('>', chartMarker.index);
        const chartTag = benchmarkHTML.slice(chartStart, chartEnd + 1);
        const reliablePasses = Number(run.task_count ?? 0) * Number(run.recall ?? 0);
        const expectedChartValues = [
          ['data-recall', Number(run.recall ?? 0)],
          ['data-cost-per-reliable-pass-usd', reliablePasses > 0 ? Number(run.estimated_list_cost_usd ?? 0) / reliablePasses : 0],
          ['data-latency-p50-ms', Number(run.latency_p50_ms ?? 0)],
        ];
        for (const [field, expected] of expectedChartValues) {
          const rendered = Number(renderedDataAttribute(chartTag, field));
          if (!Number.isFinite(rendered) || Math.abs(rendered - expected) > 1e-12) {
            failures.push(`DeepORG chart ${field} for ${run.run_id} does not match deeporg.yaml (${rendered} != ${expected})`);
          }
        }
        for (const [rollup, expectedValue] of Object.entries(run.rollup_recall ?? {})) {
          const rendered = Number(renderedDataAttribute(chartTag, `data-rollup-${rollup}`));
          if (!Number.isFinite(rendered) || Math.abs(rendered - Number(expectedValue)) > 1e-12) {
            failures.push(`DeepORG chart rollup ${rollup} for ${run.run_id} does not match deeporg.yaml`);
          }
        }
      }

      const efficiencyMarker = new RegExp(`data-benchmark-efficiency-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).exec(benchmarkHTML);
      if (!efficiencyMarker) {
        failures.push(`Benchmark leaderboard is missing efficiency data for best result ${run.run_id}`);
      } else {
        const rowStart = benchmarkHTML.lastIndexOf('<tr', efficiencyMarker.index);
        const rowEnd = benchmarkHTML.indexOf('>', efficiencyMarker.index);
        const rowTag = benchmarkHTML.slice(rowStart, rowEnd + 1);
        for (const [field, expectedValue] of efficiencyFields) {
          const rendered = renderedDataAttribute(rowTag, field);
          const expected = Number(expectedValue(run));
          if (rendered === undefined || Number(rendered) !== expected) {
            failures.push(`Benchmark efficiency ${field} for ${run.run_id} does not match deeporg.yaml`);
          }
        }
      }

      const scoreRowPattern = new RegExp(`data-benchmark-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).exec(benchmarkHTML);
      if (!scoreRowPattern) {
        failures.push(`Benchmark leaderboard is missing governance data for best result ${run.run_id}`);
      } else {
        const scoreRowStart = benchmarkHTML.lastIndexOf('<tr', scoreRowPattern.index);
        const scoreRowEnd = benchmarkHTML.indexOf('>', scoreRowPattern.index);
        const scoreRowTag = benchmarkHTML.slice(scoreRowStart, scoreRowEnd + 1);
        for (const [field, expectedValue] of governanceFields) {
          if (Number(renderedDataAttribute(scoreRowTag, field)) !== Number(expectedValue(run))) {
            failures.push(`Benchmark governance ${field} for ${run.run_id} does not match deeporg.yaml`);
          }
        }
      }
    }

    for (const run of parsed.runs.filter((entry) => entry.board_eligible !== true)) {
      const escapedRunID = String(run.run_id).replaceAll(/[.*+?^$\{\}()|[\]\\]/g, '\\$&');
      if (new RegExp(`data-benchmark-chart-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).test(benchmarkHTML) ||
          new RegExp(`data-benchmark-efficiency-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).test(benchmarkHTML)) {
        failures.push(`Ineligible run ${run.run_id} leaked onto the detailed best-result board`);
      }
    }

    for (const run of currentRuns) {
      const escapedRunID = String(run.run_id).replaceAll(/[.*+?^$\{\}()|[\]\\]/g, '\\$&');
      for (const [category, expectedValue] of Object.entries(run.category_recall ?? {})) {
        const escapedCategory = category.replaceAll(/[.*+?^$\{\}()|[\]\\]/g, '\\$&');
        const categoryPattern = new RegExp(`data-category-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})\\s+data-category=(?:"${escapedCategory}"|'${escapedCategory}'|${escapedCategory})\\s+data-recall=(?:"([^"]*)"|'([^']*)'|([^\\s>]+))`);
        const match = categoryPattern.exec(benchmarkHTML);
        const rendered = match ? Number(match[1] ?? match[2] ?? match[3]) : Number.NaN;
        if (!Number.isFinite(rendered) || Math.abs(rendered - Number(expectedValue)) > 1e-12) {
          failures.push(`DeepORG current-exam category ${category} for ${run.run_id} does not match deeporg.yaml`);
        }
      }
      if (run.rollup_recall && parsed.suite?.rollup_map && parsed.suite?.category_counts) {
        const recomputed = {};
        const weights = {};
        for (const [category, group] of Object.entries(parsed.suite.rollup_map)) {
          const count = Number(parsed.suite.category_counts[category] ?? 0);
          const score = run.category_recall?.[category];
          if (count > 0 && score !== undefined) {
            recomputed[group] = (recomputed[group] ?? 0) + count * Number(score);
            weights[group] = (weights[group] ?? 0) + count;
          }
        }
        for (const [group, expectedValue] of Object.entries(run.rollup_recall)) {
          if (!(group in recomputed)) continue;
          const derived = recomputed[group] / weights[group];
          if (Math.abs(derived - Number(expectedValue)) > 1e-9) {
            failures.push(`DeepORG rollup ${group} for ${run.run_id} disagrees with its category scores`);
          }
        }
      }
    }
    for (const run of bestRuns.filter((entry) => entry.generation !== comparisonGeneration)) {
      const escapedRunID = String(run.run_id).replaceAll(/[.*+?^$\{\}()|[\]\\]/g, '\\$&');
      if (new RegExp(`data-category-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).test(benchmarkHTML)) {
        failures.push(`Historical best result ${run.run_id} leaked into the same-exam category chart`);
      }
    }

    for (const run of parsed.runs) {
      const escapedRunID = String(run.run_id).replaceAll(/[.*+?^$\{\}()|[\]\\]/g, '\\$&');
      const runPagePath = path.join(publicRoot, 'benchmarks', 'deeporg', 'runs', String(run.slug), 'index.html');
      if (!(await exists(runPagePath))) continue;
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
        if (Number(renderedDataAttribute(summaryTag, field)) !== Number(expectedValue(run))) {
          failures.push(`Benchmark run summary ${field} for ${run.run_id} does not match deeporg.yaml`);
        }
      }
      for (const [field, expectedValue] of governanceFields) {
        if (Number(renderedDataAttribute(summaryTag, field)) !== Number(expectedValue(run))) {
          failures.push(`Benchmark run summary ${field} for ${run.run_id} does not match deeporg.yaml`);
        }
      }
      for (const [category, expectedValue] of Object.entries(run.category_recall ?? {})) {
        const escapedCategory = category.replaceAll(/[.*+?^$\{\}()|[\]\\]/g, '\\$&');
        const categoryPattern = new RegExp(`data-category=(?:"${escapedCategory}"|'${escapedCategory}'|${escapedCategory})\\s+data-recall=(?:"([^"]*)"|'([^']*)'|([^\\s>]+))`);
        const match = categoryPattern.exec(runPage);
        const rendered = match ? Number(match[1] ?? match[2] ?? match[3]) : Number.NaN;
        if (!Number.isFinite(rendered) || Math.abs(rendered - Number(expectedValue)) > 1e-12) {
          failures.push(`DeepORG run category ${category} for ${run.run_id} does not match deeporg.yaml`);
        }
      }
    }
  }

  const homePath = path.join(publicRoot, 'index.html');
  if (topBest && (await exists(homePath))) {
    const home = await readFile(homePath, 'utf8');
    const benchmarkPage = await readFile(benchmarkIndexPath, 'utf8');
    if (!home.includes('data-benchmark-proof') ||
        renderedDataAttribute(home, 'data-benchmark-selection') !== 'best-trusted') {
      failures.push('Homepage is missing the best-trusted benchmark proof module');
    }
    if (/\bcohort\b/i.test(home)) {
      failures.push('Homepage exposes internal cohort jargon');
    }
    for (const run of bestRuns) {
      const escapedRunID = String(run.run_id).replaceAll(/[.*+?^$\{\}()|[\]\\]/g, '\\$&');
      const chartMarker = new RegExp(`data-benchmark-chart-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).exec(home);
      if (!chartMarker) {
        failures.push(`Homepage benchmark chart is missing best result ${run.run_id}`);
        continue;
      }
      const chartStart = home.lastIndexOf('<div', chartMarker.index);
      const chartEnd = home.indexOf('>', chartMarker.index);
      const chartTag = home.slice(chartStart, chartEnd + 1);
      const reliablePasses = Number(run.task_count ?? 0) * Number(run.recall ?? 0);
      const expected = [
        ['data-recall', Number(run.recall ?? 0)],
        ['data-cost-per-reliable-pass-usd', reliablePasses > 0 ? Number(run.estimated_list_cost_usd ?? 0) / reliablePasses : 0],
        ['data-latency-p50-ms', Number(run.latency_p50_ms ?? 0)],
        ['data-unsafe-effects', Number(run.unsafe_effects ?? 0)],
        ['data-task-count', Number(run.task_count ?? 0)],
        ['data-repeats', Number(run.repeats ?? 0)],
      ];
      for (const [field, value] of expected) {
        const rendered = Number(renderedDataAttribute(chartTag, field));
        if (!Number.isFinite(rendered) || Math.abs(rendered - value) > 1e-12) {
          failures.push(`Homepage benchmark ${field} for ${run.run_id} does not match deeporg.yaml`);
        }
      }
    }
    for (const run of parsed.runs.filter((entry) => entry.board_eligible !== true)) {
      const escapedRunID = String(run.run_id).replaceAll(/[.*+?^$\{\}()|[\]\\]/g, '\\$&');
      if (new RegExp(`data-benchmark-chart-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).test(home)) {
        failures.push(`Ineligible run ${run.run_id} leaked onto the homepage board`);
      }
    }

    const heroMarker = /<[^>]*data-benchmark-hero[^>]*>/.exec(home);
    if (!heroMarker) {
      failures.push('Homepage hero is missing the data-driven benchmark module');
    } else {
      const heroTag = heroMarker[0];
      const heroAttempts = bestRuns.reduce((total, run) => total + Number(run.episode_count ?? 0), 0);
      const heroUnsafe = bestRuns.reduce((total, run) => total + Number(run.unsafe_effects ?? 0), 0);
      const heroExpected = [
        ['data-hero-models', bestRuns.length],
        ['data-hero-attempts', heroAttempts],
        ['data-hero-unsafe', heroUnsafe],
        ['data-hero-published-runs', parsed.runs.length],
      ];
      for (const [field, value] of heroExpected) {
        const rendered = Number(renderedDataAttribute(heroTag, field));
        if (!Number.isFinite(rendered) || rendered !== value) {
          failures.push(`Homepage hero ${field} does not match deeporg.yaml`);
        }
      }
      if (heroUnsafe !== 0 && /\b0 unsafe effects\b/.test(home)) {
        failures.push(`Homepage claims 0 unsafe effects but its displayed best results report ${heroUnsafe}`);
      }
      if (/every (ranked )?model[^.]*small|frontier models have not|haven't (even )?run the big/i.test(home)) {
        failures.push('Homepage still contains stale model-size claims');
      }
    }

    const storyMarker = renderedDataAttribute(benchmarkPage, 'data-current-recall');
    if (storyMarker === undefined || Number(storyMarker) !== Number(topBest.recall)) {
      failures.push('DeepORG result story does not match the highest trusted result');
    }
    const storySafety = renderedDataAttribute(benchmarkPage, 'data-current-unsafe-effects');
    if (storySafety === undefined || Number(storySafety) !== Number(topBest.unsafe_effects ?? 0)) {
      failures.push('DeepORG result story safety does not match the highest trusted result');
    }
  }
}

const deepORGCardPath = path.join(publicRoot, 'og', 'deeporg-og.png');
if (await exists(deepORGCardPath)) {
  const png = await readFile(deepORGCardPath);
  if (png.length < 24 || png.toString('ascii', 1, 4) !== 'PNG') {
    failures.push('DeepORG social card is not a valid PNG');
  } else {
    const width = png.readUInt32BE(16);
    const height = png.readUInt32BE(20);
    if (width !== 1200 || height !== 630) {
      failures.push(`DeepORG social card is ${width}x${height}; expected 1200x630`);
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
  for (const required of [
    "document.querySelector('[data-benchmark-count-up]')",
    "document.querySelectorAll('[data-bench-reveal], [data-bench-demo]')",
  ]) {
    if (!siteJS.includes(required)) {
      failures.push(`Benchmark landing motion is missing progressive enhancement guard: ${required}`);
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
  for (const required of [
    '[data-bench-reveal][data-bench-motion="ready"]',
    '[data-bench-demo][data-bench-motion="ready"]',
  ]) {
    if (!siteCSS.includes(required)) {
      failures.push(`Benchmark landing CSS can no longer distinguish JavaScript-enhanced rendering: ${required}`);
    }
  }
}

if (await exists(path.join(siteRoot, 'static', 'install.sh'))) {
// The homepage invites readers to run three exam items verbatim and calls them
// real published tasks. They are transcribed into the benchmark-try shortcode,
// so a suite rotation would silently turn that invitation into fiction. Assert
// every quoted item still exists word-for-word in the committed suite.
const suitePath = path.join(repoRoot, 'cmd', 'benchmark', 'public-suite.json');
const tryPath = path.join(publicRoot, 'index.html');
if ((await exists(suitePath)) && (await exists(tryPath))) {
  const suite = JSON.parse(await readFile(suitePath, 'utf8'));
  const prompts = new Set((suite.tasks ?? []).map((task) => String(task.prompt)));
  const homeHTML = await readFile(tryPath, 'utf8');
  // Scope to the section itself. Scanning to end-of-document would swallow any
  // blockquote a later section adds and report it as a fake exam item.
  const tryStart = homeHTML.indexOf('data-benchmark-try');
  if (tryStart === -1) {
    failures.push('Homepage lost the try-the-exam section');
  }
  const tryEnd = tryStart === -1 ? -1 : homeHTML.indexOf('</section>', tryStart);
  const trySection = tryStart === -1 ? '' : homeHTML.slice(tryStart, tryEnd === -1 ? undefined : tryEnd);
  const quoted = [...trySection.matchAll(/<blockquote>([\s\S]*?)<\/blockquote>/g)].map((match) =>
    match[1]
      .replace(/<[^>]*>/g, '')
      .replace(/&quot;/g, '"')
      .replace(/&#39;|&rsquo;/g, "'")
      .replace(/&amp;/g, '&')
      .replace(/[\u201c\u201d]/g, '')
      .replace(/\u2019/g, "'")
      .trim()
  );
  if (tryStart !== -1 && quoted.length === 0) {
    failures.push('Homepage try-the-exam section quotes no benchmark items');
  }
  for (const prompt of quoted) {
    if (!prompts.has(prompt)) {
      failures.push(`Homepage quotes "${prompt.slice(0, 60)}..." as a published DeepORG item, but it is not in cmd/benchmark/public-suite.json`);
    }
  }
}

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
  const isBenchmarkAlias = rel === 'benchmark/index.html' || rel.startsWith(`benchmark${path.sep}`) ||
    rel === path.join('benchmarks', 'organizational-agent', 'index.html') ||
    rel.startsWith(path.join('benchmarks', 'organizational-agent') + path.sep);
  const isBuildOnlyDocument = rel === path.join('og', 'index.html');
  if (!isBenchmarkAlias && !isBuildOnlyDocument) {
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

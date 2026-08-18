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
    "We haven't run the frontier models yet.",
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

  if (!benchmarkData.includes('schema_version: graphjin.benchmark.data/v2')) {
    failures.push('DeepORG benchmark data is not schema v2');
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
  const comparisonGeneration = parsed.suite.comparison_generation ?? parsed.suite.generation;
  const rankedRuns = parsed.runs.filter(
    (run) => run.ranked === true && run.generation === comparisonGeneration
  );
  const topRanked = [...rankedRuns].sort((a, b) => Number(b.recall) - Number(a.recall))[0];
  // Mirrors layouts/partials/benchmark-prior-context.html: for each model absent
  // from the current ranked cohort, its most recent legitimately demoted cohort
  // row from the latest prior generation. Retracted/superseded runs never qualify.
  const priorCohortContext = (() => {
    const rankedModels = new Set(rankedRuns.map((run) => String(run.model)));
    const candidates = parsed.runs.filter(
      (run) => run.accepted === true && String(run.unranked_reason ?? '').startsWith('previous public benchmark cohort')
    );
    const priorGeneration = candidates.map((run) => String(run.generation)).sort().at(-1);
    if (!priorGeneration) return { generation: undefined, runs: [] };
    const seenModels = new Set();
    const runs = [];
    const ordered = candidates
      .filter((run) => String(run.generation) === priorGeneration)
      .sort((a, b) => String(b.generated_at).localeCompare(String(a.generated_at)));
    for (const run of ordered) {
      const model = String(run.model);
      if (rankedModels.has(model) || seenModels.has(model)) continue;
      seenModels.add(model);
      runs.push(run);
    }
    runs.sort((a, b) => Number(b.recall) - Number(a.recall));
    return { generation: priorGeneration, runs };
  })();
  const contextRuns = priorCohortContext.runs;
  const displayedColumnCount = rankedRuns.length + contextRuns.length;
  const benchmarkLandingPath = path.join(publicRoot, 'benchmark', 'index.html');
  if (rankedRuns.length > 0 && (await exists(benchmarkLandingPath))) {
    const landingHTML = await readFile(benchmarkLandingPath, 'utf8');
    if (!landingHTML.includes('data-benchmark-landing')) {
      failures.push('Friendly DeepORG page is missing its landing marker');
    }
    if (String(renderedDataAttribute(landingHTML, 'data-benchmark-generation')) !== String(comparisonGeneration)) {
      failures.push('Friendly DeepORG page generation does not match deeporg.yaml');
    }
    const renderedModelColumns = [...landingHTML.matchAll(/data-benchmark-model=(?:"([^"]+)"|'([^']+)'|([^\s>]+))/g)]
      .map((match) => match[1] ?? match[2] ?? match[3]);
    if (renderedModelColumns.length !== displayedColumnCount) {
      failures.push(`Friendly DeepORG comparison rendered ${renderedModelColumns.length} model columns for ${rankedRuns.length} ranked + ${contextRuns.length} prior-cohort runs`);
    }
    const leaderMarker = new RegExp(`data-benchmark-leader=(?:"${String(topRanked.run_id).replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&')}"|'${String(topRanked.run_id).replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&')}'|${String(topRanked.run_id).replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&')})(?=\\s|>)`).exec(landingHTML);
    if (!leaderMarker) {
      failures.push('Friendly DeepORG hero does not use the top ranked run');
    } else {
      const leaderStart = landingHTML.lastIndexOf('<div', leaderMarker.index);
      const leaderEnd = landingHTML.indexOf('>', leaderMarker.index);
      const leaderTag = landingHTML.slice(leaderStart, leaderEnd + 1);
      const reliablePasses = Number(topRanked.task_count ?? 0) * Number(topRanked.recall ?? 0);
      const expectedLeaderValues = [
        ['data-recall', Number(topRanked.recall ?? 0)],
        ['data-unsafe-effects', Number(topRanked.unsafe_effects ?? 0)],
        ['data-latency-p50-ms', Number(topRanked.latency_p50_ms ?? 0)],
        ['data-cost-per-reliable-pass-usd', reliablePasses > 0 ? Number(topRanked.estimated_list_cost_usd ?? 0) / reliablePasses : 0],
        ['data-task-count', Number(topRanked.task_count ?? 0)],
        ['data-repeats', Number(topRanked.repeats ?? 0)],
      ];
      for (const [field, expected] of expectedLeaderValues) {
        if (Number(renderedDataAttribute(leaderTag, field)) !== expected) {
          failures.push(`Friendly DeepORG hero ${field} does not match deeporg.yaml`);
        }
      }
    }
    // The comparison block doubles as the shareable card and the social image,
    // so it must name the benchmark and state the suite size rather than lead
    // with a generic "compare models" heading.
    if (!landingHTML.includes('>The DeepORG Benchmark</p>')) {
      failures.push('Friendly DeepORG comparison card lost its benchmark-name eyebrow');
    }
    if (!landingHTML.includes('>DeepORG: Can an AI agent actually do what your organization needs?</h2>')) {
      failures.push('Friendly DeepORG comparison card lost its benchmark question heading');
    }
    const suiteTaskTotal = Object.values(parsed.suite.category_counts ?? {}).reduce(
      (total, value) => total + Number(value),
      0
    );
    if (!new RegExp(`DeepORG runs one frozen exam of ${suiteTaskTotal} tasks`).test(landingHTML)) {
      failures.push('Friendly DeepORG comparison card should state the frozen suite size');
    }
    if (!/\bbenchmark-card-footer\b/.test(landingHTML)) {
      failures.push('Friendly DeepORG comparison card lost its provenance footer');
    }
    const hasScrollHint = /\bbenchmark-scroll-hint\b/.test(landingHTML);
    if (hasScrollHint !== (displayedColumnCount > 3)) {
      failures.push('Friendly DeepORG comparison scroll hint does not match its model count');
    }
    const bestCellCount = (landingHTML.match(/\bis-best\b/g) ?? []).length;
    if ((rankedRuns.length < 2 && bestCellCount > 0) || (rankedRuns.length > 1 && bestCellCount === 0)) {
      failures.push('Friendly DeepORG best-value highlighting does not match its model count');
    }
    if (!landingHTML.includes(`What ${Math.round(Number(topRanked.recall) * 100)} actually means`)) {
      failures.push('Friendly DeepORG plain-English score heading does not match the top ranked run');
    }
    const validConfidence = Number.isFinite(Number(topRanked.recall_ci_low)) &&
      Number.isFinite(Number(topRanked.recall_ci_high)) &&
      Number(topRanked.recall_ci_low) >= 0 && Number(topRanked.recall_ci_high) <= 1 &&
      Number(topRanked.recall_ci_high) >= Number(topRanked.recall_ci_low);
    const renderedConfidenceLow = renderedDataAttribute(landingHTML, 'data-recall-ci-low');
    const renderedConfidenceHigh = renderedDataAttribute(landingHTML, 'data-recall-ci-high');
    if (validConfidence) {
      if (Number(renderedConfidenceLow) !== Number(topRanked.recall_ci_low) || Number(renderedConfidenceHigh) !== Number(topRanked.recall_ci_high)) {
        failures.push('Friendly DeepORG confidence range does not match deeporg.yaml');
      }
    } else if (renderedConfidenceLow !== undefined || renderedConfidenceHigh !== undefined) {
      failures.push('Friendly DeepORG renders an invalid or unavailable confidence range');
    }
    if (!landingHTML.includes('https://graphjin.com/og/deeporg-og.png')) {
      failures.push('Friendly DeepORG page does not reference its dedicated social card');
    }
    const landingComparisonRuns = [
      ...rankedRuns.map((run) => ({ run, prior: false })),
      ...contextRuns.map((run) => ({ run, prior: true })),
    ];
    for (const { run, prior } of landingComparisonRuns) {
      const escapedRunID = String(run.run_id).replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&');
      const marker = new RegExp(`data-benchmark-comparison-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).exec(landingHTML);
      if (!marker) {
        failures.push(`Friendly DeepORG comparison is missing ${run.run_id}`);
        continue;
      }
      const linkStart = landingHTML.lastIndexOf('<a', marker.index);
      const linkEnd = landingHTML.indexOf('>', marker.index);
      const linkTag = landingHTML.slice(linkStart, linkEnd + 1);
      const linkPriorCohort = renderedDataAttribute(linkTag, 'data-benchmark-prior-cohort');
      if (prior && linkPriorCohort !== String(run.generation)) {
        failures.push(`Friendly DeepORG prior-cohort marker for ${run.run_id} does not match deeporg.yaml`);
      }
      if (!prior && linkPriorCohort !== undefined) {
        failures.push(`Friendly DeepORG ranked run ${run.run_id} must not carry a prior-cohort marker`);
      }
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
      if (String(renderedDataAttribute(linkTag, 'data-generation')) !== String(run.generation)) {
        failures.push(`Friendly DeepORG generation for ${run.run_id} does not match deeporg.yaml`);
      }
      const expectedReport = `/benchmarks/deeporg/runs/${run.slug}/`;
      if (renderedDataAttribute(linkTag, 'href') !== expectedReport) {
        failures.push(`Friendly DeepORG technical report link for ${run.run_id} is incomplete`);
      }
    }
    const renderedTaskCounts = [...landingHTML.matchAll(/data-benchmark-task-count=(?:"(\d+)"|'(\d+)'|(\d+))/g)]
      .map((match) => Number(match[1] ?? match[2] ?? match[3]));
    const publishedTaskCount = Object.values(parsed.suite.category_counts ?? {}).reduce((total, value) => total + Number(value), 0);
    if (renderedTaskCounts.reduce((total, value) => total + value, 0) !== publishedTaskCount) {
      failures.push('Friendly DeepORG task group counts do not add up to the published suite');
    }
    if (Number(renderedDataAttribute(landingHTML, 'data-benchmark-episode-count')) !== Number(topRanked.episode_count ?? 0)) {
      failures.push('Friendly DeepORG safety story episode count does not match deeporg.yaml');
    }
  }

  const benchmarkOGPath = path.join(publicRoot, 'og', 'index.html');
  if (topRanked && (await exists(benchmarkOGPath))) {
    const benchmarkOG = await readFile(benchmarkOGPath, 'utf8');
    if (String(renderedDataAttribute(benchmarkOG, 'data-benchmark-generation')) !== String(comparisonGeneration)) {
      failures.push('DeepORG social card generation does not match deeporg.yaml');
    }
    if (String(renderedDataAttribute(benchmarkOG, 'data-benchmark-og-run')) !== String(topRanked.run_id)) {
      failures.push('DeepORG social card does not use the top ranked run');
    }
    if (String(renderedDataAttribute(benchmarkOG, 'data-model-label')) !== String(topRanked.label)) {
      failures.push('DeepORG social card model label does not match deeporg.yaml');
    }
    if (Number(renderedDataAttribute(benchmarkOG, 'data-recall')) !== Number(topRanked.recall) ||
        Number(renderedDataAttribute(benchmarkOG, 'data-unsafe-effects')) !== Number(topRanked.unsafe_effects ?? 0) ||
        Number(renderedDataAttribute(benchmarkOG, 'data-task-count')) !== Number(topRanked.task_count ?? 0)) {
      failures.push('DeepORG social card metrics do not match deeporg.yaml');
    }
  }
  if (rankedRuns.length > 0 && (await exists(benchmarkIndexPath))) {
    const benchmarkHTML = await readFile(benchmarkIndexPath, 'utf8');
    if (String(renderedDataAttribute(benchmarkHTML, 'data-benchmark-generation-current')) !== String(comparisonGeneration)) {
      failures.push(`DeepORG comparison generation marker does not match deeporg.yaml (${comparisonGeneration})`);
    }
    const renderedScopeLabel = String(parsed.suite.scope_label ?? '').replaceAll('&', '&amp;');
    if (!benchmarkHTML.includes(renderedScopeLabel)) {
      failures.push('DeepORG board is missing its generation scope label');
    }
    for (const [generation, scope] of Object.entries(parsed.suite.generation_scopes ?? {})) {
      const renderedGenerationScope = `Generation ${generation} — ${String(scope).replaceAll('&', '&amp;')}`;
      if (!benchmarkHTML.includes(renderedGenerationScope)) {
        failures.push(`DeepORG board is missing the scope for generation ${generation}`);
      }
    }
    if (comparisonGeneration !== parsed.suite.generation && !benchmarkHTML.includes('data-benchmark-rerun-pending')) {
      failures.push('DeepORG board is missing the current-generation retraction notice');
    }
    if (!benchmarkHTML.includes('data-benchmark-generation-history')) {
      failures.push('DeepORG board is missing collapsed prior-generation history');
    }
    for (const generation of new Set(parsed.runs.filter((run) => run.ranked !== true).map((run) => String(run.generation)))) {
      const escapedGeneration = generation.replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&');
      const historyPattern = new RegExp(`data-benchmark-history-group=(?:"${escapedGeneration}"|'${escapedGeneration}'|${escapedGeneration})(?=\\s|>)`);
      if (!historyPattern.test(benchmarkHTML)) {
        failures.push(`DeepORG board is missing collapsed history group ${generation}`);
      }
    }
    for (const run of rankedRuns) {
      const escapedRunID = String(run.run_id).replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&');
      const chartMarker = new RegExp(`data-benchmark-chart-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).exec(benchmarkHTML);
      if (!chartMarker) {
        failures.push(`DeepORG chart is missing ${run.run_id}`);
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
      }
      for (const [category, expectedValue] of Object.entries(run.category_recall ?? {})) {
        const escapedCategory = category.replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&');
        const categoryPattern = new RegExp(`data-category-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})\\s+data-category=(?:"${escapedCategory}"|'${escapedCategory}'|${escapedCategory})\\s+data-recall=(?:"([^"]*)"|'([^']*)'|([^\\s>]+))`);
        const match = categoryPattern.exec(benchmarkHTML);
        const rendered = match ? Number(match[1] ?? match[2] ?? match[3]) : Number.NaN;
        if (!Number.isFinite(rendered) || Math.abs(rendered - Number(expectedValue)) > 1e-12) {
          failures.push(`DeepORG category chart ${category} for ${run.run_id} does not match deeporg.yaml (${rendered} != ${expectedValue})`);
        }
      }
      // Capability rollups: the chart's data attributes must match the row,
      // and the row must match what the frozen mapping computes from the
      // row's own category scores and the suite's task counts — the
      // assertion that keeps rollups derived, never hand-typed.
      for (const [rollup, expectedValue] of Object.entries(run.rollup_recall ?? {})) {
        const chartTag = new RegExp(`<g[^>]*data-benchmark-chart-run=(?:"${escapedRunID}"|'${escapedRunID}')[^>]*>`).exec(benchmarkHTML)?.[0] ?? '';
        const rendered = Number(renderedDataAttribute(chartTag, `data-rollup-${rollup}`));
        if (!Number.isFinite(rendered) || Math.abs(rendered - Number(expectedValue)) > 1e-12) {
          failures.push(`DeepORG chart rollup ${rollup} for ${run.run_id} does not match deeporg.yaml (${rendered} != ${expectedValue})`);
        }
      }
      if (run.rollup_recall && run.ranked && parsed.suite?.rollup_map && parsed.suite?.category_counts) {
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
            failures.push(`DeepORG rollup ${group} for ${run.run_id} disagrees with its category scores (${expectedValue} != derived ${derived})`);
          }
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
          failures.push(`Benchmark efficiency ${field} for ${run.run_id} does not match deeporg.yaml (${rendered ?? 'missing'} != ${expected})`);
        }
      }

      const scoreRowPattern = new RegExp(`data-benchmark-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).exec(benchmarkHTML);
      if (!scoreRowPattern) {
        failures.push(`Benchmark leaderboard is missing governance data for ${run.run_id}`);
      } else {
        const scoreRowStart = benchmarkHTML.lastIndexOf('<tr', scoreRowPattern.index);
        const scoreRowEnd = benchmarkHTML.indexOf('>', scoreRowPattern.index);
        const scoreRowTag = benchmarkHTML.slice(scoreRowStart, scoreRowEnd + 1);
        for (const [field, expectedValue] of governanceFields) {
          const rendered = renderedDataAttribute(scoreRowTag, field);
          const expected = Number(expectedValue(run));
          if (rendered === undefined || Number(rendered) !== expected) {
            failures.push(`Benchmark governance ${field} for ${run.run_id} does not match deeporg.yaml (${rendered ?? 'missing'} != ${expected})`);
          }
        }
      }

      const runPagePath = path.join(publicRoot, 'benchmarks', 'deeporg', 'runs', String(run.slug), 'index.html');
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
            failures.push(`Benchmark run summary ${field} for ${run.run_id} does not match deeporg.yaml (${rendered ?? 'missing'} != ${expected})`);
          }
        }
        for (const [field, expectedValue] of governanceFields) {
          const rendered = renderedDataAttribute(summaryTag, field);
          const expected = Number(expectedValue(run));
          if (rendered === undefined || Number(rendered) !== expected) {
            failures.push(`Benchmark run summary ${field} for ${run.run_id} does not match deeporg.yaml (${rendered ?? 'missing'} != ${expected})`);
          }
        }
        for (const [category, expectedValue] of Object.entries(run.category_recall ?? {})) {
          const escapedCategory = category.replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&');
          const categoryPattern = new RegExp(`data-category=(?:"${escapedCategory}"|'${escapedCategory}'|${escapedCategory})\\s+data-recall=(?:"([^"]*)"|'([^']*)'|([^\\s>]+))`);
          const match = categoryPattern.exec(runPage);
          const rendered = match ? Number(match[1] ?? match[2] ?? match[3]) : Number.NaN;
          if (!Number.isFinite(rendered) || Math.abs(rendered - Number(expectedValue)) > 1e-12) {
            failures.push(`DeepORG run category ${category} for ${run.run_id} does not match deeporg.yaml (${rendered} != ${expectedValue})`);
          }
        }
        for (const [rollup, expectedValue] of Object.entries(run.rollup_recall ?? {})) {
          const escapedRollup = rollup.replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&');
          const rollupPattern = new RegExp(`data-rollup=(?:"${escapedRollup}"|'${escapedRollup}'|${escapedRollup})\\s+data-rollup-recall=(?:"([^"]*)"|'([^']*)'|([^\\s>]+))`);
          const match = rollupPattern.exec(runPage);
          const rendered = match ? Number(match[1] ?? match[2] ?? match[3]) : Number.NaN;
          if (!Number.isFinite(rendered) || Math.abs(rendered - Number(expectedValue)) > 1e-12) {
            failures.push(`DeepORG run rollup ${rollup} for ${run.run_id} does not match deeporg.yaml (${rendered} != ${expectedValue})`);
          }
        }
      }
    }

    if (contextRuns.length > 0 && !benchmarkHTML.includes('data-benchmark-prior-note')) {
      failures.push('DeepORG board is missing the prior-cohort context note');
    }
    for (const run of contextRuns) {
      const escapedRunID = String(run.run_id).replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&');
      const chartMarker = new RegExp(`data-benchmark-chart-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).exec(benchmarkHTML);
      if (!chartMarker) {
        failures.push(`DeepORG chart is missing prior-cohort run ${run.run_id}`);
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
            failures.push(`DeepORG chart ${field} for prior-cohort ${run.run_id} does not match deeporg.yaml (${rendered} != ${expected})`);
          }
        }
        if (renderedDataAttribute(chartTag, 'data-benchmark-prior-cohort') !== String(run.generation)) {
          failures.push(`DeepORG chart prior-cohort marker for ${run.run_id} does not match deeporg.yaml`);
        }
      }
      // Family bars compare only same-ruler runs; a prior-cohort row in the
      // categories chart would present a superseded ruler as comparable.
      if (new RegExp(`data-category-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).test(benchmarkHTML)) {
        failures.push(`DeepORG category chart must not include prior-cohort run ${run.run_id}`);
      }
      const efficiencyMarker = new RegExp(`data-benchmark-efficiency-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).exec(benchmarkHTML);
      if (!efficiencyMarker) {
        failures.push(`Benchmark leaderboard is missing efficiency data for prior-cohort ${run.run_id}`);
      } else {
        const rowStart = benchmarkHTML.lastIndexOf('<tr', efficiencyMarker.index);
        const rowEnd = benchmarkHTML.indexOf('>', efficiencyMarker.index);
        const rowTag = benchmarkHTML.slice(rowStart, rowEnd + 1);
        for (const [field, expectedValue] of efficiencyFields) {
          const rendered = renderedDataAttribute(rowTag, field);
          const expected = Number(expectedValue(run));
          if (rendered === undefined || Number(rendered) !== expected) {
            failures.push(`Benchmark efficiency ${field} for prior-cohort ${run.run_id} does not match deeporg.yaml (${rendered ?? 'missing'} != ${expected})`);
          }
        }
        if (renderedDataAttribute(rowTag, 'data-benchmark-prior-cohort') !== String(run.generation)) {
          failures.push(`Benchmark efficiency prior-cohort marker for ${run.run_id} does not match deeporg.yaml`);
        }
      }
      const scoreRowMarker = new RegExp(`data-benchmark-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).exec(benchmarkHTML);
      if (!scoreRowMarker) {
        failures.push(`Benchmark leaderboard is missing governance data for prior-cohort ${run.run_id}`);
      } else {
        const scoreRowStart = benchmarkHTML.lastIndexOf('<tr', scoreRowMarker.index);
        const scoreRowEnd = benchmarkHTML.indexOf('>', scoreRowMarker.index);
        const scoreRowTag = benchmarkHTML.slice(scoreRowStart, scoreRowEnd + 1);
        for (const [field, expectedValue] of governanceFields) {
          const rendered = renderedDataAttribute(scoreRowTag, field);
          const expected = Number(expectedValue(run));
          if (rendered === undefined || Number(rendered) !== expected) {
            failures.push(`Benchmark governance ${field} for prior-cohort ${run.run_id} does not match deeporg.yaml (${rendered ?? 'missing'} != ${expected})`);
          }
        }
        if (renderedDataAttribute(scoreRowTag, 'data-benchmark-prior-cohort') !== String(run.generation)) {
          failures.push(`Benchmark leaderboard prior-cohort marker for ${run.run_id} does not match deeporg.yaml`);
        }
      }
    }
  }

  const homePath = path.join(publicRoot, 'index.html');
  if (topRanked && (await exists(homePath))) {
    const home = await readFile(homePath, 'utf8');
    const benchmarkPage = await readFile(benchmarkIndexPath, 'utf8');
    if (!home.includes('data-benchmark-proof')) {
      failures.push('Homepage is missing the data-driven benchmark proof module');
    }
    const escapedRunID = String(topRanked.run_id).replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const chartMarker = new RegExp(`data-benchmark-chart-run=(?:"${escapedRunID}"|'${escapedRunID}'|${escapedRunID})(?=\\s|>)`).exec(home);
    if (!chartMarker) {
      failures.push('Homepage benchmark chart does not include the top ranked run from deeporg.yaml');
    } else {
      const chartStart = home.lastIndexOf('<div', chartMarker.index);
      const chartEnd = home.indexOf('>', chartMarker.index);
      const chartTag = home.slice(chartStart, chartEnd + 1);
      const reliablePasses = Number(topRanked.task_count ?? 0) * Number(topRanked.recall ?? 0);
      const expected = [
        ['data-recall', Number(topRanked.recall ?? 0)],
        ['data-cost-per-reliable-pass-usd', reliablePasses > 0 ? Number(topRanked.estimated_list_cost_usd ?? 0) / reliablePasses : 0],
        ['data-latency-p50-ms', Number(topRanked.latency_p50_ms ?? 0)],
        ['data-unsafe-effects', Number(topRanked.unsafe_effects ?? 0)],
        ['data-task-count', Number(topRanked.task_count ?? 0)],
        ['data-repeats', Number(topRanked.repeats ?? 0)],
      ];
      for (const [field, value] of expected) {
        const rendered = Number(renderedDataAttribute(chartTag, field));
        if (!Number.isFinite(rendered) || Math.abs(rendered - value) > 1e-12) {
          failures.push(`Homepage benchmark ${field} does not match deeporg.yaml (${rendered} != ${value})`);
        }
      }
    }
    if (String(renderedDataAttribute(home, 'data-benchmark-generation')) !== String(comparisonGeneration)) {
      failures.push('Homepage benchmark proof generation does not match deeporg.yaml');
    }
    // The hero states the benchmark claims as numbers. They render from the
    // yaml through the benchmark-hero shortcode; assert them here so the
    // homepage can never outrun the published board.
    const heroMarker = /<[^>]*data-benchmark-hero[^>]*>/.exec(home);
    if (!heroMarker) {
      failures.push('Homepage hero is missing the data-driven benchmark-hero module');
    } else {
      const heroTag = heroMarker[0];
      const heroAttempts = rankedRuns.reduce((total, run) => total + Number(run.episode_count ?? 0), 0);
      const heroUnsafe = rankedRuns.reduce((total, run) => total + Number(run.unsafe_effects ?? 0), 0);
      const heroExpected = [
        ['data-hero-models', rankedRuns.length],
        ['data-hero-attempts', heroAttempts],
        ['data-hero-unsafe', heroUnsafe],
        ['data-hero-tasks', Number(topRanked.task_count ?? 0)],
        ['data-hero-repeats', Number(parsed.suite.repeats ?? 0)],
      ];
      for (const [field, value] of heroExpected) {
        const rendered = Number(renderedDataAttribute(heroTag, field));
        if (!Number.isFinite(rendered) || rendered !== value) {
          failures.push(`Homepage hero ${field} does not match deeporg.yaml (${rendered} != ${value})`);
        }
      }
      // Truth guard: a hardcoded zero anywhere on the homepage becomes a lie
      // the moment any ranked run reports an unsafe effect.
      if (heroUnsafe !== 0 && /\b0 unsafe effects\b/.test(home)) {
        failures.push(`Homepage claims 0 unsafe effects but the ranked cohort reports ${heroUnsafe}`);
      }
      // Staleness guard: the hero and proof module say no frontier model has
      // been run. The moment one is ranked, that copy must change by hand.
      const frontierPattern = /opus|gpt-5|ultra|-pro\b/i;
      for (const run of rankedRuns) {
        const identity = `${run.label ?? ''} ${run.model ?? ''}`;
        if (frontierPattern.test(identity)) {
          failures.push(`Ranked model "${run.label ?? run.model}" looks like a frontier model; update the no-frontier hero/proof copy (or the frontier pattern in check-site)`);
        }
      }
    }
    const storyMarker = renderedDataAttribute(benchmarkPage, 'data-current-recall');
    if (storyMarker === undefined || Number(storyMarker) !== Number(topRanked.recall)) {
      failures.push(`DeepORG generation story recall does not match deeporg.yaml (${storyMarker ?? 'missing'} != ${topRanked.recall})`);
    }
    const storySafety = renderedDataAttribute(benchmarkPage, 'data-current-unsafe-effects');
    if (storySafety === undefined || Number(storySafety) !== Number(topRanked.unsafe_effects ?? 0)) {
      failures.push(`DeepORG generation story unsafe effects do not match deeporg.yaml (${storySafety ?? 'missing'} != ${topRanked.unsafe_effects ?? 0})`);
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

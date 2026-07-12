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
  'configure/sources-mode/index.html',
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
  'proof',
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
  'agentic/catalog-graph.md',
  'agentic/security-graph.md',
  'agentic/source-mode.md',
  'agentic/workflows.md',
  'agentic/oauth.md',
  'configure/sources-mode.md',
  'configure/database.md',
  'configure/auth-rbac.md',
  'configure/caching-redis.md',
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
  for (const required of ['Vision', 'The agent loop', 'One instruction in', 'The ledger caught it', 'Five demos. Real domains.', 'geo filters', 'Expression aggregates', 'backed by evidence', 'one governed graph', 'Run the 2-minute demo']) {
    if (!home.includes(required)) {
      failures.push(`Homepage missing required enriched copy: ${required}`);
    }
  }
  for (const socialMeta of ['og:title', 'og:image', 'twitter:card']) {
    const socialPattern = new RegExp(`<meta\\s[^>]*(?:property|name)=(["'])?${socialMeta}\\1(?=[\\s/>])`);
    if (!socialPattern.test(home)) {
      failures.push(`Homepage missing social meta ${socialMeta}`);
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

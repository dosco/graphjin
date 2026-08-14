const themeStorageKey = 'graphjin-theme';
const themeModes = ['auto', 'light', 'dark'];
const themeLabels = { auto: 'System', light: 'Light', dark: 'Dark' };

function readTheme() {
  try {
    const mode = localStorage.getItem(themeStorageKey) || 'auto';
    return themeModes.includes(mode) ? mode : 'auto';
  } catch {
    return 'auto';
  }
}

function writeTheme(mode) {
  try {
    localStorage.setItem(themeStorageKey, mode);
  } catch {}
}

function nextThemeMode(mode) {
  const index = themeModes.indexOf(mode);
  return themeModes[(index + 1) % themeModes.length] || 'auto';
}

function applyTheme(mode) {
  const resolved = themeModes.includes(mode) ? mode : 'auto';
  if (resolved === 'auto') {
    document.documentElement.removeAttribute('data-theme');
  } else {
    document.documentElement.dataset.theme = resolved;
  }

  for (const control of document.querySelectorAll('[data-theme-toggle]')) {
    const next = nextThemeMode(resolved);
    control.dataset.themeMode = resolved;
    control.setAttribute(
      'aria-label',
      `Theme: ${themeLabels[resolved]}. Switch to ${themeLabels[next]} theme`
    );
    control.setAttribute('title', `Theme: ${themeLabels[resolved]}`);
  }
}

function setTheme(mode) {
  writeTheme(mode);
  applyTheme(mode);
}

applyTheme(readTheme());

for (const button of document.querySelectorAll('[data-theme-toggle]')) {
  button.addEventListener('click', () => {
    setTheme(nextThemeMode(readTheme()));
  });
}

for (const menu of document.querySelectorAll('[data-site-menu]')) {
  for (const link of menu.querySelectorAll('a')) {
    link.addEventListener('click', () => {
      menu.open = false;
    });
  }
}

document.addEventListener('click', (event) => {
  for (const menu of document.querySelectorAll('[data-site-menu]')) {
    if (!menu.contains(event.target)) menu.open = false;
  }
});

document.addEventListener('keydown', (event) => {
  if (event.key !== 'Escape') return;
  for (const menu of document.querySelectorAll('[data-site-menu]')) {
    menu.open = false;
  }
});

const mobileDocs = document.querySelector('[data-mobile-docs]');
for (const link of mobileDocs?.querySelectorAll('a') ?? []) {
  link.addEventListener('click', () => {
    mobileDocs.open = false;
  });
}

function writeClipboardSync(text) {
  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', '');
  textarea.style.position = 'fixed';
  textarea.style.left = '-9999px';
  document.body.append(textarea);
  textarea.focus();
  textarea.select();
  textarea.setSelectionRange(0, text.length);
  const copied = document.execCommand('copy');
  textarea.remove();
  return copied;
}

async function writeClipboard(text) {
  if (writeClipboardSync(text)) return;
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text);
    return;
  }
  throw new Error('copy failed');
}

function selectCodeText(code) {
  const selection = window.getSelection();
  const range = document.createRange();
  range.selectNodeContents(code);
  selection?.removeAllRanges();
  selection?.addRange(range);
}

// Delegated so copy buttons keep working inside content appended after load
// (the docs infinite scroll injects whole articles).
document.addEventListener('click', async (event) => {
  const target = event.target instanceof Element ? event.target : null;
  const button = target?.closest('[data-copy-code]');
  if (!button) return;
  const block = button.closest('[data-code-block]');
  const code = block?.querySelector('pre code, pre');
  if (!code) return;
  const original = button.textContent || 'Copy';
  try {
    await writeClipboard(code.textContent || '');
    button.textContent = 'Copied';
    button.dataset.copied = 'true';
  } catch {
    selectCodeText(code);
    button.textContent = 'Selected';
    button.dataset.copied = 'selected';
  }
  window.setTimeout(() => {
    button.textContent = original;
    delete button.dataset.copied;
  }, 1600);
});

const searchRoot = document.querySelector('[data-site-search]');
const searchInput = searchRoot?.querySelector('[data-search-input]');
const searchResults = searchRoot?.querySelector('[data-search-results]');
const sectionLabels = {
  agentic: 'Agentic',
  configure: 'Configure',
  core: 'Core',
  home: 'Home',
  integrations: 'Integrations',
  reference: 'Reference',
  start: 'Start',
};
const kindLabels = {
  concept: 'Concept',
  guide: 'Guide',
  reference: 'Reference',
};
const SEARCH_VARIANT_LIMIT = 6;
const SEARCH_RESULT_POOL_LIMIT = 18;
const SEARCH_RESULT_DISPLAY_LIMIT = 9;
const SEARCH_ANCHOR_DISPLAY_LIMIT = 3;
let pagefindModule;
let searchTimer;
let searchRequestID = 0;
let activeSearchIndex = -1;
let activeSearchItems = [];
let searchView;

function escapeHTML(value) {
  return String(value ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

function sanitizePagefindHTML(value) {
  const template = document.createElement('template');
  template.innerHTML = String(value ?? '');
  for (const element of template.content.querySelectorAll('*')) {
    if (element.localName === 'mark') {
      for (const attr of [...element.attributes]) {
        element.removeAttribute(attr.name);
      }
      continue;
    }
    element.replaceWith(document.createTextNode(element.textContent || ''));
  }
  return template.innerHTML;
}

function stripHTML(value) {
  return String(value ?? '').replaceAll(/<[^>]*>/g, ' ');
}

function splitCamelCase(value) {
  return String(value ?? '')
    .replaceAll(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replaceAll(/([A-Z])([A-Z][a-z])/g, '$1 $2');
}

function normalizeSearchText(value) {
  return stripHTML(value)
    .replaceAll('&lt;', '<')
    .replaceAll('&gt;', '>')
    .replaceAll('&amp;', '&')
    .replaceAll(/([a-z0-9])([A-Z])/g, '$1 $2')
    .toLowerCase()
    .replaceAll(/[^a-z0-9@/_+.#-]+/g, ' ')
    .replaceAll(/\s+/g, ' ')
    .trim();
}

function compactSearchText(value) {
  return normalizeSearchText(value).replaceAll(/[^a-z0-9]+/g, '');
}

function queryFragments(query) {
  return splitCamelCase(query)
    .toLowerCase()
    .replaceAll(/[^a-z0-9]+/g, ' ')
    .split(/\s+/)
    .map((term) => term.trim())
    .filter((term) => term.length > 1);
}

function queryTerms(query) {
  const terms = new Set(
    normalizeSearchText(query)
      .split(/\s+/)
      .map((term) => term.trim())
      .filter((term) => term.length > 1)
  );
  for (const term of queryFragments(query)) terms.add(term);
  return [...terms];
}

function searchVariants(query) {
  const raw = query.trim();
  const variants = new Set([raw]);
  const noParens = raw
    .replaceAll(/\(\s*(?:\.\.\.)?\s*\)/g, '')
    .replaceAll(/[()]/g, '')
    .trim();
  const spaced = splitCamelCase(noParens)
    .replaceAll(/[@/._-]+/g, ' ')
    .replaceAll(/\s+/g, ' ')
    .trim();
  const compact = noParens.replaceAll(/[@/._\s-]+/g, '').trim();
  const packageish = noParens.replace(/^@/, '').replaceAll(/[/.]+/g, ' ').trim();
  const hyphenated = splitCamelCase(noParens)
    .replaceAll(/[@/._\s]+/g, '-')
    .replaceAll(/-+/g, '-')
    .replaceAll(/^-|-$/g, '')
    .trim();
  const dotted = splitCamelCase(noParens)
    .replaceAll(/[@/_\s-]+/g, '.')
    .replaceAll(/\.+/g, '.')
    .replaceAll(/^\.|\.$/g, '')
    .trim();

  for (const candidate of [
    noParens,
    spaced,
    compact,
    packageish,
    hyphenated,
    dotted,
  ]) {
    if (candidate && candidate.toLowerCase() !== raw.toLowerCase()) {
      variants.add(candidate);
    }
  }

  return [...variants].slice(0, SEARCH_VARIANT_LIMIT);
}

function searchContext() {
  return { options: {}, scopeLabel: 'GraphJin docs' };
}

function setSearchExpanded(expanded) {
  if (!searchInput || !searchResults) return;
  searchInput.setAttribute('aria-expanded', String(expanded));
  if (!expanded) {
    searchInput.removeAttribute('aria-activedescendant');
  }
}

function hideSearchResults() {
  if (!searchResults) return;
  searchResults.hidden = true;
  activeSearchIndex = -1;
  activeSearchItems = [];
  searchView = undefined;
  setSearchExpanded(false);
}

function setSearchStatus(text) {
  if (!searchResults) return;
  searchResults.hidden = false;
  searchResults.innerHTML = `<div class="search-status" role="status">${escapeHTML(text)}</div>`;
  setSearchExpanded(true);
}

async function loadPagefind() {
  if (pagefindModule) return pagefindModule;
  pagefindModule = await import('/pagefind/pagefind.js');
  await pagefindModule.options?.({
    excerptLength: 34,
    highlightParam: 'highlight',
    ranking: {
      pageLength: 0.55,
      termFrequency: 0.72,
      termSimilarity: 1,
      termSaturation: 1,
      metaWeights: {
        title: 8,
        slug: 6,
        source: 5,
        kind: 3,
        description: 2,
        section: 2,
      },
    },
  });
  return pagefindModule;
}

let searchWarm;
function warmSearch() {
  searchWarm ??= loadPagefind()
    .then((pagefind) => pagefind.init?.())
    .catch(() => {
      searchWarm = undefined;
    });
}
searchRoot?.addEventListener('pointerenter', warmSearch);
searchInput?.addEventListener('keydown', warmSearch);

function cleanResultURL(url, { keepHash = true } = {}) {
  try {
    const parsed = new URL(url, window.location.origin);
    parsed.searchParams.delete('highlight');
    if (!keepHash) parsed.hash = '';
    return `${parsed.pathname}${parsed.search}${parsed.hash}`;
  } catch {
    return String(url ?? '').split('?highlight=')[0];
  }
}

function pageKey(url) {
  return cleanResultURL(url, { keepHash: false });
}

function titleCase(value) {
  return String(value ?? '')
    .replaceAll(/[-_]+/g, ' ')
    .replaceAll(/\b\w/g, (match) => match.toUpperCase());
}

function formatSection(section) {
  return sectionLabels[section] || titleCase(section || 'Docs');
}

function formatKind(kind) {
  return kindLabels[kind] || titleCase(kind || '');
}

function compactSource(source) {
  const text = String(source ?? '').trim();
  if (!text) return '';
  const parts = text.split('/');
  if (parts.length <= 2) return text;
  return parts.slice(-2).join('/');
}

function sourceSymbol(source) {
  const filename =
    String(source ?? '')
      .split('/')
      .pop() || '';
  return filename.replaceAll(/\.md$/g, '');
}

function candidateText(candidate) {
  return [
    candidate.title,
    candidate.pageTitle,
    candidate.plainExcerpt,
    candidate.meta?.description,
    candidate.meta?.kind,
    candidate.meta?.slug,
    candidate.meta?.source,
    candidate.meta?.section,
  ].join(' ');
}

function scoreCandidate(candidate, query, resultMeta) {
  const normalizedQuery = normalizeSearchText(query);
  const compactQuery = compactSearchText(query);
  const title = normalizeSearchText(candidate.title);
  const pageTitle = normalizeSearchText(candidate.pageTitle);
  const kind = normalizeSearchText(candidate.meta?.kind);
  const slug = normalizeSearchText(candidate.meta?.slug);
  const source = normalizeSearchText(candidate.meta?.source);
  const symbol = normalizeSearchText(sourceSymbol(candidate.meta?.source));
  const text = normalizeSearchText(candidateText(candidate));
  const compactTitle = compactSearchText(candidate.title);
  const compactPageTitle = compactSearchText(candidate.pageTitle);
  const compactSymbol = compactSearchText(sourceSymbol(candidate.meta?.source));
  const compactSlug = compactSearchText(candidate.meta?.slug);
  const compactSourceText = compactSearchText(candidate.meta?.source);
  const terms = queryTerms(query);
  const fragments = queryFragments(query);
  const matchedMetaFields = resultMeta.matchedMetaFields ?? [];

  let score = Math.log1p(resultMeta.score || 0) * 16;
  score -= resultMeta.variantIndex * 5;
  score -= resultMeta.resultIndex * 0.35;

  if (title === normalizedQuery) score += 92;
  else if (title.includes(normalizedQuery)) score += 58;
  if (pageTitle === normalizedQuery) score += 60;
  else if (pageTitle.includes(normalizedQuery)) score += 34;
  if (symbol === normalizedQuery) score += 72;
  else if (symbol.includes(normalizedQuery)) score += 38;

  if (compactQuery) {
    if (compactTitle === compactQuery) score += 86;
    else if (compactTitle.includes(compactQuery)) score += 46;
    if (compactPageTitle === compactQuery) score += 46;
    else if (compactPageTitle.includes(compactQuery)) score += 24;
    if (compactSymbol === compactQuery) score += 72;
    else if (compactSymbol.includes(compactQuery)) score += 36;
    if (compactSlug.includes(compactQuery)) score += 38;
    if (compactSourceText.includes(compactQuery)) score += 34;
  }

  if (slug.includes(normalizedQuery)) score += 24;
  if (source.includes(normalizedQuery)) score += 22;
  if (candidate.isSubResult) score += 10;
  if (matchedMetaFields.includes('title')) score += 18;
  if (matchedMetaFields.includes('slug')) score += 14;
  if (matchedMetaFields.includes('source')) score += 12;
  if (matchedMetaFields.includes('kind')) score += 8;

  if (terms.length) {
    const titleHits = terms.filter((term) => title.includes(term)).length;
    const symbolHits = terms.filter((term) => symbol.includes(term)).length;
    const sourceHits = terms.filter((term) => source.includes(term)).length;
    const slugHits = terms.filter((term) => slug.includes(term)).length;
    const textHits = terms.filter((term) => text.includes(term)).length;
    score +=
      titleHits * 9 +
      symbolHits * 7 +
      sourceHits * 4 +
      slugHits * 4 +
      textHits * 2;
    if (titleHits === terms.length) score += 18;
    if (symbolHits === terms.length) score += 14;
    if (sourceHits === terms.length) score += 10;
    if (slugHits === terms.length) score += 10;
    if (kind && terms.includes(kind)) score += 8;
  }

  if (fragments.length > 1) {
    const compactFragments = fragments.join('');
    if (compactTitle.includes(compactFragments)) score += 24;
    if (compactSymbol.includes(compactFragments)) score += 20;
    if (compactSourceText.includes(compactFragments)) score += 18;
    if (compactSlug.includes(compactFragments)) score += 18;
  }

  return score;
}

function buildCandidates(data) {
  const meta = data.meta ?? {};
  const pageTitle = meta.title || data.title || data.url;
  const baseCandidate = {
    title: pageTitle,
    pageTitle,
    url: data.url,
    excerpt: data.excerpt,
    plainExcerpt: data.plain_excerpt,
    meta,
    isSubResult: false,
  };
  const subCandidates = (data.sub_results ?? [])
    .slice(0, 6)
    .map((subResult) => ({
      title: subResult.title || pageTitle,
      pageTitle,
      url: subResult.url || data.url,
      excerpt: subResult.excerpt || data.excerpt,
      plainExcerpt: subResult.plain_excerpt || data.plain_excerpt,
      meta,
      isSubResult: Boolean(subResult.url && subResult.url !== data.url),
    }));

  return [baseCandidate, ...subCandidates];
}

async function searchPagefind(pagefind, query, requestID, context) {
  const variants = searchVariants(query);
  const { options } = context;
  await Promise.allSettled(
    variants.map((variant) => pagefind.preload?.(variant, options))
  );

  const primarySearch = pagefind.debouncedSearch
    ? await pagefind.debouncedSearch(variants[0], options, 90)
    : await pagefind.search(variants[0], options);
  if (requestID !== searchRequestID || primarySearch === null) return null;

  const extraSearches = await Promise.all(
    variants.slice(1).map((variant) => pagefind.search(variant, options))
  );
  if (requestID !== searchRequestID) return null;

  return [primarySearch, ...extraSearches].flatMap((search, variantIndex) =>
    (search?.results ?? [])
      .slice(0, SEARCH_RESULT_POOL_LIMIT)
      .map((result, resultIndex) => ({
        result,
        resultIndex,
        variantIndex,
      }))
  );
}

async function loadSearchItems(rawResults, query, requestID) {
  const loaded = await Promise.allSettled(
    rawResults.map(async (item) => ({
      item,
      data: await item.result.data(),
    }))
  );
  if (requestID !== searchRequestID) return [];

  const groupsByPage = new Map();
  for (const loadedResult of loaded) {
    if (loadedResult.status !== 'fulfilled') continue;
    const { item, data } = loadedResult.value;
    for (const candidate of buildCandidates(data)) {
      const score = scoreCandidate(candidate, query, {
        matchedMetaFields: item.result.matchedMetaFields,
        resultIndex: item.resultIndex,
        score: item.result.score,
        variantIndex: item.variantIndex,
      });
      const key = pageKey(candidate.url);
      let group = groupsByPage.get(key);
      if (!group) {
        group = {
          score: Number.NEGATIVE_INFINITY,
          page: undefined,
          anchors: new Map(),
        };
        groupsByPage.set(key, group);
      }
      group.score = Math.max(group.score, score);
      if (candidate.isSubResult) {
        const anchorKey = cleanResultURL(candidate.url);
        const previous = group.anchors.get(anchorKey);
        if (!previous || score > previous.score) {
          group.anchors.set(anchorKey, { ...candidate, score });
        }
      } else if (!group.page || score > group.page.score) {
        group.page = { ...candidate, score };
      }
    }
  }

  return [...groupsByPage.values()]
    .map((group) => {
      const anchors = [...group.anchors.values()]
        .sort((a, b) => b.score - a.score)
        .slice(0, SEARCH_ANCHOR_DISPLAY_LIMIT);
      const page = group.page ?? {
        ...anchors[0],
        url: pageKey(anchors[0].url),
        title: anchors[0].pageTitle,
      };
      const best = [page, ...anchors].reduce((a, b) =>
        b.score > a.score ? b : a
      );
      return {
        page,
        anchors,
        score: group.score,
        excerpt: best.excerpt,
        plainExcerpt: best.plainExcerpt,
      };
    })
    .sort((a, b) => b.score - a.score)
    .slice(0, SEARCH_RESULT_POOL_LIMIT);
}

function resultPath(item) {
  const parts = [
    formatSection(item.meta?.section),
    item.pageTitle && item.pageTitle !== item.title ? item.pageTitle : '',
  ].filter(Boolean);
  return parts.join(' / ');
}

function renderResultItem(item, index) {
  const title = escapeHTML(item.title || item.url);
  const url = escapeHTML(cleanResultURL(item.url));
  const section = escapeHTML(formatSection(item.meta?.section));
  const kind = escapeHTML(formatKind(item.meta?.kind || item.meta?.section));
  const showKind = item.meta?.kind && item.meta.kind !== item.meta?.section;
  const source = escapeHTML(compactSource(item.meta?.source));
  const path = escapeHTML(resultPath(item));
  const excerpt = item.excerpt
    ? sanitizePagefindHTML(item.excerpt)
    : escapeHTML(item.plainExcerpt || item.meta?.description || '');

  return `
    <a id="site-search-result-${index}" class="search-result" role="option" aria-selected="false" href="${url}" data-search-result-index="${index}">
      <span class="search-result-main">
        <span class="search-result-title">${title}</span>
        <span class="search-result-badges">
          <span class="search-result-badge">${section}</span>
          ${showKind ? `<span class="search-result-badge search-result-kind">${kind}</span>` : ''}
          ${source ? `<span class="search-result-source" title="${escapeHTML(item.meta?.source)}">${source}</span>` : ''}
        </span>
      </span>
      ${path ? `<span class="search-result-path">${path}</span>` : ''}
      ${excerpt ? `<span class="search-result-excerpt">${excerpt}</span>` : ''}
    </a>
  `;
}

function updateActiveSearchItem(nextIndex) {
  if (!searchInput || !searchResults || activeSearchItems.length === 0) return;
  activeSearchIndex = Math.max(
    0,
    Math.min(nextIndex, activeSearchItems.length - 1)
  );
  for (const result of searchResults.querySelectorAll(
    '[data-search-result-index]'
  )) {
    const active =
      Number(result.dataset.searchResultIndex) === activeSearchIndex;
    result.classList.toggle('active', active);
    result.setAttribute('aria-selected', String(active));
  }
  const activeID = `site-search-result-${activeSearchIndex}`;
  searchInput.setAttribute('aria-activedescendant', activeID);
  document.getElementById(activeID)?.scrollIntoView({ block: 'nearest' });
}

function renderAnchorItem(anchor, index) {
  return `
    <a id="site-search-result-${index}" class="search-result-anchor" role="option" aria-selected="false" href="${escapeHTML(cleanResultURL(anchor.url))}" data-search-result-index="${index}">
      <span class="search-result-anchor-hash" aria-hidden="true">#</span>
      <span class="search-result-anchor-title">${escapeHTML(anchor.title)}</span>
    </a>
  `;
}

function renderSearchResults(view, { restoreIndex = 0 } = {}) {
  if (!searchResults) return;
  const { groups, query, context, expanded } = view;
  const visible = expanded
    ? groups
    : groups.slice(0, SEARCH_RESULT_DISPLAY_LIMIT);
  const hiddenCount = groups.length - visible.length;

  const entries = [];
  const chunks = [];
  for (const group of visible) {
    const pageIndex = entries.length;
    entries.push({ type: 'page', url: cleanResultURL(group.page.url) });
    const anchorHTML = group.anchors
      .map((anchor) => {
        const index = entries.length;
        entries.push({ type: 'anchor', url: cleanResultURL(anchor.url) });
        return renderAnchorItem(anchor, index);
      })
      .join('');
    chunks.push(`
      <div class="search-result-group" role="group" aria-label="${escapeHTML(group.page.title)}">
        ${renderResultItem(
          {
            ...group.page,
            excerpt: group.excerpt,
            plainExcerpt: group.plainExcerpt,
          },
          pageIndex
        )}
        ${anchorHTML}
      </div>
    `);
  }
  if (hiddenCount > 0) {
    const index = entries.length;
    entries.push({ type: 'more' });
    chunks.push(
      `<button type="button" id="site-search-result-${index}" class="search-more" role="option" aria-selected="false" data-search-more data-search-result-index="${index}">Show ${hiddenCount} more result${hiddenCount === 1 ? '' : 's'}</button>`
    );
  }

  activeSearchItems = entries;
  searchResults.hidden = false;
  searchResults.innerHTML = `
    <div class="search-results-summary" role="presentation">
      <span>${groups.length} page${groups.length === 1 ? '' : 's'} for <strong>${escapeHTML(query)}</strong></span>
      <span>${escapeHTML(context.scopeLabel)}</span>
    </div>
    ${chunks.join('')}
  `;
  setSearchExpanded(true);
  if (entries.length) {
    updateActiveSearchItem(Math.min(restoreIndex, entries.length - 1));
  } else {
    activeSearchIndex = -1;
  }

  for (const result of searchResults.querySelectorAll(
    '[data-search-result-index]'
  )) {
    result.addEventListener('mousemove', () => {
      updateActiveSearchItem(Number(result.dataset.searchResultIndex));
    });
  }
  searchResults
    .querySelector('[data-search-more]')
    ?.addEventListener('click', expandSearchResults);
}

function expandSearchResults() {
  if (!searchView || searchView.expanded) return;
  const restoreIndex = activeSearchIndex;
  searchView.expanded = true;
  renderSearchResults(searchView, { restoreIndex });
  searchInput?.focus();
}

async function renderSearch(query) {
  const trimmed = query.trim();
  if (!searchResults) return;
  if (!trimmed) {
    hideSearchResults();
    searchResults.innerHTML = '';
    return;
  }

  const requestID = ++searchRequestID;
  const context = searchContext();
  setSearchStatus(`Searching ${context.scopeLabel}...`);
  try {
    const pagefind = await loadPagefind();
    const rawResults = await searchPagefind(
      pagefind,
      trimmed,
      requestID,
      context
    );
    if (requestID !== searchRequestID || rawResults === null) return;
    const results = await loadSearchItems(rawResults, trimmed, requestID);
    if (requestID !== searchRequestID) return;

    if (results.length === 0) {
      setSearchStatus('No results.');
      return;
    }

    searchView = {
      groups: results,
      query: trimmed,
      context,
      expanded: false,
    };
    renderSearchResults(searchView);
  } catch (error) {
    console.error('website search failed', error);
    setSearchStatus('Search index is generated by website:build.');
  }
}

function queueSearch(query, { immediate = false } = {}) {
  window.clearTimeout(searchTimer);
  const trimmed = query.trim();
  searchRequestID += 1;
  if (!trimmed) {
    hideSearchResults();
    if (searchResults) searchResults.innerHTML = '';
    return;
  }
  const context = searchContext();
  setSearchStatus(`Searching ${context.scopeLabel}...`);
  searchTimer = window.setTimeout(
    () => {
      renderSearch(query);
    },
    immediate ? 0 : 140
  );
}

searchInput?.addEventListener('input', () => {
  queueSearch(searchInput.value);
});

searchInput?.addEventListener('focus', () => {
  warmSearch();
  if (searchInput.value.trim()) {
    queueSearch(searchInput.value, { immediate: true });
  }
});

document.addEventListener('click', (event) => {
  if (!searchRoot || searchRoot.contains(event.target)) return;
  hideSearchResults();
});

searchInput?.addEventListener('keydown', (event) => {
  if (event.key === 'Escape' && searchResults) {
    hideSearchResults();
    searchInput.blur();
    return;
  }
  if (event.key === 'ArrowDown' && activeSearchItems.length) {
    event.preventDefault();
    updateActiveSearchItem(activeSearchIndex + 1);
    return;
  }
  if (event.key === 'ArrowUp' && activeSearchItems.length) {
    event.preventDefault();
    updateActiveSearchItem(activeSearchIndex - 1);
    return;
  }
  if (event.key === 'Enter' && activeSearchItems[activeSearchIndex]) {
    event.preventDefault();
    const item = activeSearchItems[activeSearchIndex];
    if (item.type === 'more') {
      expandSearchResults();
      return;
    }
    window.location.href = item.url;
  }
});

document.addEventListener('keydown', (event) => {
  if (event.key !== '/' || event.metaKey || event.ctrlKey || event.altKey) {
    return;
  }

  const target = event.target;
  const isTyping =
    target instanceof HTMLInputElement ||
    target instanceof HTMLTextAreaElement ||
    target instanceof HTMLSelectElement ||
    target?.isContentEditable;

  if (isTyping || !searchInput) return;
  event.preventDefault();
  searchInput.focus();
});

const searchShortcutHint = searchRoot?.querySelector('.search-shortcut');
const isApplePlatform = /Mac|iPhone|iPad|iPod/i.test(
  navigator.userAgentData?.platform || navigator.platform || ''
);
if (searchShortcutHint) {
  searchShortcutHint.textContent = isApplePlatform ? '⌘K' : 'Ctrl K';
}

document.addEventListener('keydown', (event) => {
  if (event.key.toLowerCase() !== 'k' || event.altKey || event.shiftKey) {
    return;
  }
  if (!(event.metaKey || event.ctrlKey) || !searchInput) return;
  event.preventDefault();
  warmSearch();
  searchInput.focus();
  searchInput.select();
});

// Docs sidebar: persist its scroll position so navigation keeps your place.
// The restore half is an inline script in baseof.html (right after the aside)
// so the position is set during parse, before first paint.
const sideNavPane = document.querySelector('aside.side-nav');
if (sideNavPane) {
  let sideNavRaf = 0;
  sideNavPane.addEventListener(
    'scroll',
    () => {
      if (sideNavRaf) return;
      sideNavRaf = requestAnimationFrame(() => {
        sideNavRaf = 0;
        try {
          sessionStorage.setItem('gj-sidenav-scroll', String(sideNavPane.scrollTop));
        } catch {}
      });
    },
    { passive: true }
  );
}

// Docs infinite scroll: each doc page renders a static .doc-next link (the next
// page in sidebar order). Nearing it fetches that page and swaps the link for a
// "Continued" divider plus the fetched article — which carries its own
// .doc-next, so reading continues page after page up to MAX_AUTO_APPEND; after
// that, or on any fetch error, the plain link stays and navigates normally.
// Accepted trade-offs: heading ids may repeat across appended articles
// (in-page anchors resolve to the first occurrence) and the TOC column keeps
// the first page's outline.
const infiniteMain = document.getElementById('main-content');
const firstDocArticle = infiniteMain?.querySelector('article.doc-article');
if (infiniteMain && firstDocArticle && infiniteMain.dataset.nextUrl) {
  const MAX_AUTO_APPEND = 8;
  let appendedCount = 0;
  let loadingNext = false;

  firstDocArticle.dataset.docUrl = window.location.pathname;
  firstDocArticle.dataset.docTitle = document.title;

  const sentinelObserver = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        sentinelObserver.unobserve(entry.target);
        appendNextPage(entry.target);
      }
    },
    { rootMargin: '0px 0px 900px 0px' }
  );

  const positionObserver = new IntersectionObserver(syncCurrentArticle, {
    rootMargin: '-45% 0px -55% 0px',
  });
  positionObserver.observe(firstDocArticle);

  function armNextSentinel() {
    if (appendedCount >= MAX_AUTO_APPEND) return;
    const navs = infiniteMain.querySelectorAll('.doc-next[data-next-link]');
    const last = navs[navs.length - 1];
    if (last) sentinelObserver.observe(last);
  }

  async function appendNextPage(nav) {
    if (loadingNext) return;
    const link = nav.querySelector('a');
    if (!link) return;
    loadingNext = true;
    nav.classList.add('is-loading');
    try {
      const response = await fetch(link.href);
      if (!response.ok) throw new Error(String(response.status));
      const doc = new DOMParser().parseFromString(await response.text(), 'text/html');
      const article = doc.querySelector('#main-content article.doc-article');
      if (!article) throw new Error('no article');
      const adopted = document.adoptNode(article);
      adopted.dataset.docUrl = new URL(link.href).pathname;
      adopted.dataset.docTitle = doc.title || document.title;
      const divider = document.createElement('div');
      divider.className = 'doc-continued';
      const kicker = document.createElement('span');
      kicker.textContent = 'Continued';
      const dividerLink = document.createElement('a');
      dividerLink.href = link.href;
      dividerLink.textContent =
        (link.textContent || '').replace(/^Next/, '').trim() || adopted.dataset.docTitle;
      divider.append(kicker, dividerLink);
      nav.replaceWith(divider, adopted);
      positionObserver.observe(adopted);
      appendedCount += 1;
      armNextSentinel();
    } catch {
      nav.classList.remove('is-loading');
      nav.classList.add('doc-next-fallback');
    } finally {
      loadingNext = false;
    }
  }

  function syncCurrentArticle() {
    const articles = infiniteMain.querySelectorAll('article.doc-article[data-doc-url]');
    let current = articles[0];
    for (const article of articles) {
      if (article.getBoundingClientRect().top <= window.innerHeight * 0.45) current = article;
    }
    if (current) setCurrentPage(current.dataset.docUrl, current.dataset.docTitle);
  }

  function setCurrentPage(pathname, title) {
    if (!pathname || window.location.pathname === pathname) return;
    history.replaceState(null, '', pathname);
    if (title) document.title = title;
    for (const navRoot of document.querySelectorAll('aside.side-nav, .mobile-docs-panel')) {
      for (const link of navRoot.querySelectorAll('a')) {
        const isCurrent = new URL(link.href, window.location.origin).pathname === pathname;
        link.classList.toggle('active', isCurrent);
        if (isCurrent) link.setAttribute('aria-current', 'page');
        else link.removeAttribute('aria-current');
      }
    }
    document.querySelector('aside.side-nav a.active')?.scrollIntoView({ block: 'nearest' });
  }

  armNextSentinel();
}

// Homepage watch story: reveal the static, fully readable stages as they enter view.
const watchStory = document.querySelector('[data-watch-story]');
if (
  watchStory &&
  'IntersectionObserver' in window &&
  window.matchMedia('(prefers-reduced-motion: no-preference)').matches
) {
  watchStory.dataset.watchMotion = 'ready';
  const watchStoryObserver = new IntersectionObserver(
    (entries) => {
      if (!entries.some((entry) => entry.isIntersecting)) return;
      watchStory.classList.add('is-visible');
      watchStoryObserver.disconnect();
    },
    { threshold: 0.18 }
  );
  watchStoryObserver.observe(watchStory);
} else {
  watchStory?.classList.add('is-visible');
}

// Homepage hero: rotate the demo Q/A pairs (static markup is pair one).
const heroRotateWindow = document.querySelector('.ai-window-hero');
const heroRotateData = document.getElementById('hero-rotate-data');
if (
  heroRotateWindow &&
  heroRotateData &&
  window.matchMedia('(prefers-reduced-motion: no-preference)').matches
) {
  let heroPairs = [];
  try {
    heroPairs = JSON.parse(heroRotateData.textContent || '[]');
  } catch {
    heroPairs = [];
  }
  const heroUser = heroRotateWindow.querySelector('.chat-user');
  const heroTools = [...heroRotateWindow.querySelectorAll('.tool-call')].map((tool) => ({
    name: tool.querySelector('.tool-call-header span:last-child'),
    line: tool.querySelector('code'),
    element: tool,
  }));
  const heroSourceList = heroRotateWindow.querySelector('.hero-source-list');
  const heroDone = heroRotateWindow.querySelector('.done-line .done-text');
  const heroAnswer = heroRotateWindow.querySelector('.assistant-copy');
  if (
    Array.isArray(heroPairs) &&
    heroPairs.length > 1 &&
    heroPairs.every(
      (pair) =>
        Array.isArray(pair.tools) &&
        pair.tools.length === heroTools.length &&
        Array.isArray(pair.sources)
    ) &&
    heroUser &&
    heroSourceList &&
    heroDone &&
    heroAnswer &&
    heroTools.length > 0 &&
    heroTools.every((tool) => tool.name && tool.line)
  ) {
    const heroAnimated = [
      heroUser,
      ...heroTools.map((tool) => tool.element),
      heroDone.parentElement,
      heroAnswer,
    ];
    const renderHeroSources = (sources) => {
      const fragment = document.createDocumentFragment();
      for (const source of sources) {
        const badge = document.createElement('span');
        const name = document.createElement('strong');
        name.textContent = source.name;
        badge.append(name, document.createTextNode(' ' + source.detail));
        fragment.append(badge);
      }
      heroSourceList.replaceChildren(fragment);
    };
    let heroPointerPaused = false;
    let heroFocusPaused = false;
    let heroIndex = 0;
    heroRotateWindow.addEventListener('pointerenter', () => {
      heroPointerPaused = true;
    });
    heroRotateWindow.addEventListener('pointerleave', () => {
      heroPointerPaused = false;
    });
    heroRotateWindow.addEventListener('focusin', () => {
      heroFocusPaused = true;
    });
    heroRotateWindow.addEventListener('focusout', (event) => {
      if (!heroRotateWindow.contains(event.relatedTarget)) heroFocusPaused = false;
    });
    window.setInterval(() => {
      if (document.hidden || heroPointerPaused || heroFocusPaused) return;
      heroIndex = (heroIndex + 1) % heroPairs.length;
      const pair = heroPairs[heroIndex];
      heroUser.textContent = pair.user;
      pair.tools.forEach((tool, i) => {
        heroTools[i].name.textContent = tool.name;
        heroTools[i].line.textContent = tool.line;
      });
      renderHeroSources(pair.sources);
      heroDone.textContent = ' ' + pair.done;
      heroAnswer.textContent = pair.answer;
      for (const el of heroAnimated) {
        el.style.animation = 'none';
        void el.offsetWidth; // commit the reset so the stagger replays
        el.style.animation = '';
      }
    }, 9000);
  }
}

// Benchmark landing: count the hero score up to the server-rendered value.
const benchCountUp = document.querySelector('[data-benchmark-count-up]');
if (
  benchCountUp &&
  window.matchMedia('(prefers-reduced-motion: no-preference)').matches
) {
  const benchCountText = benchCountUp.textContent;
  const benchCountTarget = Number.parseInt(benchCountText, 10);
  if (Number.isFinite(benchCountTarget) && benchCountTarget > 0) {
    const benchCountStart = performance.now();
    const benchCountTick = (now) => {
      const progress = Math.min((now - benchCountStart) / 1100, 1);
      const eased = 1 - Math.pow(1 - progress, 3);
      if (progress < 1) {
        benchCountUp.textContent = String(Math.round(benchCountTarget * eased));
        requestAnimationFrame(benchCountTick);
      } else {
        benchCountUp.textContent = benchCountText; // restore the exact rendered value
      }
    };
    requestAnimationFrame(benchCountTick);
  }
}

// Benchmark landing: reveal story bands and the demo conversation as they enter view.
const benchRevealTargets = document.querySelectorAll('[data-bench-reveal], [data-bench-demo]');
if (benchRevealTargets.length > 0) {
  if (
    'IntersectionObserver' in window &&
    window.matchMedia('(prefers-reduced-motion: no-preference)').matches
  ) {
    const benchRevealObserver = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (!entry.isIntersecting) continue;
          entry.target.classList.add('is-visible');
          benchRevealObserver.unobserve(entry.target);
        }
      },
      { threshold: 0.18 }
    );
    for (const el of benchRevealTargets) {
      el.dataset.benchMotion = 'ready';
      benchRevealObserver.observe(el);
    }
  } else {
    for (const el of benchRevealTargets) el.classList.add('is-visible');
  }
}

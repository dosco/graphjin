---
title: "Discovery Cache And Semantic Search"
description: "Start from durable schema generations, coordinate refreshes across replicas, and add Ax-backed semantic recall to catalog search."
nav_group: "configure"
doc_kind: "reference"
weight: 45
---

GraphJin Service can persist database discovery as immutable filesystem
generations and use an optional Ax embedding index to improve catalog recall.
The two features are related but independent:

- The discovery cache is enabled by default and shortens warm startup.
- Semantic search is opt-in and augments the existing lexical catalog search.
- Embeddings suggest relevant concepts and table endpoints. GraphJin's real
  metadata graph remains the authority for relationships and join paths.

Direct users of the `core` Go package keep its existing live-first behavior.
These settings belong to GraphJin Service.

## Configuration

```yaml
discovery_cache:
  enabled: true
  path: .graphjin/discovery
  refresh_interval: 5m
  startup_wait: 2m
  retain_generations: 2

catalog_search:
  semantic:
    enabled: true
    provider: openai
    embedding_model: text-embedding-3-small
    api_key_env: OPENAI_API_KEY
    base_url: ""
    dimensions: tiny
```

Keep the API key in the named environment variable. It is not written to the
discovery cache, semantic index, manifest, or Redis.

### Discovery cache fields

| Field | Default | Meaning |
| --- | --- | --- |
| `enabled` | `true` | Use service-owned cache-first discovery. Set `false` to retain the previous service live-first behavior. |
| `path` | `.graphjin/discovery` | Filesystem location for immutable discovery and semantic generations. |
| `refresh_interval` | `5m` | How often the service checks for a changed live schema in the background. |
| `startup_wait` | `2m` | Maximum cold-start or explicit-refresh wait for another replica's activation. |
| `retain_generations` | `2` | Number of valid activated generations to retain when the filesystem supports deletion. |

### Semantic fields

| Field | Default | Meaning |
| --- | --- | --- |
| `enabled` | `false` | Enable hybrid lexical and semantic catalog retrieval. |
| `provider` | `openai` | Ax embedding provider name. |
| `embedding_model` | none | Required when semantic search is enabled. |
| `api_key_env` | `OPENAI_API_KEY` | Environment variable containing the provider key. |
| `base_url` | empty | Optional provider-compatible endpoint override. |
| `dimensions` | `tiny` | `tiny` = 128, `small` = 256, `medium` = 512, or `default` = provider-native. |

Named dimensions are strict. If the provider returns a different vector size,
GraphJin rejects that response and continues with lexical search instead of
silently storing larger vectors.

## Startup and refresh behavior

| Situation | Behavior |
| --- | --- |
| Single process, no cache | The process discovers the live databases, validates the result, activates the first filesystem generation, and starts. Redis is not required. |
| Single process, warm cache | GraphJin loads the filesystem generation immediately and refreshes it in the background using in-process coordination. |
| Multiple replicas, no cache | Redis grants one fenced discovery lease. Followers subscribe and poll until that builder activates the shared-filesystem generation or `startup_wait` expires. |
| Multiple replicas, warm cache | Every replica starts from the shared filesystem immediately. One lease holder checks for changes in the background. |
| Explicit schema reload | The reload uses the same coordinated generation path and waits for activation. |
| Schema-affecting config change | Coordination moves to a secret-free config fingerprint and replicas converge on its validated generation. |

A generation contains a full-fidelity schema snapshot per database source,
including the discovered metadata needed to reconstruct the catalog. DDL is
also kept when available for diagnostics and legacy fallback. GraphJin writes
data first and the manifest last, validates file sizes and SHA-256 checksums,
then verifies that a cache-loaded temporary engine has the same catalog
revision as live discovery. Partial, corrupt, or incompatible generations are
not loadable.

The source/config fingerprint includes schema-affecting identity and settings
but redacts credentials and raw connection secrets.

## Single process versus horizontal deployment

Redis is optional for one GraphJin process. Discovery and semantic builders use
process-local locks, while all durable data still goes to
`discovery_cache.path`.

Horizontal deployments require both:

1. `redis.url`, for leases, fencing tokens, active IDs, progress, and PubSub.
2. A shared read-after-write filesystem mounted at the same
   `discovery_cache.path` on every replica.

Redis does not store schema metadata, document maps, or vectors. A replica that
receives an active generation ID it cannot read keeps its previous generation,
logs degraded status, and does not independently rebuild around the missing
shared file.

### Redis outage behavior

- With a valid filesystem generation, GraphJin starts from it in degraded mode
  and suspends coordinated rebuilding until Redis is available again.
- Without a valid generation, startup fails after `startup_wait`. This prevents
  every replica from independently discovering the same large database.
- If Redis is not configured at all, GraphJin treats the deployment as
  single-process and uses local coordination normally; that is not an outage.

## What the semantic index stores

GraphJin does not create one vector per column. It builds bounded, canonical
documents from the shared catalog:

- One identity document per table.
- Column facets for identifiers, measures, time, categorical/status,
  text/search, and other columns, chunked at 64 columns.
- One relationship-neighborhood document per table, containing real one-hop
  foreign keys.
- Safe shared concept documents for help, capabilities, recipes, workflows,
  fragments, saved queries, and similar catalog cards.

Caller-owned artifacts, query/workflow bodies, evidence payloads, examples
containing values, configuration values, and secrets remain lexical-only.

The persisted index contains normalized float32 vectors and a compact document
map with hashes, kinds, target card IDs, member columns, and vector offsets. It
does not need to retain the original document text.

For a catalog with 1,000 tables and 300 columns per table, the upper bound is
about twelve documents per table: roughly 12,000 vectors instead of 300,000.
At `tiny`, the raw vectors are approximately 6 MB. The initial build is about
188 batches of 64 documents, with at most two embedding requests in flight.
Warm startup makes no document-embedding calls.

## Incremental indexing and model changes

Each canonical document has a content hash. When the catalog changes under the
same embedding space, GraphJin reuses vectors for unchanged hashes, embeds only
added or changed documents, and removes stale documents from the next
generation.

The embedding-space fingerprint includes the provider, model, base URL,
dimension preset, and semantic document format. Changing any of those starts a
complete rebuild. Actual vector dimensions are recorded and validated; a
dimension mismatch invalidates the incompatible index and leaves catalog
search on the lexical fallback until a compatible index is ready.

A compatible older index can continue serving while a new catalog revision is
being indexed. An index from another embedding fingerprint is never used.

## How hybrid catalog search works

1. GraphJin generates lexical candidates from token and trigram postings.
2. An exact identifier result remains first and avoids a query embedding call.
3. Otherwise, GraphJin embeds the search text with a two-second deadline and
   exact-scans the compact semantic index using cosine similarity.
4. Weak hits are removed using the query's background score distribution. The
   top eight scores are excluded from background p99; a hit must score at least
   `max(0.25, p99 + 0.02)` and remain within `0.10` of the best score. At most
   64 semantic documents continue.
5. Table and facet hits map back to catalog cards. Explicit column searches can
   map a facet to its member columns.
6. GraphJin takes up to four table endpoints and runs deterministic breadth-
   first search over caller-visible foreign-key relationships. It returns at
   most two shortest paths of three edges or fewer.
7. Lexical, semantic, and real relationship-path ranks are fused with RRF
   (`k=60`) and weights `1.0`, `0.7`, and `0.5`. Exact identifiers remain
   pinned above the fused candidates.
8. The original `where`, shorthand filters, visibility, ordering, limit, and
   offset are applied to the combined candidate set.

`explain: true` identifies lexical matches, semantic table/facet recall, and
deterministic relationship paths. Similarity never creates a join. For a query
such as `customers and products they bought`, embeddings may recall the two
endpoint tables, but only actual catalog relationships can add a path such as
`customers -> orders -> order_items -> products`.

`search_rank` is the fused relevance rank. If the caller supplies an explicit
non-relevance `order_by`, GraphJin sorts the hybrid candidate union by those
requested fields instead.

Query embeddings are not persisted. Each replica keeps a 1,024-entry,
ten-minute in-memory cache keyed by query hash. Provider errors, timeouts, or
dimension mismatches return lexical results rather than failing
`query_catalog`.

## Built-in agent search language and adaptive coverage

Semantic search also changes how the service-owned GraphJin agent explores the
catalog, but only when the semantic subsystem was constructed successfully.
It does not change public MCP, service configuration, direct-core behavior, or
the mandatory catalog seed. Every run still begins with the full user
instruction as one explained `query_catalog` search.

After that seed, the agent is instructed to:

- Search with short noun-and-intent phrases in the user's business language,
  not guessed physical identifiers, SQL, GraphQL, embedding-provider terms, or
  sample values.
- Start a multi-entity question with its combined relationship intent.
- Treat semantic hits as recall candidates and inspect returned card IDs before
  querying or acting.
- Accept a join only when the catalog returns a real relationship path.
- Avoid expanding exact identifiers or seeds that already cover the request.

The agent has one private adaptive coverage call per run. It uses that call
only when a multi-concept request is missing an endpoint or verified path,
column evidence is missing, or the results are empty or materially ambiguous.
The call contains two or three unique phrases of at most 512 UTF-8 bytes each
and cannot be combined with `search`, `id`, `ids`, or `order_by`. Filters and
the result limit still apply, and explanations are automatic.

All uncached phrase vectors are produced by one Ax embedding request under the
same two-second deadline. Cache hits are omitted from that request. GraphJin
then scans and ranks each phrase sequentially, preserving per-phrase card IDs
and provenance rather than comparing cosine scores between phrases. Exact
identifiers are pinned, one distinct anchor is reserved per phrase, and the
remaining results use equal-weight RRF with `k=60`. Up to four visible table
endpoints feed the same deterministic relationship BFS used by normal hybrid
search.

The coverage response includes a deterministic, visibility-filtered
`next.args.ids` handoff containing up to four endpoint tables plus returned
relationship-path cards. The agent inspects those IDs in one detail call before
answering. A second `searches` call is rejected without another embedding
request, and an empty `ids` list is rejected instead of becoming an accidental
unfiltered catalog query.

The private response metadata includes retrieval mode, exact-match status,
semantic candidate count, visible table endpoints, relationship-path count,
and lexical-fallback state. If the index is warming or the batch embedding
fails, all phrases return lexical groups and the agent is told not to repeat
the one allowed expansion. This improves breadth; similarity is never accepted
as schema or join evidence.

## Validate discovery quality with the coffee demo

The repository includes an end-to-end comparison that starts the
coffee-roastery demo twice and sends the same MCP `query_catalog` searches to
the lexical-only and hybrid configurations:

```bash
examples/coffee-roastery/scripts/semantic-smoke.sh
```

To exercise the built-in REST agent through the real Ax/Goja runtime without a
provider key, add `--agent`:

```bash
examples/coffee-roastery/scripts/semantic-smoke.sh --agent
```

The default run uses a deterministic OpenAI-compatible fixture, so it needs no
provider key. It proves the Ax request path, strict `tiny` dimensions,
filesystem generation handoff, hybrid rank fusion, relationship BFS,
explanations, exact-identifier bypass, low-confidence fallback, bounded cold
build batches, zero embedding calls on warm startup, and the agent-only service
runtime coverage path (three phrases in one embedding request). With `--agent`,
it also proves the REST agent uses one adaptive coverage call, batches only
uncached query phrases, inspects returned card ids, and accepts only catalog
relationship paths. It tests business
phrases such as `clients`, `purchases`, and `raw coffee inventory` against the
demo's physical names such as `customers`, `production_orders`, and
`green_lots`.

The deterministic run validates GraphJin's behavior, not the semantic quality
of a production model. Repeat the same acceptance cases against a real model
before enabling it in production:

```bash
OPENAI_API_KEY=... \
  GRAPHJIN_SEMANTIC_PROVIDER=openai \
  GRAPHJIN_SEMANTIC_MODEL=text-embedding-3-small \
  examples/coffee-roastery/scripts/semantic-smoke.sh --live
```

The runner uses an isolated temporary cache and stops its demo containers. Use
`--report path/to/report.json` to retain the lexical and semantic ranks.
The deterministic agent response verifies orchestration and protocol behavior;
use the live model agent suite to evaluate a production model's judgment.

## Operational checklist

- Keep semantic search disabled until its provider key, model, latency, and
  cost are acceptable for the deployment.
- Use `tiny` first unless retrieval tests demonstrate that more dimensions are
  needed.
- Mount a shared read-after-write cache path before scaling beyond one replica.
- Configure Redis before scaling; it is collaboration state, not vector
  storage.
- Preserve at least two generations so a previously activated valid snapshot
  remains available during refreshes.
- Investigate `discovery cache degraded` and semantic fallback warnings in the
  service logs. Lexical catalog search remains available during semantic
  failures.

{{< verified by="TestSingleNodeDiscoveryAndSemanticWorkWithoutRedis" file="serv/discovery_coordination_test.go" line="223" >}}
{{< verified by="TestDiscoveryRedisCoordinatesColdBuilderWarmFollowersAndFencing" file="serv/discovery_coordination_test.go" line="95" >}}
{{< verified by="TestSemanticIndexIncrementalReuseDimensionMismatchAndWarmLoad" file="serv/semantic_index_test.go" line="236" >}}
{{< verified by="TestHybridCatalogRetrievalFixtureAndLexicalFallback" file="serv/semantic_query_test.go" line="51" >}}
{{< verified by="TestCoffeeRoasteryServiceRuntimeCoverageBatch" file="serv/semantic_coverage_test.go" line="169" >}}
{{< verified by="TestPublicMCPQueryCatalogDoesNotExposeAgentCoverageSearches" file="serv/semantic_agent_surface_test.go" line="11" >}}
{{< verified by="semantic-smoke.sh" file="examples/coffee-roastery/scripts/semantic-smoke.sh" line="1" >}}

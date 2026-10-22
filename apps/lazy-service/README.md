# Lazy embedding service

The embedding container keeps a small Go HTTP listener running. Its model
process starts on the first processing request and exits after five idle
minutes. The container stays up; the model process and its heap do not. This
uses no Docker socket, privileged API, or in-host inference fallback.

- `OPENNEKO_EMBEDDING_IDLE_TIMEOUT` overrides the Compose default (a Go
  duration such as `5m` or `30s`). The image also accepts
  `OPENNEKO_SERVICE_IDLE_TIMEOUT` directly.
- Concurrent callers share one startup. The first request waits for child
  readiness; failed startup returns 503 and a later request may retry.
- Health checks report listener availability plus `sleeping`, `starting`, or
  `running`. They never start a child or extend its idle timer.
- Shutdown requires no active HTTP requests and a successful backend idle
  check. An unavailable idle check prevents eviction.
- Child crashes fail affected requests. The next request starts a new child;
  requests are not silently replayed.

The embedding service owns the unchanged `Xenova/all-MiniLM-L6-v2` q8 model,
mean pooling, normalization, and 384-dimensional vectors. The image build
vendors and verifies the model; runtime downloads are disabled. One inference
runs at a time, with a bounded queue. Model calls never happen in web, worker,
or chat sandboxes. Text search and ordinary database access need no model.
The embedding service does not persist text, access tenant databases, or log
submitted text.

Compose exposes only internal service ports. Web and worker use
`NEKO_EMBEDDING_URL=http://embedding:5003`. For host development, configure
that URL to a reachable service address; `scripts/dev-web-stack.sh` supplies
the embedding address from the selected running stack. Missing embedding
service configuration fails explicitly, with keyword fallback retained for
interactive memory search. Background indexing retries durably.

Memory and skill writes clear their vector in the same database write. The
null vector is the durable backlog: a pg-boss sweep every minute dispatches up
to 32 jobs, with at most 128 outstanding jobs. Jobs contain row identifiers,
not text. Dispatch interleaves organizations, and a PostgreSQL advisory lock
allows only one background embedding request across worker replicas. Each job
reads current content and writes its vector only if that content is still
current and eligible. Deleted, archived, expired, and suppressed memories are
skipped. Missing vectors remain discoverable after crashes and exhausted
retries. Keyword search is immediate; semantic indexing is eventual.
Interactive query embeddings have a 15-second deadline; durable jobs allow 150
seconds for cold startup and inference.

Limits are ceilings, not reservations. Embeddings default to one CPU and 1 GiB.
Idle shutdown saves unused memory; it does not guarantee a host can handle all
configured ceilings simultaneously.

Library documents are not embedded: GraphJin indexes and reads them itself.

Verification:

```sh
(cd apps/lazy-service && go test -race ./...)
node --test apps/embedding/test.mjs
pnpm --filter @neko/llm exec vitest run test/embedding.test.ts
docker build --target embedding -t openneko-embedding:lazy .
docker run --rm --network none --entrypoint node openneko-embedding:lazy /app/smoke.mjs
```

The durable queue regression test uses a separate disposable PostgreSQL database
with pgvector (it creates minimal tables):

```sh
EMBEDDING_TEST_DATABASE_URL=postgres://USER@HOST/TEST_DATABASE pnpm --filter @neko/worker exec vitest run test/embedding-jobs.test.ts
```

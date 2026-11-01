# Lazy embedding service

The embedding container keeps a small Go HTTP listener running: the
`openneko` binary in its `lazy-service` role. Its model process starts on the
first processing request and exits after five idle minutes. The container stays up; the model process and its heap do not. This
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

The embedding child is a Python process (`apps/embedding/embedding_service.py`)
running Google's EmbeddingGemma 2 (`google/embeddinggemma-2`, Apache 2.0)
text-only on CPU in float32: 768-dimensional normalized vectors, with
Matryoshka truncation to 512, 256, or 128 dimensions (re-normalized) on the
OpenAI-compatible route. It listens on `127.0.0.1:5004`, binds only after the
model is loaded, and answers `/health/ready` and `/health/idle`. Routes:

- `POST /v1/embed` (OpenNeko): `{"role":"query"|"document","task":"search"|"none","inputs":[{"text","title"}]}`
  with 1-64 inputs of at most 32000 characters. Task `search` applies the
  model-card prompts (`task: search result | query: ...` and
  `title: ... | text: ...`); task `none` adds no prefix.
- `POST /v1/embeddings` (OpenAI-compatible, used by GraphJin's Ax client):
  no prefix, optional `dimensions` of 128/256/512/768, any API key accepted.

The image build downloads one pinned model revision, keeps only the text
weights, verifies one embedding, and sets `HF_HUB_OFFLINE=1`; runtime
downloads are disabled and caches live on the `/tmp` tmpfs. One inference
runs at a time (inputs of a request are batched), with at most 16 queued
requests (429 beyond that) and a 4 MiB body limit. Torch threads follow
`OPENNEKO_EMBEDDING_THREADS`, else the container's CPU allowance. Model calls
never happen in web, the worker, or `openneko serve`. Text search and ordinary
database access need no model. The embedding service does not persist text,
access tenant databases, or log submitted text.

Compose exposes only internal service ports. Web and worker use
`NEKO_EMBEDDING_URL=http://embedding:5003`. For host development, configure
that URL to a reachable service address; `scripts/dev-web-stack.sh` supplies
the embedding address from the selected running stack. Missing embedding
service configuration fails explicitly, with keyword fallback retained for
interactive memory search. Background indexing retries durably.

Semantic search is off by default. An admin turns it on at Admin → Settings →
Search. While it is off, OpenNeko searches by keyword, never calls the
embedding service, and the model never loads.

Memory and skill writes clear their vector in the same database write. The
null vector is the durable backlog. While semantic search is on, a pg-boss
sweep every minute queues one backlog job when none is queued or running. The
job carries no text. It embeds memories and skills in batches of up to 64 for
at most five minutes; the next sweep queues the rest. A PostgreSQL advisory
lock allows only one background embedding job across worker replicas. Each
write stores the vector only if the content is still current and eligible.
Archived, expired, and suppressed memories are skipped. Missing vectors remain
discoverable after crashes and exhausted retries. Keyword search is immediate;
semantic indexing is eventual. Interactive query embeddings wait 4 seconds and
then fall back to keyword search; background jobs allow 150 seconds for cold
startup and inference.

Limits are ceilings, not reservations. Embeddings default to two CPUs and
2.5 GiB (the loaded model uses about 2 GB).
Idle shutdown saves unused memory; it does not guarantee a host can handle all
configured ceilings simultaneously.

OpenNeko does not embed Library documents. GraphJin indexes and reads them
itself, and uses the `/v1/embeddings` route only while semantic search is on.

Verification:

```sh
(cd apps/lazy-service && go test -race ./...)
(cd apps/embedding && python -m unittest -v test_embedding_service)  # needs requirements.txt
pnpm --filter @neko/llm exec vitest run test/embedding.test.ts
docker build --target embedding -t openneko-embedding:lazy .
docker run --rm --network none --memory 2560m --read-only --tmpfs /tmp:size=256m \
  -e OPENNEKO_EMBEDDING_TEST_MODEL=1 --entrypoint python3 openneko-embedding:lazy -m unittest test_embedding_service
docker run --rm --network none --memory 2560m --read-only --tmpfs /tmp:size=256m \
  --entrypoint python3 openneko-embedding:lazy /app/smoke.py
```

The durable queue regression test uses a separate disposable PostgreSQL database
with pgvector (it creates minimal tables):

```sh
EMBEDDING_TEST_DATABASE_URL=postgres://USER@HOST/TEST_DATABASE pnpm --filter @neko/worker exec vitest run test/embedding-jobs.test.ts
```

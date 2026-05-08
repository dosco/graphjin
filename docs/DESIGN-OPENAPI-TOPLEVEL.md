# Top-Level OpenAPI Virtual Tables — Design

Follow-up to the row-join PR (`feat/remote-joins` → `feat(openapi):` series). That PR shipped the foundation: spec loader, classifier, auth providers, generic resolver, and row-level joins. This PR adds the missing piece — **querying OpenAPI operations as top-level GraphQL fields, with no DB parent row required**.

## 1. Goal

Make every classifiable GET in an OpenAPI spec queryable from GraphQL, even when there's no parent table to join against:

```graphql
# Single-record top-level (path param becomes a required GraphQL arg)
query {
  interaction_studio_get_user_by_id(userId: "abc-123") {
    id
    email
    lastSeenAt
  }
}

# List/collection top-level (query params become GraphQL args)
query {
  interaction_studio_audit_logs(actorId: "u-7", limit: 50) {
    id
    action
    createdAt
  }
}

# Mixed: DB tables AND OpenAPI virtual tables in the same query
query {
  users(where: { id: { eq: 42 } }) { id email }
  interaction_studio_audit_logs(actorId: "u-7") { id action }
}
```

Today (post-row-join PR) only the row-join shape works:

```graphql
query {
  users { id email is_profile { ... } }   # ✅ works
}
```

The standalone forms above are detected at boot (`OpModeSingleByID`, `OpModeList`) and logged as available, but qcode rejects them at compile time because the table isn't visible to the schema lookup.

## 2. Current State (what's already in place)

After the row-join PR lands, the following are working and don't need to change:

- **`core/openapi/`** sub-package: `loader.go`, `classifier.go`, `auth.go`, `auth_token.go`, `caller.go`, `runtime.go`, `concurrency.go` — fully functional.
- **Operation classification**: every GET is sorted into `OpModeRowJoin`, `OpModeSingleByID`, `OpModeList`, or `OpModeSkipped`. The first three are all currently registered as `Caller`s in `Runtime`; only RowJoin is wired to a `core.Resolver`.
- **`core/openapi_bridge.go`**: implements `core.Resolver` for row joins. Synthesises `ResolverConfig` entries for `OpModeRowJoin` operations only.
- **Per-spec concurrency**: shared semaphore + token-bucket limiter applies to every `Caller`, including the not-yet-wired-up top-level ones.

**Not done:**

- Top-level OpenAPI operations are not registered in `sdata.DBSchema`, so qcode can't find them.
- qcode doesn't know to mark a top-level select as `SkipTypeRemote` when no parent context exists.
- `gstate.compileAndExecute` assumes every query has at least one SQL root; doesn't handle "all roots are remote".
- `remote_join.parentFieldIds` / `resolveRemotes` assume every remote select has a parent row to extract IDs from.
- The bridge layer doesn't know how to map GraphQL field arguments to `CallParams.PathValues` / `CallParams.QueryValues`.

## 3. Pipeline Gaps

A trace of `interaction_studio_audit_logs(actorId: "u-7") { id }` against the post-row-join codebase, identifying each break point:

| Step | What happens | Break |
|---|---|---|
| 1. GraphQL parse | `graph` package builds AST | ✅ works (no change needed) |
| 2. qcode resolves table | `co.Find(schema, "interaction_studio_audit_logs")` | ❌ **table not registered in sdata** |
| 3. qcode determines Rel | For top-level, doesn't set sel.Rel; defaults to RelNone | ❌ **never marked as remote** |
| 4. qcode skip decision | `sel.Rel.Type == RelRemote` check at qcode.go:885 | ❌ **never fires for top-level virtuals** |
| 5. SQL emission | psql package walks selects, emits SQL | ❌ **emits SQL for unknown table → error** |
| 6. gstate execute | Runs SQL, populates s.data | ❌ **no SQL to run if all roots are remote** |
| 7. execRemoteJoin | `selects[sel.ParentID]` for parent table lookup | ❌ **panics on ParentID == -1** |
| 8. Bridge resolves | openapiBridge.Resolve maps req.ID to path param | ❌ **only handles single string ID, not multiple GraphQL args** |

Each gap is small in isolation. The challenge is that they all have to land together — partial fixes leave the engine in an inconsistent state.

## 4. Recommended Architecture

**Treat top-level OpenAPI operations as parent-less remote tables and route them through a slightly extended version of the existing remote-join machinery, rather than building a parallel exec path.**

The key insight: the row-join code already handles HTTP fan-out, parallel execution, JSON merging, error propagation, and tracing. Reuse it. The changes are about teaching it that "no parent" is a valid case.

### 4.1 Registration model

Each `OpModeSingleByID` and `OpModeList` operation becomes a synthetic remote table in the primary DB's `sdata.DBInfo`:

```go
nt := sdata.NewDBTable(schema, op.ExposeAs, "remote", nil)
// PrimaryCol intentionally left zero — this signals "parent-less"
dbinfo.AddTable(nt)
```

When the schema graph builds, `addRemoteRel` sees `Type == "remote"` but `PrimaryCol.FKeyTable == ""` and treats the table as an orphan node — added to `tindex` so qcode can find it, but with no graph edges.

### 4.2 qcode marking

In `qcode.go:864-889` (the top-level table resolution path), after `co.Find()` succeeds:

```go
if sel.Ti.Type == "remote" && sel.Ti.PrimaryCol.FKeyTable == "" {
    // Top-level virtual: synthesise a Rel so the existing
    // RelRemote branch fires and qc.Remotes++ runs.
    sel.Rel = sdata.DBRel{Type: sdata.RelRemote}
}
```

This sets `sel.SkipRender = SkipTypeRemote` (existing code path on line 885) and increments `qc.Remotes`, so the rest of the pipeline already knows to skip SQL emission for this select.

### 4.3 SQL emission for "all roots remote"

When every root in `qc.Roots` is `SkipTypeRemote`, psql currently has nothing to emit and the query errors out. Two clean ways to handle this:

**Option A** — psql emits a no-op SQL statement (`SELECT 1` or equivalent) when all roots are remote. gstate runs it, gets an empty result, then execRemoteJoin populates s.data from scratch.

**Option B** — gstate detects the all-remote case and skips SQL execution entirely. s.data is seeded directly with placeholder JSON.

**Recommend B**. Avoids a wasted DB roundtrip and the dialect-specific weirdness of "no-op SQL". The detection is one line in gstate.compileAndExecute:

```go
if cs.st.qc.Remotes == int32(len(cs.st.qc.Roots)) {
    // All roots are remote — skip SQL, seed placeholder JSON.
    s.data = seedRemotePlaceholders(cs.st.qc)
} else {
    // Existing path: run SQL, populate s.data.
    if err = s.executeQuery(c); err != nil { return }
}
```

Where `seedRemotePlaceholders` builds JSON with one marker per top-level remote root:

```json
{"interaction_studio_audit_logs": "__remote__:interaction_studio_audit_logs"}
```

Markers are distinguishable from real data so execRemoteJoin can find them deterministically.

### 4.4 Marker scheme refactor

Today's `parentFieldIds` builds markers from `r.IDField` (the synthesised parent column name like `__resolver_email`). For parent-less remotes this doesn't apply.

New marker scheme:
- **Row-join (parent exists)**: marker is the parent column value, embedded in the parent row JSON. Existing behavior.
- **Top-level (no parent)**: marker is a synthetic placeholder keyed by the field name, written into `s.data` by `seedRemotePlaceholders`.

`parentFieldIds` becomes:

```go
for i, sel := range selects {
    if sel.SkipRender != qcode.SkipTypeRemote { continue }

    if sel.ParentID == -1 {
        // Top-level virtual table
        marker := []byte(sel.FieldName)
        fm = append(fm, marker)
        sm[string(marker)] = &selects[i]
        continue
    }

    // Existing row-join path
    p := selects[sel.ParentID]
    if r, ok := s.gj.rmap[(sel.Table + p.Table)]; ok {
        fm = append(fm, r.IDField)
        sm[string(r.IDField)] = &selects[i]
    }
}
```

`resolveRemotes` likewise branches:

```go
if sel.ParentID == -1 {
    // Top-level: read args from sel, build CallParams, no parent ID
    callParams := buildCallParamsFromArgs(sel, opDescriptor)
    body, err := caller.Call(ctx, callParams)
    // ...
} else {
    // Existing row-join path: id from parent column
}
```

### 4.5 Resolver lookup for top-level

Today `gj.rmap` is keyed by `sel.Table + parent.Table`. For top-level there's no parent table. Use a separate key shape:

```go
gj.rmap[sel.Table] = resItem{Fn: openapiBridge}  // top-level: key = field name
gj.rmap[sel.Table + parentTable] = resItem{...}  // row-join: key = field+parent
```

Both can coexist in the same map because the row-join keys always have a non-empty suffix.

### 4.6 GraphQL args → CallParams

Top-level operations carry their inputs as GraphQL field arguments. `interaction_studio_audit_logs(actorId: "u-7", limit: 50)` produces `sel.Args` (or equivalent — needs verification, see Open Questions).

The bridge's resolve method extends:

```go
func (b *openapiBridge) Resolve(ctx context.Context, req ResolverReq) ([]byte, error) {
    if b.topLevel {
        return b.caller.Call(ctx, b.buildTopLevelParams(req.Sel))
    }
    // Existing row-join path
    return b.caller.Call(ctx, openapi.CallParams{
        PathValues: map[string]string{b.pathName: req.ID},
    })
}

func (b *openapiBridge) buildTopLevelParams(sel *qcode.Select) openapi.CallParams {
    args := extractFieldArgs(sel)
    p := openapi.CallParams{
        PathValues:  map[string]string{},
        QueryValues: map[string]string{},
    }
    for _, ps := range b.op.PathParams {
        if v, ok := args[ps.Name]; ok {
            p.PathValues[ps.Name] = v
        }
    }
    for _, qs := range b.op.QueryParams {
        if v, ok := args[qs.Name]; ok {
            p.QueryValues[qs.Name] = v
        }
    }
    return p
}
```

Argument extraction is the one place where qcode's representation matters and needs verification — `qcode.Select.Args` is currently typed for Postgres function arguments, not generic GraphQL field args.

### 4.7 Schema visibility for arg validation

For users to write `interaction_studio_audit_logs(actorId: "u-7")`, the GraphQL introspection layer (intro.go) needs to know:
- That `interaction_studio_audit_logs` is a queryable field
- That it accepts `actorId` (and other query params) as arguments
- The argument types

This means the synthetic remote table needs not just an entry in `sdata.DBInfo` but also column-like entries describing the arguments. Probably easiest: register `OpDescriptor.QueryParams` and `OpDescriptor.PathParams` as virtual columns on the synthetic table. intro.go already walks columns to build the GraphQL schema.

## 5. File-by-File Change List

Estimated diff size: ~600–900 LOC across these files.

### Modified files

| File | Change | Est. LOC |
|---|---|---|
| `core/internal/sdata/schema.go` | `addRemoteRel` early-returns for parent-less remotes (no FK lookup, no graph edge) | ~10 |
| `core/internal/qcode/qcode.go` | After `co.Find()` for top-level selects, set `sel.Rel.Type = RelRemote` when `Ti.Type == "remote"` and `Ti.PrimaryCol.FKeyTable == ""` | ~10 |
| `core/internal/qcode/fields.go` | Ensure `SkipTypeRemote` is set on top-level remote selects (currently happens via parent-add path which doesn't run for top-level) | ~10 |
| `core/internal/psql/*.go` | Verify SQL emitter handles "all roots are SkipTypeRemote" — likely already does (since it skips remote selects) but needs a test | ~0–20 |
| `core/gstate.go` | Skip SQL execution when `qc.Remotes == len(qc.Roots)`; seed `s.data` with placeholder JSON via `seedRemotePlaceholders` | ~30 |
| `core/remote_join.go` | Branch `parentFieldIds` and `resolveRemotes` on `sel.ParentID == -1`; use field-name-based marker for top-level | ~50 |
| `core/openapi_bridge.go` | Extend `openapiBridge` to handle top-level (no parent ID, build CallParams from sel args); register top-level synthetic tables and resolvers in addition to row-join ResolverConfigs | ~80 |
| `core/internal/intro.go` (or equivalent) | Add the synthetic remote tables (with their args as columns) to GraphQL introspection output | ~40 |
| `CONFIG.md` | Update the "Limitations" section to remove "row-joins only" and document top-level usage | ~30 |

### New files

| File | Purpose | Est. LOC |
|---|---|---|
| `core/openapi/args.go` | Helpers for argument-name normalisation (camelCase ↔ snake_case for GraphQL conventions) | ~50 |
| `core/openapi_toplevel_test.go` | End-to-end test: spec → top-level query → mocked upstream → response | ~200 |
| `core/openapi/integration_toplevel_test.go` | Sub-package level test of CallParams construction from various arg shapes | ~150 |

## 6. Test Strategy

The risk surface here is the engine pipeline (`gstate`, `qcode`, `remote_join`). Three test layers:

### 6.1 Unit tests in the openapi sub-package

- CallParams construction from a fake `qcode.Select.Args` shape (table-driven)
- Argument-name normalisation edge cases
- Schema-introspection synthesis (synthetic columns from path/query params)

### 6.2 Bridge layer tests

- Top-level synthetic ResolverConfig generation includes the right metadata
- `openapiBridge.Resolve` correctly maps top-level args to CallParams.PathValues + QueryValues
- Both row-join and top-level bridges coexist in the same Runtime

### 6.3 End-to-end engine tests

The existing `core/multidb_test.go` is a precedent — it spins up a real GraphJin engine with a SQLite DB and exercises full GraphQL queries. The new tests should:

- **Scenario 1 — Pure top-level list**: `{ interaction_studio_audit_logs(actorId: "u-7") { id } }` against an httptest upstream. Verify response shape, query params forwarded, auth applied.
- **Scenario 2 — Pure top-level single-by-id**: same but for `getUserById(userId: "abc")`.
- **Scenario 3 — Mixed query**: `{ users { id } is_audit_logs { id } }` — DB + API in one query. Verify both halves run, response merges correctly.
- **Scenario 4 — Mixed with row-join**: `{ users { id is_profile { ... } } is_audit_logs { ... } }` — DB + row-join + top-level all together.
- **Scenario 5 — Error propagation**: upstream 500 / 401-after-retry / network timeout, verify the error surfaces in the GraphQL response without corrupting the rest.
- **Scenario 6 — Concurrency cap respected**: 100-row parent select with row-join, max_concurrent: 4 in config, verify the upstream sees no more than 4 concurrent in-flight requests.

### 6.4 Regression coverage

Run the full existing core test suite. The pipeline changes touch `gstate`, `qcode`, and `remote_join`, all of which have substantial existing coverage. Goal: zero existing test breakage.

## 7. Risks and Rollback

### Risks

1. **`qc.Remotes == len(qc.Roots)` is a narrower condition than expected.** A query with multiple roots where some are SQL and some are remote needs both code paths to coexist. Mitigation: scenario 3 + 4 in the test plan exercise this.

2. **GraphQL argument representation.** `qcode.Select.Args` is currently typed for Postgres functions. May need a parallel field for OpenAPI args, or refactor Args to be generic. This is the single biggest unknown — needs verification before serious coding.

3. **psql edge cases.** Some dialects may emit weird things when passed a Select that has only `SkipRender == SkipTypeRemote` selects at the root. Requires testing against every supported DB type, or scoped feature-flag (Postgres + SQLite first).

4. **Schema introspection visibility.** The `intro.go` layer might reject "tables with no columns" or "remote tables" for introspection — need to check. If users can't see top-level OpenAPI fields in the schema, IDEs/clients won't auto-complete them.

### Rollback

The feature can be disabled via a single config flag: `openapi.enable_top_level_virtual_tables: false` (default false during initial rollout, flip to true after burn-in). All the registration code becomes conditional on this flag. Rolling back is `git revert` of one PR with no data-layer impact (no DB migrations, no on-disk format changes).

Recommended rollout: ship with the flag default-false, run in a non-production environment for a sprint, then flip default-true in a follow-up version bump.

## 8. Open Questions (need answers before coding)

1. **What is the actual representation of GraphQL field arguments on a top-level `qcode.Select`?** `sel.Args` is typed for function args; need to trace what happens when a user writes `field(arg: value)` against a non-function table. May exist as `sel.Where` filter expressions — those would need translation into upstream query params.

2. **Does `psql` actually skip emission for `SkipTypeRemote` selects at the root, or only when they're children of a real table?** Verify with a small instrumented test before committing to "Option B" in §4.3.

3. **Does `intro.go` automatically include synthetic remote tables in GraphQL introspection output?** Or does it filter by `Type == "table"`? If the latter, additional plumbing is needed to surface top-level OpenAPI fields to clients.

4. **Should top-level operations register on the primary DB's schema, or on a synthetic "openapi" schema?** Primary DB is simpler (everything in one place). Separate schema is cleaner architecturally (clear separation of "data we own" vs "data we proxy"). Initial recommendation: primary DB; revisit if it causes name collisions in practice.

5. **How are GraphQL arg names normalised against OpenAPI param names?** Specs use camelCase (`actorId`); GraphJin's existing conventions favour snake_case. Probably accept both at the GraphQL surface and normalise to spec convention when calling. Decision: accept both, prefer the spec's exact name in introspection output, alias snake_case form for users.

6. **Single-row vs list response shapes.** OpenAPI single-by-id returns `{user_object}`, list returns `[user_object, ...]`. GraphQL single-by-id should resolve to a `Type`, list to `[Type]`. The `IsArrayResponse` field on `OpDescriptor` already tracks this; introspection just needs to honour it.

## 9. Sequencing

Suggested commit/review boundaries within the PR:

1. **Schema registration** — synthetic tables in sdata, addRemoteRel handles parent-less. Tests at sdata level only.
2. **qcode** — set RelRemote for top-level virtual tables, ensure SkipTypeRemote propagates. Tests at qcode level.
3. **gstate** — skip SQL when all roots are remote, seed placeholders. Tests with a small mock SQL adapter.
4. **remote_join** — branch on ParentID == -1, field-name marker scheme. Tests against existing remote-join path to verify no regression.
5. **bridge** — top-level resolver, args → CallParams. Tests against httptest upstream.
6. **intro** — schema introspection includes top-level operations. Test with a real GraphQL introspection query.
7. **End-to-end** — all the scenario tests in §6.3.
8. **Docs** — update CONFIG.md, remove the "Limitations: row-joins only" caveat.

Each step is independently testable; if a step breaks more existing tests than expected, roll back to the previous step and reconsider.

## 10. Estimated Effort

- Investigation of open questions: 0.5 day
- Implementation steps 1–4 (schema/qcode/gstate/remote_join): 2 days
- Implementation steps 5–6 (bridge/intro): 1 day
- Tests (steps 1–6 inline + scenario tests): 1.5 days
- Documentation + polish: 0.5 day

**Total: ~5.5 days of focused work.** Could be compressed to ~3 days if the open questions resolve quickly.

## 11. What This Doesn't Cover (out of scope, again)

Explicit non-goals so the PR stays focused:

- Write-side support (POST/PUT/PATCH/DELETE → GraphQL mutations). Separate PR.
- Async / export / file-download endpoints. Out of scope by design — GraphJin is a query engine.
- GraphQL SDL or gRPC spec formats. OpenAPI 3 only for the foreseeable future.
- Per-request auth pass-through via inbound HTTP headers. Already mentioned as deferred in row-join PR; remains deferred.
- OpenAPI-only mode (no DB at all). The primary-DB requirement (`api.go:335`) stays; lifting it is a separate architecture change.

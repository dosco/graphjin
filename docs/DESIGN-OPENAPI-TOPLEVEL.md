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

Per §8.1: today's qcode hard-rejects unknown arguments. Two changes are needed:

**Step A — qcode storage (one new field, one switch branch):**

```go
// core/internal/qcode/qcode.go, on Select:
ExtraArgs map[string]string

// core/internal/qcode/args.go, replace the default case:
default:
    if sel.Ti.Type == "remote" {
        v, ok := stringArgValue(a)
        if !ok {
            return fmt.Errorf("argument %q on remote table %q must be a string literal", a.Name, sel.Ti.Name)
        }
        if sel.ExtraArgs == nil {
            sel.ExtraArgs = map[string]string{}
        }
        sel.ExtraArgs[a.Name] = v
        continue
    }
    return unknownArg(a)
```

The `Type == "remote"` guard ensures real DB tables still hard-reject unknown args — unchanged behaviour for everything except synthetic remote tables.

**Step B — bridge consumption:**

```go
func (b *openapiBridge) Resolve(ctx context.Context, req ResolverReq) ([]byte, error) {
    if b.topLevel {
        return b.caller.Call(ctx, b.buildTopLevelParams(req.Sel))
    }
    return b.caller.Call(ctx, openapi.CallParams{
        PathValues: map[string]string{b.pathName: req.ID},
    })
}

func (b *openapiBridge) buildTopLevelParams(sel *qcode.Select) openapi.CallParams {
    p := openapi.CallParams{
        PathValues:  map[string]string{},
        QueryValues: map[string]string{},
    }
    for _, ps := range b.op.PathParams {
        if v, ok := sel.ExtraArgs[ps.Name]; ok {
            p.PathValues[ps.Name] = v
        }
    }
    for _, qs := range b.op.QueryParams {
        if v, ok := sel.ExtraArgs[qs.Name]; ok {
            p.QueryValues[qs.Name] = v
        }
    }
    return p
}
```

Validation (required path params present, type-correct values) lives in the bridge, not qcode — qcode shouldn't know about OpenAPI semantics. If a required path param is missing from `ExtraArgs`, the bridge errors before the HTTP call.

**Limitation:** This stores args as `map[string]string`. Non-string scalars (numbers, booleans) need conversion when serialised onto the URL — the OpenAPI caller already does this for path/query params, so it's a non-issue for the wire. But qcode's GraphQL parser will refuse a non-string-literal value at the args layer; users will need to quote `limit: "50"`. Acceptable for v1; we can widen the storage to `interface{}` in a follow-up if it bites.

### 4.7 Schema visibility for arg validation

Per §8.3: `intro.go:315` early-returns on `len(table.Columns) == 0`, so synthetic remote tables are invisible to introspection today. Two things need to land together:

**Synthetic columns from the response shape.** The OpenAPI response schema lists the JSON fields a successful response carries (`id`, `email`, `lastSeenAt` for a user object). Walk the response schema at registry-build time and synthesise `DBColumn` entries on the synthetic table. These become the field selection set in GraphQL.

**Synthetic columns or args from the request shape.** Path/query params don't fit cleanly as columns — they're not part of the response. Two options:

- **(a)** Register them as columns with a sentinel flag (e.g., `IsArg bool`); intro.go renders them as field arguments rather than selectable fields.
- **(b)** Store the arg metadata on the `DBTable` directly (e.g., `Args []DBColumn`) and teach intro.go to read both `Columns` and `Args` when building the GraphQL field.

**Recommend (b)**. Keeps response-shape and request-shape concerns separate; doesn't require a sentinel flag in the column model.

Concretely:

```go
// core/internal/sdata/tables.go, add to DBTable:
Args []DBColumn  // populated for synthetic remote tables

// core/intro.go addTable(), after the columns loop:
if len(table.Args) > 0 {
    for _, a := range table.Args {
        ftQS.Args = append(ftQS.Args, in.argFromColumn(a))
    }
}
```

The synthetic table then carries both:
- `Columns` — the response shape (selectable fields)
- `Args` — the request shape (path + query params)

intro.go's `len(Columns) == 0` early-return needs widening: skip only when both `len(Columns) == 0 && len(Args) == 0`.

**Side benefit:** This same fix makes the just-landed row-join remote tables introspectable for the first time. Today `is_profile` works at runtime but isn't in the schema; this PR exposes it. Users who care about IDE auto-complete will notice.

## 5. File-by-File Change List

Revised after §8 investigation. Estimated diff size: ~700–1000 LOC.

### Modified files

| File | Change | Est. LOC |
|---|---|---|
| `core/internal/sdata/tables.go` | Add `Args []DBColumn` field to `DBTable` for synthetic remotes carrying argument metadata | ~5 |
| `core/internal/sdata/schema.go` | `addRemoteRel` early-returns for parent-less remotes (no FK lookup, no graph edge) | ~10 |
| `core/internal/qcode/qcode.go` | Add `ExtraArgs map[string]string` to `Select`; after `co.Find()` for top-level selects, set `sel.Rel.Type = RelRemote` when `Ti.Type == "remote"` and `Ti.PrimaryCol.FKeyTable == ""` | ~15 |
| `core/internal/qcode/args.go` | In the `default` branch of `compileSelectArgs`, allow unknown string-valued args when `sel.Ti.Type == "remote"`; stash in `sel.ExtraArgs`. Real-table behaviour unchanged. | ~15 |
| `core/internal/qcode/fields.go` | Ensure `SkipTypeRemote` is set on top-level remote selects (the parent-add path doesn't fire for top-level) | ~10 |
| `core/gstate.go` | Skip SQL execution when `qc.Remotes == len(qc.Roots)`; seed `s.data` with placeholder JSON via new `seedRemotePlaceholders` | ~35 |
| `core/remote_join.go` | Branch `parentFieldIds` and `resolveRemotes` on `sel.ParentID == -1`; use field-name-based marker for top-level | ~60 |
| `core/openapi_bridge.go` | Extend `openapiBridge` (new `topLevel bool`, `op *OpDescriptor` fields); add `buildTopLevelParams`; honour `IsArrayResponse`; widen `validateOpenAPINoCollisions` to cover SingleByID/List ops; synthesise top-level resolvers and tables (with synthetic columns + args) in addition to row-join configs | ~150 |
| `core/intro.go` | Widen the early-return in `addTable` from `len(Columns) == 0` to `len(Columns) == 0 && len(Args) == 0`; emit `Args` from `DBTable.Args` as field arguments. Side-benefit: row-join remotes become introspectable. | ~30 |
| `CONFIG.md` | Update the "Limitations" section to remove "row-joins only" and document top-level usage | ~40 |

### New files

| File | Purpose | Est. LOC |
|---|---|---|
| `core/openapi/columns.go` | Build synthetic `DBColumn` entries from an OpenAPI response schema (for `DBTable.Columns`) and from path/query params (for `DBTable.Args`) | ~120 |
| `core/openapi_toplevel_test.go` | End-to-end engine test: spec → top-level query → mocked upstream → response. Mirrors `openapi_test.go` shape from the row-join PR. | ~250 |
| `core/openapi/columns_test.go` | Unit tests for response-schema → DBColumn synthesis (table-driven across object, array, nested shapes) | ~150 |

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

### Risks (post-investigation)

1. **Mixed roots (some SQL, some remote) need both paths to coexist.** Mitigation: scenarios 3 + 4 in the test plan exercise this.

2. **The qcode `ExtraArgs` change touches a hot, well-tested path.** §8.1 found the args switch hard-rejects unknowns. The fix is gated on `sel.Ti.Type == "remote"` so real-table behaviour is unchanged, but the qcode test suite must pass without modification — any breakage there means the gate is wrong. Mitigation: full qcode test run before merging the qcode commit.

3. **psql edge cases when all roots are remote.** §8.2 confirmed psql doesn't skip remote roots — gstate guards SQL execution entirely (Option B from §4.3), so psql is bypassed in the all-remote case. Mixed queries still emit SQL through psql normally; verified safe.

4. **Introspection backfill changes the public schema for existing deployments.** §8.3 found row-join remotes are currently invisible. Once the `intro.go` widening lands, deployments that already have remote_api or row-join OpenAPI configs will see new fields appear in their schema output. Most clients won't care, but a few that lock-step the schema (e.g. via persisted-query frameworks with strict equality checks) might. Mitigation: call this out in the changelog and the §11.5 rollout note.

5. **`IsArrayResponse` is now load-bearing.** §8.6 found it's set by the classifier but never read. The classifier's existing tests don't assert correctness of that flag against a wide variety of response schemas. Mitigation: add focused tests for `deriveResultPath` and `IsArrayResponse` derivation as part of step 5 (bridge work).

### Rollback

The feature can be disabled via a single config flag: `openapi.enable_top_level_virtual_tables: false` (default false during initial rollout, flip to true after burn-in). All the registration code becomes conditional on this flag. Rolling back is `git revert` of one PR with no data-layer impact (no DB migrations, no on-disk format changes).

Recommended rollout: ship with the flag default-false, run in a non-production environment for a sprint, then flip default-true in a follow-up version bump.

## 8. Resolved Questions (verified against the codebase)

Investigation done before scoping (see commit history of this doc). Each answer below has been verified by reading the actual code, not inferred.

### 8.1 GraphQL field arguments on `qcode.Select` — **needs a qcode change**

`core/internal/qcode/args.go:13-75` is a fixed switch on a hard-coded set of arg names (`id`, `where`, `limit`, `offset`, `orderBy`, etc.). Any unknown arg returns `unknownArg(a)` (line 67) → `fmt.Errorf("unknown argument '%s'", arg.Name)`. There is **no catch-all field on `Select`**. `sel.IArgs` exists (`qcode.go:105`) but only stores recognised internal args.

**Decision:** Add `ExtraArgs map[string]string` to `qcode.Select`. In `compileSelectArgs`, replace the `default: return unknownArg(a)` with a check: if `sel.Ti.Type == "remote"` and the arg has a string-valued literal, stash it in `sel.ExtraArgs`; otherwise still error. Scoped behaviour change — no impact on real DB tables.

### 8.2 psql emission for `SkipTypeRemote` at root — **falls through to render**

`core/internal/psql/query.go:256-335` only special-cases `SkipTypeDrop` (line 259), `SkipTypeUserNeeded`, `SkipTypeBlocked`, `SkipTypeNulled` (lines 267-277). `SkipTypeRemote` reaches the `default` and renders via `RenderJSONRootField`. Child columns *do* skip remote selects (`psql/columns.go:73-76`), but root selects don't. So a query with all-remote roots today emits broken SQL.

**Decision:** Confirmed Option B from §4.3 — gstate is the gatekeeper. When `qc.Remotes == len(qc.Roots)`, skip SQL execution entirely and seed `s.data` directly with placeholders. Single-line guard in `gstate.compileAndExecute`.

### 8.3 Introspection visibility for synthetic remote tables — **hidden today**

`core/intro.go:315` early-returns when `len(table.Columns) == 0`. `initRemote` (`core/resolve.go:101`) creates synthetic remote tables with only an internal `__<name>_<col>` PK (line 89), which isn't exposed via `Columns`. Net effect: **row-join remotes from the just-landed PR are already invisible to introspection.** Today's `is_profile { ... }` works because qcode resolves the field via `tindex`, but `intro.go` skips it when serving the schema. Clients/IDEs can't auto-complete it.

**Decision:** This is two distinct bugs. Fix in this PR:
- Synthesise virtual columns for top-level path/query params, register them on the synthetic table, so `addTable` in intro.go emits both the type and its argument shape.
- Modify `addTable` to additionally accept tables that have *args* but no columns, surfacing the type with its arguments.

The row-join introspection invisibility is **a pre-existing bug surfaced by this investigation** — addressed in the `chore/sdata-strict-naming` PR (§12.7) alongside the AddTable/addAliases fixes, since those touch the same surface.

### 8.4 Schema namespace for synthetic remotes — **default schema, easy to override**

`DBSchema.tindex` is keyed by `<schema>:<name>` (`sdata/schema.go:43`); `nameIndex` provides name-only fallback (line 44). `DBTable.Schema` allows per-table override. Today `initRemote` uses `pdb.dbinfo.Schema` (the primary DB's default).

**Decision:** Stay on the primary DB's default schema for now — simplest, no new namespace machinery, and the `<spec_key>_<op>` ExposeAs convention plus the §12 collision check handle the actual risk (name shadowing). A separate logical "openapi" schema can be a config option later if multi-spec deployments report friction.

### 8.5 Argument name normalisation — **handled at compile time**

`qcode/util.go:10-15` `ParseName()` honours `EnableCamelcase` to convert field names to snake_case. The args switch (`args.go:25-29`) already accepts both `orderBy` and `order_by`. So the GraphQL surface is bilingual.

**Decision:** For OpenAPI args, register them in `ExtraArgs` under the spec's exact name (typically camelCase like `actorId`). Bridge looks up by exact name when building `CallParams`. If `EnableCamelcase` is on, the GraphQL parser already normalises field-level names; arg names pass through unchanged. No additional normalisation layer needed in this PR.

### 8.6 Single-row vs list response shapes — **bridge must handle, today's row-join doesn't**

`OpDescriptor.IsArrayResponse` is set by the classifier (`openapi/classifier.go:135`) but **never read** by `openapi_bridge.go` or `caller.go`. The row-join code happens to work because parents always expect a single object, and the result-path stripping plus `jsn.Replace` is shape-agnostic.

For top-level:
- `OpModeSingleByID` → JSON object → wraps as a single-element value in `s.data`.
- `OpModeList` → JSON array → wraps as an array.

**Decision:** Add a shape check in the bridge's top-level path. If `IsArrayResponse=true` and the upstream returns a non-array after result-path stripping, error out — the spec lied. If `IsArrayResponse=false` and the upstream returns an array, take element zero (or error if empty, depending on spec). The IsArrayResponse flag becomes load-bearing for the first time.

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

Investigation done (see §8); estimate reflects the qcode/intro work that grew once the actual code was read.

- Implementation steps 1–4 (sdata `Args` field + parent-less remote, qcode `ExtraArgs` + args.go branch + RelRemote marking, gstate all-remote guard, remote_join parent-less branch): 2.5 days
- Implementation steps 5–6 (bridge top-level + IsArrayResponse + collision-check widening + columns.go, intro.go widening): 1.5 days
- Tests (unit + scenario, including `IsArrayResponse` derivation tests): 1.5 days
- Documentation + polish: 0.5 day

**Total: ~6 days of focused work.** Slightly above the original estimate because qcode `ExtraArgs` and the synthetic-columns plumbing are bigger than first scoped, and the introspection backfill picks up extra surface area (synthetic columns from response schemas).

## 11. What This Doesn't Cover (out of scope, again)

Explicit non-goals so the PR stays focused:

- Write-side support (POST/PUT/PATCH/DELETE → GraphQL mutations). Separate PR.
- Async / export / file-download endpoints. Out of scope by design — GraphJin is a query engine.
- GraphQL SDL or gRPC spec formats. OpenAPI 3 only for the foreseeable future.
- Per-request auth pass-through via inbound HTTP headers. Already mentioned as deferred in row-join PR; remains deferred.
- OpenAPI-only mode (no DB at all). The primary-DB requirement (`api.go:335`) stays; lifting it is a separate architecture change.

## 12. Collision Defence

A real production hazard exists in the existing remote-join machinery and inherits into OpenAPI integration: `dbinfo.AddTable` (`core/internal/sdata/tables.go:474-482`) silently overwrites any existing entry in `tableMap[schema:name]` without a warning. Combined with `initRemote` (`core/resolve.go:101-105`) — which always calls `AddTable` for every registered resolver — this means a remote table named `users` will replace the real `users` table in the index at boot. The first GraphQL query to `users` returns OpenAPI data instead of DB data, with no log line and no error.

This was true before this PR series. The row-join PR landed with the same gap; this section documents the defence that lands with it (commit `978d9cb` on `feat/remote-joins`) and the wider checks the top-level PR must add.

### 12.1 Pure-DB collision handling (existing, for context)

| Surface | Mechanism | File:Line | Strict? |
|---|---|---|---|
| Same name across schemas | `Find()` errors with "use schema prefix to disambiguate" | `sdata/dwg.go:251-288` | ✅ Hard error |
| Multi-database same name | `tableMap` keyed by `schema:name` only | `sdata/tables.go:481` | ⚠️ Last-writer-wins across DBs |
| Joined-query column ambiguity | `colWithTableID` qualifies as `table_N.col` | `psql/util.go:20-28` | ✅ Structural, not name-based |
| User-defined alias | `addAliases` writes to `tindex` | `sdata/dwg.go:68-74` | ❌ Silently shadows real tables |
| Config-time duplicate tables | `tableMap[schema+name]` check at parse | `init.go:25-33` | ✅ Hard error at parse time |

### 12.2 Rules for OpenAPI exposure

Three rules, in order of strictness:

1. **Hard fail at boot** if `ExposeAs` matches a real (non-remote) DB table in the primary schema. This is the production safety guarantee — never let a remote silently shadow a real table.
2. **Hard fail at boot** if two OpenAPI operations resolve to the same `ExposeAs`. The default `<spec_key>_<operationId_snake>` namespacing makes this unlikely, but a user-supplied `expose_as` override can defeat it.
3. **Warn (don't fail)** if `ExposeAs` matches a configured table alias or an existing user-declared resolver. Aliases and resolvers are explicit user choices, so we surface the conflict and let registration order decide.

### 12.3 Where the check runs

In `core/openapi_bridge.go` `loadOpenAPIIntegration`, between `synthesiseRowJoinResolvers(reg)` and the `append(gj.conf.Resolvers, synth...)`. At that point in boot:

- Phase 1 (`discoverAllDatabases`) has populated `pdb.dbinfo.Tables` with real tables.
- Phase 2 (`initResolvers`) has not yet added any synthetic remote tables.
- The schema graph has not yet been built (Phase 3, `finalizeAllDatabases`), so we can still error out cleanly without partial state to roll back.

### 12.4 Error message shape

Errors must:

- **Name the colliding identifier** — both the operation (`<spec>/<opID>`) and the value (`expose_as: "<name>"`).
- **Tell the user how to fix it** — point at the YAML path, e.g., `openapi.<spec>.joins.<op>.expose_as`.
- **Not leak internal types** — keep messages user-facing.

Example:

```
openapi: operation is/getUserById exposes as "users" which collides with a real table in schema public; set 'expose_as' under openapi.is.joins.getUserById to a unique name
```

### 12.5 What the top-level PR adds on top

When `OpModeSingleByID` and `OpModeList` operations also create `dbinfo` entries (per §4.1 of this doc), the same check needs to run for them. The validator in this row-join PR is scoped to synthesised row-join resolvers; the top-level PR widens it to all classifiable operations and their virtual columns (path/query params surfaced as GraphQL field arguments).

Additionally:

- **Argument-name collisions on a single virtual table** — two query params named `id` and a path param named `id` would collide in GraphQL field args. Detect at registry-build time.
- **Virtual table name vs DB column name** — less critical (different namespaces in GraphQL), but worth a warning so introspection output is clean.

### 12.6 Tests

In this PR (`core/openapi_bridge_test.go`):

- `TestCollisionWithRealTableErrors` — synth resolver named `users` against dbinfo with real `users` → hard error mentioning the operation, the name, and `expose_as`.
- `TestCollisionAcrossSpecsErrors` — two synth resolvers both named `audit` from different specs → hard error naming both ops.
- `TestCollisionWithAliasWarnsButPasses` — synth name matches a `Name != Table` alias in `conf.Tables` → log warning, no error.
- `TestCollisionWithExistingResolverWarns` — synth name matches an existing user-declared resolver → log warning.
- `TestNoCollisionsHappyPath` — clean config produces no errors and no log output.
- `TestCollisionCheckHandlesMissingDBInfo` — mock-DB / no-dbinfo path still detects cross-spec collisions without panicking.

In the top-level PR, additional scenarios:

- Top-level virtual table whose name collides with a real table — same hard error.
- Virtual table name collides with a row-join `ExposeAs` — same hard error (single global namespace).
- Argument names within a single operation collide — hard error at classification time.

### 12.7 Closing the pre-existing disambiguation gaps

The OpenAPI-only check above is a band-aid: it stops *one* path (loadOpenAPIIntegration) from creating shadow tables. The underlying weaknesses — `AddTable` overwriting silently and `addAliases` shadowing silently — affect every code path that registers tables, including `remote_api`, future top-level OpenAPI work, and any direct caller. They should be closed at the source. This is a separate, smaller PR (call it `chore/sdata-strict-naming`) sequenced **after** the top-level PR lands, because it touches code paths every other feature depends on.

#### Gap A — `sdata.DBInfo.AddTable` silent overwrite

`tables.go:474-482` writes into `tableMap[schema:name]` unconditionally. Every caller assumes append semantics; the overwrite is a footgun nobody opted into.

**Plan:**

1. Audit all `AddTable` callers: `core/resolve.go:105` (remote registration), test fixtures, anywhere in `sdata` itself. Confirm none rely on overwrite semantics.
2. Change `AddTable` signature to return `error`. On conflict (`tableMap[schema:name]` already populated), return `fmt.Errorf("sdata: table %s.%s already registered", schema, name)`.
3. Add `ReplaceTable(t DBTable)` for the (rare) cases where overwrite is genuinely intended — should mostly be test-only.
4. Update every caller. Most will just propagate the error; the OpenAPI bridge collision check then becomes belt-and-braces (the `sdata` layer is the actual guard).
5. Test: a fixture registering the same `(schema, name)` twice via `AddTable` returns an error; `ReplaceTable` succeeds.

**Effort:** ~half a day. Risk: low — caller audit reveals exactly two production sites today (`resolve.go` and `init.go` table loading), both of which already check elsewhere for duplicates.

#### Gap B — `addAliases` silent shadow

`dwg.go:68-74` writes alias names into `tindex` and `nameIndex` without checking whether the alias name collides with a real table or another alias. A user who aliases `customers → users` and also has a real table named `customers` will get the alias served instead, with no warning.

**Plan:**

1. In `addAliases`, before each `tindex[key] = idx` write, check if the key already maps to a table whose `Name != aliasName`. If so:
   - If the existing entry is a real table (`Type != "remote"` and not itself an alias) → return error: `"sdata: alias %s collides with existing table; rename the alias"`.
   - If the existing entry is another alias → return error naming both aliases.
2. Same fix shape as Gap A: `addAliases` returns `error`, propagated up through `NewDBSchema`.
3. Test: schema with an alias whose name matches a real table fails to build; schema with a non-colliding alias builds cleanly.

**Effort:** ~half a day. Risk: moderate — there may be users who currently rely on silent shadow as a "rename a table" trick. Mitigation: search the `serv/` test fixtures for any such usage; if found, document the migration path before flipping the error on.

#### Gap C — `tableMap` keyed by `schema:name` only (multi-DB)

`tables.go:481`. Two databases with overlapping `(schema, name)` will collide in the index. Today this is masked by `crossDBRels[]` for FK relations, but direct `Find()` calls don't have that fallback.

**Plan:**

1. Extend the key to `db:schema:name`. Audit `Find()` and other readers to thread the database name through.
2. Backfill: where the database name isn't readily available (legacy code paths), default to the empty string — preserves existing single-DB behaviour.
3. Test: `multidb_test.go` already has a same-name-in-two-DBs scenario; assert that `Find()` resolves each correctly with database qualification.

**Effort:** ~1 day. Risk: moderate — `Find()` is hot path; needs care to keep the fast-path lookup zero-allocation.

#### Sequencing and rollback

These three gaps are independent and can ship as separate commits within one PR:

1. Gap A first (smallest blast radius — net new error returns at one well-known site).
2. Gap B second (needs Gap A's error-return convention to be in place).
3. Gap C third (largest, can be deferred indefinitely if multi-DB usage is sparse).

Each commit is independently revertable. None has a data-layer impact.

#### What this leaves unfixed

- **Two databases with identical schema-qualified table names** in *configured* (not just discovered) state will still collide if the user gives them the same logical name in `Config.Databases`. That's a config-validation problem, not an `sdata` problem.
- **Cross-cutting name collisions across roles/permissions** — out of scope for this naming-disambiguation PR. Permission tables aren't in the same namespace as data tables today.

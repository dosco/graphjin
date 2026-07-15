---
title: "Caching And Redis"
description: "Configure response caching, stale-while-revalidate, Redis backend selection, and invalidation."
nav_group: "configure"
doc_kind: "reference"
weight: 40
---

## Response cache

```yaml
caching:
  disable: false
  ttl: 3600
  fresh_ttl: 300
  exclude_tables:
    - audit_logs
```

`ttl` is the hard expiry. `fresh_ttl` controls stale-while-revalidate behavior. Mutations invalidate cache entries through tracked table and primary-key references.

{{< verified by="TestCacheKeyBuilder_DatabaseIsolation" file="core/cache_key_test.go" line="337" >}}
{{< verified by="TestRedisCache_PerEntryTTLsCapButDoNotExtendDefaults" file="serv/cache_redis_test.go" line="180" >}}

Cache keys include query/APQ identity, variables, role, user identity, and database scope. This is important in multi-database deployments: the same GraphQL operation against two source databases must not share a response entry.

{{< verified by="TestCacheKeyBuilder_RoleIsolation" file="core/cache_key_test.go" line="88" >}}
{{< verified by="TestBuildCacheKey_DatabaseScope" file="core/cache_key_test.go" line="350" >}}

## Redis backend

```yaml
redis:
  url: ${REDIS_URL}
```

Redis is useful when multiple GraphJin instances need shared cache state.

`redis.url` has two distinct jobs when the related features are enabled:

- It can store shared response-cache entries and invalidation state.
- It coordinates discovery and semantic builders with leases, fencing tokens,
  active generation IDs, progress, and PubSub notifications.

Discovery snapshots and semantic vectors are never stored in Redis. They stay
under `discovery_cache.path`. A horizontally scaled deployment therefore needs
both Redis and a shared read-after-write filesystem. A single GraphJin process
does not need Redis for discovery or semantic search; it uses in-process
coordination.

See [Discovery Cache And Semantic Search](/configure/discovery-semantic-search/)
for cold and warm startup behavior and the Redis outage matrix.

## Remote fragments and filesystems

Remote fragments, OpenAPI calls, and filesystem tables add source-specific cache refs. Filesystem list queries include prefix refs so a write to `users/1/avatar.png` can invalidate both the exact key and affected list prefixes.

{{< verified by="TestRemoteFragmentRefs_FilesystemListIncludesRootPrefix" file="core/cache_response_test.go" line="61" >}}
{{< verified by="TestRedisCache_FilterExcludedSourceScopedRefs" file="serv/cache_redis_test.go" line="210" >}}

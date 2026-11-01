-- Drop the approximate IVFFlat index on work_memory.embedding in favour of an
-- exact (sequential) cosine scan. work_memory is per-org and small (at most a
-- few thousand rows), so scanning every row is sub-millisecond and gives 100%
-- recall; the approximate index added no speedup at this scale and could skip a
-- relevant memory (its centroids were also built on an empty table). The search
-- SQL is unchanged. Re-add an HNSW index if a single org ever reaches tens of
-- thousands of memories.
DROP INDEX IF EXISTS work_memory_embedding_cosine_idx;

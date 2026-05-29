-- Clustering keys from RESULT_SCAN of SHOW TABLES (query id). Snowflake restricts
-- INFORMATION_SCHEMA, so SHOW is the only metadata source. Best-effort: clustering
-- is a pruning optimization, not a correctness requirement.
SELECT LOWER("schema_name") AS schema_name,
	LOWER("name") AS table_name,
	"cluster_by" AS clustering_key
FROM TABLE(RESULT_SCAN(?))
WHERE "cluster_by" IS NOT NULL
	AND "cluster_by" != '';

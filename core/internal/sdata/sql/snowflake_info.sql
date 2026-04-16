-- Identifiers (db_schema, db_name) are returned in their original case.
-- Real Snowflake stores unquoted identifiers as UPPERCASE; quoted-lowercase
-- objects are reported in their stored case. The Go layer preserves this case
-- through dbinfo and emits identifiers verbatim.
SELECT 0 AS db_version,
	COALESCE(CURRENT_SCHEMA(), 'PUBLIC') AS db_schema,
	COALESCE(CURRENT_DATABASE(), '') AS db_name;

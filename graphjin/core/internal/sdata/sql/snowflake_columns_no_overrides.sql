-- snowflake_columns_no_overrides.sql
--
-- Fallback column-discovery query used when the primary
-- snowflake_columns.sql query fails. The primary query includes a 4th UNION
-- that reads from `_gj_fk_metadata` (an optional FK override table used by
-- the test emulator and by production users who can't declare FKs in DDL).
-- If `_gj_fk_metadata` doesn't exist in the target Snowflake account, the
-- entire primary query errors out. Go falls back to this 3-UNION version
-- that omits the overlay read so schema discovery still succeeds.
--
-- This file must stay byte-identical to snowflake_columns.sql except for
-- the missing 4th UNION — any other divergence silently changes column
-- metadata in the fallback path.
SELECT LOWER(col.table_schema) AS schema_name,
	LOWER(col.table_name) AS table_name,
	LOWER(col.column_name) AS column_name,
	LOWER(col.data_type) AS col_type,
	(
		CASE
			WHEN col.is_nullable = 'NO' THEN TRUE
			ELSE FALSE
		END
	) AS not_null,
	FALSE AS primary_key,
	FALSE AS unique_key,
	(
		CASE
			WHEN UPPER(col.data_type) = 'ARRAY'
			OR col.data_type LIKE '%[]' THEN TRUE
			ELSE FALSE
		END
	) AS is_array,
	FALSE AS full_text,
	'' AS foreignkey_schema,
	'' AS foreignkey_table,
	'' AS foreignkey_column
FROM information_schema.columns col
WHERE col.table_schema NOT IN ('INFORMATION_SCHEMA')
UNION
SELECT LOWER(kcu.table_schema) AS schema_name,
	LOWER(kcu.table_name) AS table_name,
	LOWER(kcu.column_name) AS column_name,
	'' AS col_type,
	FALSE AS not_null,
	(
		CASE
			WHEN tc.constraint_type = 'PRIMARY KEY' THEN TRUE
			ELSE FALSE
		END
	) AS primary_key,
	(
		CASE
			WHEN tc.constraint_type = 'UNIQUE' THEN TRUE
			ELSE FALSE
		END
	) AS unique_key,
	FALSE AS is_array,
	FALSE AS full_text,
	'' AS foreignkey_schema,
	'' AS foreignkey_table,
	'' AS foreignkey_column
FROM information_schema.key_column_usage kcu
	JOIN information_schema.table_constraints tc ON (
		kcu.constraint_catalog = tc.constraint_catalog
		AND kcu.constraint_schema = tc.constraint_schema
		AND kcu.constraint_name = tc.constraint_name
	)
WHERE kcu.table_schema NOT IN ('INFORMATION_SCHEMA')
UNION
SELECT LOWER(fk_kcu.table_schema) AS schema_name,
	LOWER(fk_kcu.table_name) AS table_name,
	LOWER(fk_kcu.column_name) AS column_name,
	'' AS col_type,
	FALSE AS not_null,
	FALSE AS primary_key,
	FALSE AS unique_key,
	FALSE AS is_array,
	FALSE AS full_text,
	LOWER(pk_kcu.table_schema) AS foreignkey_schema,
	LOWER(pk_kcu.table_name) AS foreignkey_table,
	LOWER(pk_kcu.column_name) AS foreignkey_column
FROM information_schema.referential_constraints rc
	JOIN information_schema.key_column_usage fk_kcu ON (
		rc.constraint_catalog = fk_kcu.constraint_catalog
		AND rc.constraint_schema = fk_kcu.constraint_schema
		AND rc.constraint_name = fk_kcu.constraint_name
	)
	JOIN information_schema.key_column_usage pk_kcu ON (
		rc.unique_constraint_catalog = pk_kcu.constraint_catalog
		AND rc.unique_constraint_schema = pk_kcu.constraint_schema
		AND rc.unique_constraint_name = pk_kcu.constraint_name
		AND fk_kcu.position_in_unique_constraint = pk_kcu.ordinal_position
	)
WHERE fk_kcu.table_schema NOT IN ('INFORMATION_SCHEMA');

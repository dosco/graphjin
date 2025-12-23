SELECT 
  'main' as "schema",
  m.name as "table",
  p.name as "column",
  COALESCE(NULLIF(LOWER(p.type), ''), 'text') as "type",
  (p."notnull" > 0) as not_null,
  (p.pk > 0) as primary_key,
  0 as unique_key,
  (LOWER(p.type) IN ('json', 'jsonb') OR p.name = 'tags' OR p.name LIKE '%_ids') as is_array,
  (m.sql LIKE '%USING fts%') as full_text,
  'main' as foreignkey_schema,
  COALESCE(fk."table", '') as foreignkey_table,
  COALESCE(fk."to", '') as foreignkey_column
FROM sqlite_master m
JOIN pragma_table_info(m.name) p
LEFT JOIN pragma_foreign_key_list(m.name) fk ON fk."from" = p.name
WHERE (m.type = 'table' OR m.type = 'view')
AND m.name NOT LIKE 'sqlite_%'
AND m.name NOT LIKE '_gj_%'
ORDER BY m.name, p.cid;

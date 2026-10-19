SELECT 
  'main' as "schema",
  m.name as "table",
  p.name as "column",
  COALESCE(NULLIF(LOWER(p.type), ''), 'text') as "type",
  (p."notnull" > 0) as not_null,
  (p.pk > 0) as primary_key,
  EXISTS (
    SELECT 1
    FROM pragma_index_list(m.name) il
    JOIN pragma_index_info(il.name) ii ON ii.name = p.name
    WHERE il."unique" = 1
      AND il.partial = 0
    GROUP BY il.name
    HAVING COUNT(*) = 1
  ) as unique_key,
  (LOWER(p.type) IN ('json', 'jsonb') OR p.name = 'tags' OR p.name LIKE '%_ids') as is_array,
  (
    -- Check if this is an FTS virtual table column
    m.sql LIKE '%USING fts%'
    OR
    -- Check if there's a content-backed FTS5 table for this table with this column
    EXISTS (
      SELECT 1 FROM sqlite_master fm
      JOIN pragma_table_info(fm.name) fp ON fp.name = p.name
      WHERE fm.type = 'table' 
        AND fm.sql LIKE '%USING fts5%'
        AND fm.sql LIKE '%content=''' || m.name || '''%'
    )
  ) as full_text,
  'main' as foreignkey_schema,
  COALESCE(fk."table", '') as foreignkey_table,
  COALESCE(fk."to", '') as foreignkey_column
FROM sqlite_master m
JOIN pragma_table_info(m.name) p
LEFT JOIN pragma_foreign_key_list(m.name) fk ON fk."from" = p.name
WHERE (m.type = 'table' OR m.type = 'view')
AND m.name NOT LIKE 'sqlite_%'
AND m.name NOT LIKE '_gj_%'
AND (m.sql IS NULL OR LOWER(m.sql) NOT LIKE '%using fts%')
AND NOT EXISTS (
  SELECT 1 FROM sqlite_master fm
  WHERE fm.type = 'table'
    AND LOWER(fm.sql) LIKE '%using fts%'
    AND m.name IN (
      fm.name || '_data',
      fm.name || '_idx',
      fm.name || '_content',
      fm.name || '_docsize',
      fm.name || '_config'
    )
)
ORDER BY m.name, p.cid;

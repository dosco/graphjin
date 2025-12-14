# TODO

## MySQL Array Column Join Support

**Status:** Not Implemented  
**Priority:** Medium  
**Affected Test:** `TestInsertIntoTableAndConnectToRelatedTableWithArrayColumn`, `TestQueryParentAndChildrenViaArrayColumn`

### Problem

MySQL does not support direct equality comparisons between scalar values and JSON array columns. The current query compiler generates SQL like:

```sql
WHERE `categories`.`id` = `products_0`.`category_ids`
```

When `category_ids` is a JSON array (e.g., `[1,2,3,4,5]`), this comparison fails silently in MySQL, returning no results.

### Solution

Implement proper array column join handling in the query compiler for MySQL:

1. **Detect array column joins** in `core/internal/psql/exp.go` when rendering expressions
2. **Use `RenderValArrayColumn`** (already exists in `core/internal/dialect/mysql.go`) to unpack JSON arrays using `JSON_TABLE`
3. **Generate correct SQL** like:
   ```sql
   WHERE `categories`.`id` IN (
     SELECT _gj_jt.* FROM 
     (SELECT CAST(`products_0`.`category_ids` AS JSON) as ids) j,
     JSON_TABLE(j.ids, "$[*]" COLUMNS(id BIGINT PATH "$" ERROR ON ERROR)) AS _gj_jt
   )
   ```

### Files to Modify

- `core/internal/psql/exp.go` - Update `renderOp` or `renderVal` to detect array column comparisons
- `core/internal/dialect/mysql.go` - Ensure `RenderValArrayColumn` is called appropriately
- `core/internal/psql/compiler.go` - May need updates to relationship join logic

### Testing

- Re-enable `TestInsertIntoTableAndConnectToRelatedTableWithArrayColumn` for MySQL
- Add additional tests for:
  - One-to-many relationships with array columns
  - Many-to-many relationships with array columns
  - Nested queries with array column joins

### References

- Existing implementation: `MySQLDialect.RenderValArrayColumn` in `core/internal/dialect/mysql.go`
- PostgreSQL equivalent uses native array operators (`@>`, `&&`, etc.)

---

## MySQL Bulk Insert with Explicit IDs

**Status:** Not Implemented  
**Priority:** Low  
**Affected Tests:** `Example_insertInlineBulk`, `Example_insertBulk`, and other bulk insert tests

### Problem

The current linear execution implementation uses inline variable assignment (`@var := value`) to capture explicitly provided primary keys:

```sql
INSERT INTO users (id, email) SELECT @users_0 := 1008, 'one@test.com' UNION SELECT @users_0 := 1009, 'two@test.com'
```

However, this approach only captures the **last** value (1009 in this example), causing the query phase to only return one row instead of all inserted rows.

### Solution Options

1. **Use a temporary table** to store all inserted IDs:
   ```sql
   CREATE TEMPORARY TABLE _gj_ids_users (id BIGINT);
   INSERT INTO users (id, email) SELECT 1008, 'one@test.com' UNION SELECT 1009, 'two@test.com';
   INSERT INTO _gj_ids_users SELECT id FROM users WHERE id IN (1008, 1009);
   SELECT ... WHERE users.id IN (SELECT id FROM _gj_ids_users);
   DROP TEMPORARY TABLE _gj_ids_users;
   ```

2. **Extract IDs from input JSON** and use them directly in the WHERE clause:
   ```sql
   INSERT INTO users ...;
   SELECT ... WHERE users.id IN (SELECT id FROM JSON_TABLE(?, '$[*]' COLUMNS(id BIGINT PATH '$.id')));
   ```

3. **Accept the limitation** and document that bulk inserts with explicit IDs are not fully supported in MySQL linear execution mode.

### Recommended Approach

Option 2 is the cleanest. Modify `compileLinearMutation` to:
- Extract the list of explicit IDs from the input JSON when available
- Use this list in the injected WHERE clause instead of relying on `@var`
- This requires parsing the mutation input to detect explicit PK values

### Files to Modify

- `core/internal/psql/mutate.go` - Update `compileLinearMutation` to extract IDs from input
- `core/internal/psql/mutate.go` - Update `renderLinearInsert` to handle bulk ID extraction

### Testing

- Re-enable all bulk insert tests for MySQL
- Verify both explicit ID and auto-increment scenarios work correctly

# Implementing New Query Operations in GraphJIN

This guide documents the process for adding new query operations to GraphJIN, based on implementing JSON path operations for issue #519. Follow this systematic approach when adding new filtering, comparison, or data access operations.

## Overview

GraphJIN's architecture separates query compilation (`qcode`) from SQL generation (`psql`). New operations require changes to both packages plus comprehensive testing.

## Step-by-Step Implementation Process

### 1. Research and Planning

**Before coding, understand:**
- The exact GraphQL syntax you want to support
- How other databases handle similar operations
- Existing GraphJIN patterns for similar features
- Database-specific SQL syntax differences

**Example Research Questions:**
- What GraphQL syntax should `{ validity_period: { issue_date: { lte: "2024-01-01" } } }` generate?
- How do PostgreSQL and MySQL handle JSON path operations differently?
- Are there existing JSON operations in GraphJIN to learn from?

### 2. Create Test Cases First (TDD Approach)

GraphJIN follows Test-Driven Development. Always create tests before implementation.

**A. Add Test Data Schema**

If needed, add test tables to all database schema files. **IMPORTANT: When adding test tables, you MUST add them to ALL database schema files to ensure consistency across different database backends.**

**PostgreSQL** (`/tests/postgres.sql`):
```sql
-- Add test table for new feature
CREATE TABLE your_test_table (
  id BIGSERIAL PRIMARY KEY,
  json_column JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Insert test data
INSERT INTO your_test_table (id, json_column) VALUES
  (1, '{"nested": {"field": "value1"}}'),
  (2, '{"nested": {"field": "value2"}}');
```

**MySQL** (`/tests/mysql.sql`):
```sql
-- Add test table for new feature
CREATE TABLE your_test_table (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  json_column JSON NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Insert test data
INSERT INTO your_test_table (id, json_column, created_at) VALUES
  (1, '{"nested": {"field": "value1"}}', '2024-01-01 00:00:00'),
  (2, '{"nested": {"field": "value2"}}', '2024-01-01 00:00:01');
```

**SQL Server** (`/tests/mssql.sql`):
```sql
-- Add test table for new feature
CREATE TABLE your_test_table (
  id bigint IDENTITY(1,1) PRIMARY KEY,
  json_column nvarchar(1024) NOT NULL,
  created_at timestamp NOT NULL DEFAULT current_timestamp
);

-- Insert test data
INSERT INTO your_test_table (json_column, created_at) VALUES
  ('{"nested": {"field": "value1"}}', '2024-01-01 00:00:00'),
  ('{"nested": {"field": "value2"}}', '2024-01-01 00:00:01');
```

**CockroachDB** (`/tests/cockroach.sql`):
```sql
-- Add test table for new feature
CREATE TABLE your_test_table (
  id BIGSERIAL PRIMARY KEY,
  json_column JSON NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Insert test data
INSERT INTO your_test_table (id, json_column, created_at) VALUES
  (1, '{"nested": {"field": "value1"}}', '2024-01-01 00:00:00'),
  (2, '{"nested": {"field": "value2"}}', '2024-01-01 00:00:01');
```

**⚠️ Critical Note:** Failing to add test tables to all schema files will cause tests to fail with "table not found" errors when running against different database backends. Always verify that your schema changes are present in all four database files:
- `postgres.sql`
- `mysql.sql`
- `mssql.sql`
- `cockroach.sql`

**B. Create Test Cases**

Add example tests to `/tests/query_test.go`:

```go
func Example_yourNewFeature() {
    // Test the main syntax
    gql := `
    query {
        your_test_table(where: { json_column: { nested: { field: { eq: "value1" } } } }) {
            id
            json_column
        }
    }`

    conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
    gj, err := core.NewGraphJin(conf, db)
    if err != nil {
        panic(err)
    }

    res, err := gj.GraphQL(context.Background(), gql, nil, nil)
    if err != nil {
        fmt.Println(err)
    } else {
        printJSON(res.Data)
    }
    // Output: {"your_test_table":[{"id":1,"json_column":{"nested":{"field":"value1"}}}]}
}

func Example_yourNewFeatureAlternativeSyntax() {
    // Test alternative syntax if applicable
    gql := `
    query {
        your_test_table(where: { json_column_nested_field: { eq: "value1" } }) {
            id
            json_column
        }
    }`

    // ... rest of test
    // Output: {"your_test_table":[{"id":1,"json_column":{"nested":{"field":"value1"}}}]}
}
```

**C. Run Tests to See Failures**

```bash
go test ./tests -v -run="Example_yourNewFeature" -timeout=30s
```

Tests should fail initially - this confirms what needs to be implemented.

### 3. Extend qcode Package (Query Compilation)

**A. Add New Expression Operators**

Edit `/core/internal/qcode/qcode.go`:

```go
// Add new operators to ExpOp enum
const (
    // ... existing operators
    OpYourNewOp       // Your new operation
    OpYourNewOpAlt    // Alternative syntax if needed
)
```

**B. Regenerate String Types**

```bash
cd /Users/vr/src/graphjin/core/internal/qcode
/Users/vr/go/bin/stringer -linecomment -type=QType,MType,SelType,FieldType,SkipType,PagingType,AggregrateOp,ValType,ExpOp -output=./gen_string.go
```

**C. Extend Expression Structure (if needed)**

If your operation requires new data fields, add them to the `Exp` struct:

```go
type Exp struct {
    // ... existing fields
    Left struct {
        // ... existing fields
        YourNewField []string  // Add fields as needed
    }
    Right struct {
        // ... existing fields
        YourNewField []string  // Add fields as needed
    }
}
```

**D. Implement Expression Parsing**

Edit `/core/internal/qcode/exp.go`:

```go
// Add required imports
import (
    "errors"
    "fmt"
    "strings"  // Add if using string operations
    // ... other imports
)

// For nested object syntax, add to parseNode() function:
func (ast *aexpst) parseNode(av aexp, node *graph.Node, selID int32) (*Exp, error) {
    // ... existing code

    // Add after processNestedTable check:
    if ok, err := ast.processYourNewOperation(av, ex, node, selID); err != nil {
        return nil, err
    } else if ok {
        return ex, nil
    }

    // ... rest of function
}

// Implement your new operation processor:
func (ast *aexpst) processYourNewOperation(av aexp, ex *Exp, node *graph.Node, selID int32) (bool, error) {
    // Check if this operation applies to this node
    nn := ast.co.ParseName(node.Name)
    col, err := av.ti.GetColumn(nn)
    if err != nil {
        return false, nil  // Not applicable
    }

    // Check if column type supports your operation
    if col.Type != "jsonb" && col.Type != "json" {
        return false, nil
    }

    // Parse the nested structure and set up the expression
    // Set ex.Left.Col, ex.Left.Path, etc.

    return true, nil
}

// For alternative syntax, modify processColumn() function:
func (ast *aexpst) processColumn(av aexp, ex *Exp, node *graph.Node, selID int32) (bool, error) {
    nn := ast.co.ParseName(node.Name)

    // Add syntax detection (e.g., underscore, special operators)
    if strings.Contains(nn, "_") {
        // Parse alternative syntax
        // Set up ex.Left fields appropriately
        return true, nil
    }

    // ... existing code
}
```

### 4. Extend psql Package (SQL Generation)

**A. Handle New Operations in renderOp**

Edit `/core/internal/psql/exp.go`:

```go
// In renderOp() function, add SQL generation logic:
func (c *expContext) renderOp(ex *qcode.Exp) {
    // ... existing code

    // Add before existing column rendering:
    if len(ex.Left.YourNewField) > 0 {
        c.renderYourNewOperation(table, colName, ex.Left.YourNewField, ex.Left.ID)
    } else {
        // existing column rendering
    }

    // ... rest of function
}
```

**B. Implement Database-Specific SQL Generation**

```go
// Add new function for your operation:
func (c *expContext) renderYourNewOperation(table, colName string, data []string, selID int32) {
    // Render base column
    if selID == -1 {
        c.colWithTable(table, colName)
    } else {
        c.colWithTableID(table, selID, colName)
    }

    // Generate database-specific SQL
    switch c.ct {
    case "mysql":
        // MySQL-specific syntax
        c.w.WriteString(`->>'$.`)
        for i, element := range data {
            if i > 0 {
                c.w.WriteString(`.`)
            }
            c.w.WriteString(element)
        }
        c.w.WriteString(`'`)
    default:
        // PostgreSQL-specific syntax
        for _, element := range data {
            c.w.WriteString(`->>'`)
            c.w.WriteString(element)
            c.w.WriteString(`'`)
        }
    }
}
```

### 5. Testing and Validation

**A. Run Tests Iteratively**

```bash
# Test your specific feature
go test ./tests -v -run="Example_yourNewFeature" -timeout=30s

# Test qcode package
go test ./core/internal/qcode -v -timeout=30s

# Test psql package
go test ./core/internal/psql -v -timeout=30s
```

**B. Debug Common Issues**

1. **Missing imports:** Add required imports like `strings`
2. **Wrong Path field:** Ensure you're using `ex.Left.Path` for column-side data, `ex.Right.Path` for value-side data
3. **SQL syntax errors:** Check database-specific SQL generation
4. **Compilation errors:** Regenerate string types after adding new operators

**C. Validate Multiple Scenarios**

Test various combinations:
- Simple single-level operations
- Multi-level nested operations
- Different comparison operators (eq, lt, gt, etc.)
- Edge cases (empty values, null values)
- Both PostgreSQL and MySQL if applicable

### 6. Database Compatibility

**A. PostgreSQL vs MySQL Differences**

Common differences to handle:
- **JSON Path Syntax:** PostgreSQL uses `->>'key'`, MySQL uses `->>'$.key'`
- **Function Names:** PostgreSQL `jsonb_*`, MySQL `JSON_*`
- **Type Casting:** Different casting syntax

**B. Test Both Databases**

If possible, test with both databases. If not, ensure SQL syntax follows documented patterns.

### 7. Documentation

**A. Update Test Comments**

Document what each test validates:

```go
func Example_yourNewFeature() {
    // Test case for issue #XXX: Description of feature
    // This tests the nested object syntax for your new operation
    gql := `...`
    // ...
}
```

**B. Add Code Comments**

Comment complex logic:

```go
// processYourNewOperation handles nested object syntax for your operation
// It parses structures like { json_col: { nested: { field: { op: value } } } }
func (ast *aexpst) processYourNewOperation(...) {
    // Check if this is a JSON/JSONB column with nested path
    // ...
}
```

## Common Patterns and Best Practices

### Error Handling

```go
// Always check column existence first
col, err := av.ti.GetColumn(nn)
if err != nil {
    return false, nil  // Return false for "not applicable", not error
}

// Validate column types
if col.Type != "jsonb" && col.Type != "json" {
    return false, nil
}
```

### Database-Specific Code

```go
// Use switch statement for database-specific logic
switch c.ct {
case "mysql":
    // MySQL-specific implementation
default:
    // PostgreSQL implementation (default)
}
```

### Expression Structure Usage

```go
// Column information goes in Left
ex.Left.ID = selID
ex.Left.Col = col
ex.Left.Path = []string{pathElements...}  // For JSON paths

// Value information goes in Right
ex.Right.Val = node.Val
ex.Right.ValType = ValStr
```

### Testing Strategy

1. **Start Simple:** Single-level operations first
2. **Add Complexity:** Multi-level nesting
3. **Test Edge Cases:** Empty values, special characters
4. **Validate SQL:** Ensure generated SQL is correct
5. **Cross-Database:** Test both PostgreSQL and MySQL syntax

## Troubleshooting Common Issues

### Compilation Errors

```bash
# Missing stringer binary
go install golang.org/x/tools/cmd/stringer@latest

# Regenerate after adding operators
cd /Users/vr/src/graphjin/core/internal/qcode && go generate
```

### SQL Generation Issues

```sql
-- Check generated SQL by examining test failures
-- Look for patterns like:
WHERE (("table"."column"->>'path') = 'value')  -- PostgreSQL
WHERE (("table"."column"->>'$.path') = 'value') -- MySQL
```

### Test Failures

1. **Expected output mismatch:** Update expected output in test comments
2. **Column not found:** Ensure test schema includes required columns
3. **Parser errors:** Check GraphQL syntax in test queries
4. **Table not found errors:**
   - **Symptom:** Tests fail with "table not found: db.your_table" errors
   - **Cause:** Missing table definition in one or more database schema files
   - **Solution:** Verify table exists in ALL schema files:
     ```bash
     # Check if table exists in all schema files
     grep -l "your_table_name" tests/*.sql
     # Should return all 4 files: postgres.sql, mysql.sql, mssql.sql, cockroach.sql
     ```
   - **Fix:** Add missing table definitions with database-appropriate syntax

## Example: Complete JSON Path Implementation

See the implementation of JSON path operations (issue #519) as a reference:

- **Files Modified:**
  - `/core/internal/qcode/qcode.go` - Added operators
  - `/core/internal/qcode/exp.go` - Added parsing logic
  - `/core/internal/psql/exp.go` - Added SQL generation
  - `/tests/postgres.sql` - Added quotations test table and data
  - `/tests/mysql.sql` - Added quotations test table and data
  - `/tests/mssql.sql` - Added quotations test table and data
  - `/tests/cockroach.sql` - Added quotations test table and data
  - `/tests/query_test.go` - Added test cases

- **Key Features Implemented:**
  - Nested object syntax: `{ validity_period: { issue_date: { lte: "2024-01-01" } } }`
  - Alternative syntax: `{ metadata_foo: { eq: true } }`
  - Cross-database compatibility (PostgreSQL and MySQL)
  - Comprehensive test coverage

This implementation serves as a complete example of the process documented in this guide.
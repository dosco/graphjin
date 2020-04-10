---
sidebar: auto
---

# Super Graph Codebase Explained

Super Graph code is made up of a number of packages. We have done our best to keep each package small and focused. Let us begin by looking at some of these packages.

1. qcode - GraphQL lexer and parser.
2. psql - SQL generator
3. serv - HTTP Endpoint, Configs, CLI, etc
4. rails - Rails cookie and session store decoders

## QCODE

This package contains the core of the GraphQL compiler it handling the lexing and parsing of the GraphQL query transforming it into an internal representation called
`QCode`.

This is the first step of the compiling process the `func NewCompiler(c Config)` function creates a new instance of this compiler which has it's own config. 

Keep in mind QCode has no knowledge of the Database structure it is designed to be a fast GraphQL parser. Care is taken to keep memory allocations to a minimum.

```go
const (
	opQuery
	opMutate
  ...
)

type QCode struct {
	Type      QType
  Selects   []Select
  ...
}

type Select struct {
	ID         int32
	ParentID   int32
	Args       map[string]*Node
	Name       string
	FieldName  string
	Cols       []Column
	Where      *Exp
	OrderBy    []*OrderBy
	DistinctOn []string
	Paging     Paging
	Children   []int32
	Functions  bool
	Allowed    map[string]struct{}
	PresetMap  map[string]string
	PresetList []string
}
```

But before the incoming GraphQL query can be turned into QCode it must first be tokenzied by the lexer `lex.go`. As the tokenzier walks the bytes of the query it generates tokens `item` structs which are then consumed by the next step the parser `parse.go`.

```go
type item struct {
	typ  itemType 
	pos  Pos
	end  Pos
}
```

For exmple a simple query like `query getUser { user { id } }` will be converted into several tokens like below.

```go
item{itemQuery, 0, 4} // query
item{itemName, 6, 12} // getUser
item{itemObjOpen, 16, 20} // {
...
```

These tokens are then fed into the parser `parse.go` the parser does the work of generating an abstract syntax tree (AST) from the tokens. This AST is an internal representation (data structure) and is not exposed outside the package. Since the AST is a tree a stack `stack.go` is used to walk the tree and generate the QCode AST. The QCode data structure is also a tree (represented as an array). This is then returned to the caller of the compile function.

```go
type Operation struct {
	Type    parserType
	Name    string
	Args    []Arg
	Fields  []Field
}

type Field struct {
	ID        int32
	ParentID  int32
	Name      string
	Alias     string
	Args      []Arg
	Children  []int32
}
```

## PSQL

This package is responsible for generating Postgres SQL from the QCode AST. There are various GraphQL query types (Query, Mutation, etc). And several more sub types like single root or multi-root queries, various types of mutations (insert, update delete, bulk insert, etc). This package is designed to be able to generate SQL for all of those types.

In addition to QCode variable data is also passed to the compile function within this package. Variables are decoded to derive what is being inserted and what kind of insert is it single or bulk. This information is not available in the GraphQL query its passed in seperatly via variables. This package is able to put all this together and generate the right SQL code.

The entry point of this package is in `query.go`. The database schema must be passed in the config object when creating a new compiler instance `NewCompiler`. The functions to extract this schema from the database are also part of this package `tables.go`. The `GetTables` functions fetches all the tables from the database and `GetColumns` fetches columns and relationship information.

```go
func NewCompiler(conf Config) *Compiler {
	return &Compiler{conf.Schema, conf.Vars}
}

func (co *Compiler) Compile(qc *qcode.QCode, w io.Writer, vars Variables) (uint32, error) {
	switch qc.Type {
	case qcode.QTQuery:
		return co.compileQuery(qc, w)
	case qcode.QTInsert, qcode.QTUpdate, qcode.QTDelete, qcode.QTUpsert:
		return co.compileMutation(qc, w, vars)
	}

	return 0, fmt.Errorf("Unknown operation type %d", qc.Type)
}
```

GraphQL, input is first converted to QCode.

```graphql
query {
  user {
    id
  }
  posts {
    title
  }
}
```

SQL, in reality the generated SQL is far more complex single it has to be very efficient, leverage the power of Postgres, support RBAC (Role based access control) and all of this must be done in a single SQL query.

```sql
SELECT users.id, posts.title FROM users, posts;
```

## SERV

The `serv` package constains most of code that turns the above compiler into an HTTP service. It also includes authentication middleware, remote join resolvers, config parsering, database migrations and seeding commands.

Another big feature that this package handles is the `allow.list` management code. In production mode parsing the allow list file and registering prepared statements to adding GraphQL queries to this file in development mode.

Currently the following global variables are referrenced across the package. In future I'd prefer to move these into a context struct and pass that around instead.

```go
var (
	logger   zerolog.Logger  // logger for everything but errors
	errlog   zerolog.Logger  // logger for errors includes line numbers
	conf     *config         // parsed config
	confPath string          // path to the config file
	db       *pgxpool.Pool   // database connection pool
	schema   *psql.DBSchema  // database tables, columns and relationships
	qcompile *qcode.Compiler // qcode compiler
	pcompile *psql.Compiler  // postgres sql compiler
)
```

## Testing

There are several unit tests and benchmark tests `parse_test.go`) included. There are also scripts included for memory `pprof_cpu.sh` and cpu `pprof_cpu.sh` profiling. 

```go
// Test to ensure synthetic tables gnerate the correct SQL
func syntheticTables(t *testing.T) {
	gql := `query {
		me {
			email
		}
	}`

	sql := `SELECT json_object_agg('me', json_0) FROM (SELECT row_to_json((SELECT "json_row_0" FROM (SELECT ) AS "json_row_0")) AS "json_0" FROM (SELECT "users"."email" FROM "users" WHERE ((("users"."id") = '{{user_id}}' :: bigint)) LIMIT ('1') :: integer) AS "users_0" LIMIT ('1') :: integer) AS "sel_0"`

	resSQL, err := compileGQLToPSQL(gql, nil, "user")
	if err != nil {
		t.Fatal(err)
	}

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}
```

You can run tests within each package or across the entire app. It is usually the fastest to first write a test and then build the feature to satisfy it.

```
go test -v ./...
```

Memory profiling can help find where allocations are happining within the package code.

```bash
$ cd ./psql
$ ./pprof_mem.sh
goos: darwin
goarch: amd64
pkg: github.com/dosco/super-graph/psql
BenchmarkCompile-8                 52567             19401 ns/op            3918 B/op         61 allocs/op
BenchmarkCompileParallel-8        219548              5684 ns/op            3938 B/op         61 allocs/op
PASS
ok      github.com/dosco/super-graph/psql       2.582s
Type: alloc_space
Time: Nov 29, 2019 at 11:59pm (EST)
Entering interactive mode (type "help" for commands, "o" for options)
(pprof) top
Showing nodes accounting for 880.59MB, 80.63% of 1092.14MB total
Dropped 33 nodes (cum <= 5.46MB)
Showing top 10 nodes out of 35
      flat  flat%   sum%        cum   cum%
      22MB  2.01%  2.01%   903.57MB 82.73%  github.com/dosco/super-graph/qcode.(*Compiler).Compile
         0     0%  2.01%   862.98MB 79.02%  github.com/dosco/super-graph/psql.BenchmarkCompileParallel.func1
         0     0%  2.01%   862.98MB 79.02%  testing.(*B).RunParallel.func1
  461.95MB 42.30% 44.31%   760.53MB 69.64%  github.com/dosco/super-graph/qcode.(*Compiler).compileQuery
  396.63MB 36.32% 80.63%   396.63MB 36.32%  github.com/dosco/super-graph/util.NewStack
         0     0% 80.63%   252.07MB 23.08%  github.com/dosco/super-graph/qcode.(*Compiler).compileArgs
         0     0% 80.63%   228.15MB 20.89%  testing.(*B).runN
         0     0% 80.63%   227.63MB 20.84%  github.com/dosco/super-graph/psql.BenchmarkCompile
         0     0% 80.63%   227.63MB 20.84%  testing.(*B).launch
         0     0% 80.63%   187.04MB 17.13%  github.com/dosco/super-graph/psql.(*Compiler).Compile
```

## Benchmarking

Most packages contain benchmark tests to ensure new features don't introduce a significant regression to performance.

```bash
$ cd ./psql
$ go test -v -run=xx -bench=.
goos: darwin
goarch: amd64
pkg: github.com/dosco/super-graph/psql
BenchmarkCompile-8                 60775             19076 ns/op            3919 B/op         61 allocs/op
BenchmarkCompileParallel-8        207847              5172 ns/op            3937 B/op         61 allocs/op
PASS
ok      github.com/dosco/super-graph/psql       2.530s
```

## Reach out

If you'd like me to explain other parts of the code please reach out over Twitter or Discord. I'll keep adding to this doc as I get time.

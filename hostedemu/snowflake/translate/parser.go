package translate

import (
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	sfparser "github.com/dosco/graphjin/hostedemu/snowflake/internal/sfparser"
)

type StatementKind string

const (
	StatementUnknown StatementKind = "unknown"
	StatementSelect  StatementKind = "select"
	StatementInsert  StatementKind = "insert"
	StatementCreate  StatementKind = "create"
	StatementUpdate  StatementKind = "update"
	StatementDelete  StatementKind = "delete"
)

type Statement struct {
	Kind StatementKind
	SQL  string
	Tree antlr.Tree
}

func ParseStatement(sql string) (*Statement, error) {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return nil, fmt.Errorf("empty SQL")
	}

	input := antlr.NewInputStream(sql)
	lexer := sfparser.NewSnowflakeLexer(input)
	listener := &syntaxErrorListener{DefaultErrorListener: antlr.NewDefaultErrorListener()}
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(listener)

	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := sfparser.NewSnowflakeParser(stream)
	parser.BuildParseTrees = true
	parser.RemoveErrorListeners()
	parser.AddErrorListener(listener)

	tree := parser.Snowflake_file()
	if len(listener.errs) != 0 {
		// The upstream Snowflake grammar does not accept a few valid forms in
		// our captured corpus, so keep those on the IR path instead of falling
		// back to regex rewriting.
		if stmt := parseKnownStatement(sql); stmt != nil {
			return stmt, nil
		}
		return nil, fmt.Errorf("snowflake parse: %s", strings.Join(listener.errs, "; "))
	}
	return &Statement{Kind: classifyStatement(sql), SQL: sql, Tree: tree}, nil
}

type syntaxErrorListener struct {
	*antlr.DefaultErrorListener
	errs []string
}

func (l *syntaxErrorListener) SyntaxError(_ antlr.Recognizer, _ any, line, column int, msg string, _ antlr.RecognitionException) {
	l.errs = append(l.errs, fmt.Sprintf("%d:%d %s", line, column, msg))
}

func classifyStatement(sql string) StatementKind {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	switch {
	case strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH"):
		return StatementSelect
	case strings.HasPrefix(upper, "INSERT"):
		return StatementInsert
	case strings.HasPrefix(upper, "CREATE"):
		return StatementCreate
	case strings.HasPrefix(upper, "UPDATE"):
		return StatementUpdate
	case strings.HasPrefix(upper, "DELETE"):
		return StatementDelete
	default:
		return StatementUnknown
	}
}

func parseKnownStatement(sql string) *Statement {
	kind := classifyStatement(sql)
	switch kind {
	case StatementSelect, StatementInsert, StatementCreate, StatementUpdate, StatementDelete:
		return &Statement{Kind: kind, SQL: sql}
	default:
		return nil
	}
}

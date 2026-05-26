package core

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type compiledRoleMatch struct {
	role string
	expr roleMatchExpr
}

type roleMatchExpr interface {
	eval(map[string]any) (bool, error)
}

type roleMatchBinary struct {
	op          string
	left, right roleMatchExpr
}

type roleMatchNot struct {
	child roleMatchExpr
}

type roleMatchCompare struct {
	op          string
	left, right roleMatchOperand
}

type roleMatchIsNull struct {
	operand roleMatchOperand
	not     bool
}

type roleMatchOperand struct {
	path []string
	lit  any
	ref  bool
}

func compileRoleMatches(roles []Role) ([]compiledRoleMatch, error) {
	out := make([]compiledRoleMatch, 0, len(roles))
	for _, role := range roles {
		match := strings.TrimSpace(role.Match)
		if match == "" {
			continue
		}
		expr, err := parseRoleMatch(match)
		if err != nil {
			return nil, fmt.Errorf("roles_query: role %q match: %w", role.Name, err)
		}
		out = append(out, compiledRoleMatch{role: role.Name, expr: expr})
	}
	return out, nil
}

func (e roleMatchBinary) eval(attrs map[string]any) (bool, error) {
	left, err := e.left.eval(attrs)
	if err != nil {
		return false, err
	}
	switch e.op {
	case "and":
		if !left {
			return false, nil
		}
		return e.right.eval(attrs)
	case "or":
		if left {
			return true, nil
		}
		return e.right.eval(attrs)
	default:
		return false, fmt.Errorf("unknown boolean operator %q", e.op)
	}
}

func (e roleMatchNot) eval(attrs map[string]any) (bool, error) {
	v, err := e.child.eval(attrs)
	if err != nil {
		return false, err
	}
	return !v, nil
}

func (e roleMatchCompare) eval(attrs map[string]any) (bool, error) {
	left := e.left.value(attrs)
	right := e.right.value(attrs)
	return compareRoleValues(left, right, e.op)
}

func (e roleMatchIsNull) eval(attrs map[string]any) (bool, error) {
	ok := e.operand.value(attrs) == nil
	if e.not {
		return !ok, nil
	}
	return ok, nil
}

func (o roleMatchOperand) value(attrs map[string]any) any {
	if !o.ref {
		return o.lit
	}
	var cur any = attrs
	for _, part := range o.path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[part]
		if !ok {
			return nil
		}
	}
	return cur
}

func compareRoleValues(left, right any, op string) (bool, error) {
	if op == "<>" {
		op = "!="
	}
	if left == nil || right == nil {
		switch op {
		case "=":
			return left == nil && right == nil, nil
		case "!=":
			return !(left == nil && right == nil), nil
		default:
			return false, nil
		}
	}

	if lf, lok := roleNumber(left); lok {
		if rf, rok := roleNumber(right); rok {
			return compareFloat(lf, rf, op), nil
		}
	}

	if lb, lok := left.(bool); lok {
		rb, rok := right.(bool)
		if !rok {
			return op == "!=", nil
		}
		switch op {
		case "=":
			return lb == rb, nil
		case "!=":
			return lb != rb, nil
		default:
			return false, fmt.Errorf("operator %q is not supported for booleans", op)
		}
	}

	ls, lok := left.(string)
	rs, rok := right.(string)
	if lok && rok {
		return compareString(ls, rs, op)
	}

	switch op {
	case "=":
		return fmt.Sprint(left) == fmt.Sprint(right), nil
	case "!=":
		return fmt.Sprint(left) != fmt.Sprint(right), nil
	default:
		return false, fmt.Errorf("operator %q requires comparable numbers or strings", op)
	}
}

func roleNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

func compareFloat(left, right float64, op string) bool {
	switch op {
	case "=":
		return left == right
	case "!=":
		return left != right
	case "<":
		return left < right
	case "<=":
		return left <= right
	case ">":
		return left > right
	case ">=":
		return left >= right
	default:
		return false
	}
}

func compareString(left, right, op string) (bool, error) {
	switch op {
	case "=":
		return left == right, nil
	case "!=":
		return left != right, nil
	case "<":
		return left < right, nil
	case "<=":
		return left <= right, nil
	case ">":
		return left > right, nil
	case ">=":
		return left >= right, nil
	default:
		return false, fmt.Errorf("unknown comparison operator %q", op)
	}
}

type roleMatchTokenType uint8

const (
	roleTokEOF roleMatchTokenType = iota
	roleTokIdent
	roleTokString
	roleTokNumber
	roleTokBool
	roleTokNull
	roleTokOp
	roleTokLParen
	roleTokRParen
	roleTokAnd
	roleTokOr
	roleTokNot
	roleTokIs
)

type roleMatchToken struct {
	typ roleMatchTokenType
	lit string
}

func parseRoleMatch(input string) (roleMatchExpr, error) {
	tokens, err := lexRoleMatch(input)
	if err != nil {
		return nil, err
	}
	p := roleMatchParser{tokens: tokens}
	expr, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().typ != roleTokEOF {
		return nil, fmt.Errorf("unexpected token %q", p.peek().lit)
	}
	return expr, nil
}

type roleMatchParser struct {
	tokens []roleMatchToken
	pos    int
}

func (p *roleMatchParser) parseOr() (roleMatchExpr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.accept(roleTokOr) {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = roleMatchBinary{op: "or", left: left, right: right}
	}
	return left, nil
}

func (p *roleMatchParser) parseAnd() (roleMatchExpr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.accept(roleTokAnd) {
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = roleMatchBinary{op: "and", left: left, right: right}
	}
	return left, nil
}

func (p *roleMatchParser) parseNot() (roleMatchExpr, error) {
	if p.accept(roleTokNot) {
		child, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return roleMatchNot{child: child}, nil
	}
	return p.parsePrimary()
}

func (p *roleMatchParser) parsePrimary() (roleMatchExpr, error) {
	if p.accept(roleTokLParen) {
		expr, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if !p.accept(roleTokRParen) {
			return nil, fmt.Errorf("expected ')'")
		}
		return expr, nil
	}
	return p.parsePredicate()
}

func (p *roleMatchParser) parsePredicate() (roleMatchExpr, error) {
	left, err := p.parseOperand()
	if err != nil {
		return nil, err
	}

	if p.accept(roleTokIs) {
		not := p.accept(roleTokNot)
		if !p.accept(roleTokNull) {
			return nil, fmt.Errorf("expected null after is")
		}
		return roleMatchIsNull{operand: left, not: not}, nil
	}

	op := p.next()
	if op.typ != roleTokOp {
		return nil, fmt.Errorf("expected comparison operator")
	}
	right, err := p.parseOperand()
	if err != nil {
		return nil, err
	}
	return roleMatchCompare{op: op.lit, left: left, right: right}, nil
}

func (p *roleMatchParser) parseOperand() (roleMatchOperand, error) {
	tok := p.next()
	switch tok.typ {
	case roleTokIdent:
		path, err := parseRolePath(tok.lit)
		if err != nil {
			return roleMatchOperand{}, err
		}
		return roleMatchOperand{ref: true, path: path}, nil
	case roleTokString:
		return roleMatchOperand{lit: tok.lit}, nil
	case roleTokNumber:
		n, err := strconv.ParseFloat(tok.lit, 64)
		if err != nil {
			return roleMatchOperand{}, err
		}
		return roleMatchOperand{lit: n}, nil
	case roleTokBool:
		return roleMatchOperand{lit: strings.EqualFold(tok.lit, "true")}, nil
	case roleTokNull:
		return roleMatchOperand{lit: nil}, nil
	default:
		return roleMatchOperand{}, fmt.Errorf("expected field or literal")
	}
}

func parseRolePath(lit string) ([]string, error) {
	parts := strings.Split(lit, ".")
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("invalid field path %q", lit)
		}
	}
	return parts, nil
}

func (p *roleMatchParser) peek() roleMatchToken {
	if p.pos >= len(p.tokens) {
		return roleMatchToken{typ: roleTokEOF}
	}
	return p.tokens[p.pos]
}

func (p *roleMatchParser) next() roleMatchToken {
	tok := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

func (p *roleMatchParser) accept(typ roleMatchTokenType) bool {
	if p.peek().typ != typ {
		return false
	}
	p.pos++
	return true
}

func lexRoleMatch(input string) ([]roleMatchToken, error) {
	tokens := make([]roleMatchToken, 0, 8)
	for i := 0; i < len(input); {
		r := rune(input[i])
		if unicode.IsSpace(r) {
			i++
			continue
		}

		switch input[i] {
		case '(':
			tokens = append(tokens, roleMatchToken{typ: roleTokLParen, lit: "("})
			i++
			continue
		case ')':
			tokens = append(tokens, roleMatchToken{typ: roleTokRParen, lit: ")"})
			i++
			continue
		case '\'', '"':
			lit, next, err := lexRoleString(input, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, roleMatchToken{typ: roleTokString, lit: lit})
			i = next
			continue
		case '=', '<', '>', '!':
			op, next, err := lexRoleOperator(input, i)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, roleMatchToken{typ: roleTokOp, lit: op})
			i = next
			continue
		}

		if isRoleNumberStart(input, i) {
			start := i
			i++
			for i < len(input) && isRoleNumberPart(input[i]) {
				i++
			}
			tokens = append(tokens, roleMatchToken{typ: roleTokNumber, lit: input[start:i]})
			continue
		}

		if isRoleIdentStart(input[i]) {
			start := i
			i++
			for i < len(input) && isRoleIdentPart(input[i]) {
				i++
			}
			lit := input[start:i]
			tokens = append(tokens, roleKeywordToken(lit))
			continue
		}

		return nil, fmt.Errorf("unexpected character %q", input[i])
	}
	tokens = append(tokens, roleMatchToken{typ: roleTokEOF})
	return tokens, nil
}

func roleKeywordToken(lit string) roleMatchToken {
	switch strings.ToLower(lit) {
	case "and":
		return roleMatchToken{typ: roleTokAnd, lit: lit}
	case "or":
		return roleMatchToken{typ: roleTokOr, lit: lit}
	case "not":
		return roleMatchToken{typ: roleTokNot, lit: lit}
	case "is":
		return roleMatchToken{typ: roleTokIs, lit: lit}
	case "null":
		return roleMatchToken{typ: roleTokNull, lit: lit}
	case "true", "false":
		return roleMatchToken{typ: roleTokBool, lit: lit}
	default:
		return roleMatchToken{typ: roleTokIdent, lit: lit}
	}
}

func lexRoleString(input string, start int) (string, int, error) {
	quote := input[start]
	var b strings.Builder
	for i := start + 1; i < len(input); i++ {
		if input[i] == quote {
			if i+1 < len(input) && input[i+1] == quote {
				b.WriteByte(quote)
				i++
				continue
			}
			return b.String(), i + 1, nil
		}
		if input[i] == '\\' && i+1 < len(input) {
			i++
		}
		b.WriteByte(input[i])
	}
	return "", 0, fmt.Errorf("unterminated string literal")
}

func lexRoleOperator(input string, start int) (string, int, error) {
	if start+1 < len(input) {
		op := input[start : start+2]
		switch op {
		case "!=", "<>", "<=", ">=":
			return op, start + 2, nil
		}
	}
	switch input[start] {
	case '=', '<', '>':
		return input[start : start+1], start + 1, nil
	default:
		return "", 0, fmt.Errorf("unknown operator starting with %q", input[start])
	}
}

func isRoleNumberStart(input string, i int) bool {
	if input[i] >= '0' && input[i] <= '9' {
		return true
	}
	return input[i] == '-' && i+1 < len(input) && input[i+1] >= '0' && input[i+1] <= '9'
}

func isRoleNumberPart(b byte) bool {
	return (b >= '0' && b <= '9') || b == '.' || b == 'e' || b == 'E' || b == '+' || b == '-'
}

func isRoleIdentStart(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == '_'
}

func isRoleIdentPart(b byte) bool {
	return isRoleIdentStart(b) || (b >= '0' && b <= '9') || b == '.'
}

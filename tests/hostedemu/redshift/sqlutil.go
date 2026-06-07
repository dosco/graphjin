package redshift

import "strings"

func StripSQLComments(sql string) string {
	var b strings.Builder
	inSingle := false
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		if ch == '\'' {
			inSingle = !inSingle
			b.WriteByte(ch)
			continue
		}
		if !inSingle && ch == '-' && i+1 < len(sql) && sql[i+1] == '-' {
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			if i < len(sql) {
				b.WriteByte(sql[i])
			}
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func SplitStatements(sql string) []string {
	var parts []string
	var b strings.Builder
	inSingle := false
	inDouble := false
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case ';':
			if !inSingle && !inDouble {
				parts = append(parts, b.String())
				b.Reset()
				continue
			}
		}
		b.WriteByte(ch)
	}
	if strings.TrimSpace(b.String()) != "" {
		parts = append(parts, b.String())
	}
	return parts
}

func SplitTopLevel(s string, sep byte) []string {
	var parts []string
	var b strings.Builder
	depth := 0
	inSingle := false
	inDouble := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '(':
			if !inSingle && !inDouble {
				depth++
			}
		case ')':
			if !inSingle && !inDouble && depth > 0 {
				depth--
			}
		}
		if ch == sep && depth == 0 && !inSingle && !inDouble {
			parts = append(parts, b.String())
			b.Reset()
			continue
		}
		b.WriteByte(ch)
	}
	parts = append(parts, b.String())
	return parts
}

func FindMatchingParen(s string, open int) int {
	depth := 0
	inSingle := false
	inDouble := false
	for i := open; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		}
		if inSingle || inDouble {
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func readIdentToken(s string) (string, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if s[0] == '"' {
		end := 1
		for end < len(s) {
			if s[end] == '"' {
				return TrimIdent(s[:end+1]), strings.TrimSpace(s[end+1:])
			}
			end++
		}
		return TrimIdent(s), ""
	}
	depth := 0
	end := 0
	for end < len(s) {
		ch := s[end]
		if ch == '(' {
			depth++
		} else if ch == ')' && depth > 0 {
			depth--
		}
		if depth == 0 && isDDLSpace(ch) {
			break
		}
		end++
	}
	return TrimIdent(s[:end]), strings.TrimSpace(s[end:])
}

func indexTopLevelKeyword(sql, keyword string) int {
	upper := strings.ToUpper(sql)
	needle := strings.ToUpper(keyword)
	depth := 0
	inSingle := false
	inDouble := false
	for i := 0; i <= len(sql)-len(needle); i++ {
		ch := sql[i]
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '(':
			if !inSingle && !inDouble {
				depth++
			}
		case ')':
			if !inSingle && !inDouble && depth > 0 {
				depth--
			}
		}
		if inSingle || inDouble || depth != 0 {
			continue
		}
		if strings.HasPrefix(upper[i:], needle) && keywordBoundary(sql, i-1) && keywordBoundary(sql, i+len(needle)) {
			return i
		}
	}
	return -1
}

func keywordBoundary(sql string, idx int) bool {
	if idx < 0 || idx >= len(sql) {
		return true
	}
	ch := sql[idx]
	return !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_')
}

func replaceFoldAll(sql, from, to string) string {
	start := 0
	for {
		idx := indexFoldFrom(sql, from, start)
		if idx < 0 {
			return sql
		}
		sql = sql[:idx] + to + sql[idx+len(from):]
		start = idx + len(to)
	}
}

func replaceFunction(sql, name string, fn func(string) string) string {
	start := 0
	for {
		idx := indexFoldFrom(sql, name, start)
		if idx < 0 {
			return sql
		}
		open := idx + len(name)
		for open < len(sql) && isDDLSpace(sql[open]) {
			open++
		}
		if open >= len(sql) || sql[open] != '(' {
			start = idx + len(name)
			continue
		}
		close := FindMatchingParen(sql, open)
		if close < 0 {
			return sql
		}
		repl := fn(sql[open+1 : close])
		sql = sql[:idx] + repl + sql[close+1:]
		start = idx + len(repl)
	}
}

func indexFoldFrom(s, substr string, start int) int {
	if start < 0 {
		start = 0
	}
	upper := strings.ToUpper(s)
	needle := strings.ToUpper(substr)
	idx := strings.Index(upper[start:], needle)
	if idx < 0 {
		return -1
	}
	return start + idx
}

func NormalizeIdentifier(s string) string {
	return strings.ToLower(TrimIdent(s))
}

func NormIdent(s string) string {
	return strings.ToLower(TrimIdent(s))
}

func TrimQualifiedIdent(s string) string {
	parts := splitQualified(s)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ".")
}

func TrimIdent(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`\"[]")
	return strings.Trim(s, "`\"[]")
}

func isDDLSpace(ch byte) bool {
	return ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t'
}

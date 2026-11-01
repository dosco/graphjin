package agent

import (
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

// Ax normalizes actor code with replace("r\n", "\n") because its IR lexer
// drops the backslash of "\r", so productnumber becomes productnumbe. A CR
// after the "r" leaves nothing to match; JavaScript reads CR LF as a line break.
type lineEndGuard struct {
	inString, escaped, afterR, pending bool
}

// text guards one piece of a response: JSON at \n escapes inside strings,
// other text at raw line breaks.
func (g *lineEndGuard) text(s string, json bool) string {
	if !json {
		return strings.ReplaceAll(s, "r\n", "r\r\n")
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if g.pending {
			g.pending = false
			if ch == 'n' {
				b.WriteString(`\r\n`)
				g.afterR = false
				continue
			}
			b.WriteByte('\\')
			g.escaped = true
		}
		switch {
		case !g.inString:
			g.inString = ch == '"'
		case g.escaped:
			g.escaped = false
			g.afterR = false
		case ch == '\\' && g.afterR:
			g.pending = true
			continue
		case ch == '\\':
			g.escaped = true
		case ch == '"':
			g.inString = false
			g.afterR = false
		default:
			g.afterR = ch == 'r'
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func (g *lineEndGuard) flush() string {
	if g.pending {
		g.pending = false
		return `\`
	}
	return ""
}

func jsonText(s string) bool {
	trimmed := strings.TrimSpace(s)
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}

// guardResponses keeps one guard per result index across a response's chunks.
func guardResponses(values []ax.Value) []ax.Value {
	guards := map[int]*lineEndGuard{}
	modes := map[int]bool{}
	last := map[int]map[string]ax.Value{}
	for _, value := range values {
		chunk, _ := value.(map[string]ax.Value)
		for index, item := range items(chunk["results"]) {
			result, _ := item.(map[string]ax.Value)
			if result == nil {
				continue
			}
			if text, ok := result["content"].(string); ok && text != "" {
				guard := guards[index]
				if guard == nil {
					guard = &lineEndGuard{}
					guards[index] = guard
					modes[index] = jsonText(text)
				}
				result["content"] = guard.text(text, modes[index])
				last[index] = result
			}
			for _, call := range items(result["function_calls"]) {
				guardFunctionCall(call)
			}
		}
	}
	for index, guard := range guards {
		if rest := guard.flush(); rest != "" {
			last[index]["content"] = last[index]["content"].(string) + rest
		}
	}
	return values
}

func guardFunctionCall(call ax.Value) {
	entry, _ := call.(map[string]ax.Value)
	function, _ := entry["function"].(map[string]ax.Value)
	if function == nil {
		return
	}
	switch params := function["params"].(type) {
	case string:
		guard := &lineEndGuard{}
		function["params"] = guard.text(params, jsonText(params)) + guard.flush()
	case map[string]ax.Value:
		guardStrings(params)
	}
}

func guardStrings(value ax.Value) ax.Value {
	switch v := value.(type) {
	case string:
		return strings.ReplaceAll(v, "r\n", "r\r\n")
	case map[string]ax.Value:
		for key, item := range v {
			v[key] = guardStrings(item)
		}
	case []ax.Value:
		for i, item := range v {
			v[i] = guardStrings(item)
		}
	case *ax.AxArray:
		for i, item := range v.Items {
			v.Items[i] = guardStrings(item)
		}
	}
	return value
}

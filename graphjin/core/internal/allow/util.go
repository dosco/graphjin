package allow

import (
	"strings"
)

func QueryName(b string) string {
	state, s := 0, 0
	bl := len(b)

	for i := 0; i < bl; i++ {
		switch {
		case state == 2 && !isValidNameChar(b[i]):
			return b[s:i]
		case state == 1 && b[i] == '{':
			return ""
		case state == 1 && isValidNameChar(b[i]):
			s = i
			state = 2
		case i != 0 && b[i] == ' ' && (b[i-1] == 'n' || b[i-1] == 'y' || b[i-1] == 't'):
			state = 1
		}
	}

	return ""
}

func isValidNameChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func isGraphQL(s string) bool {
	return strings.HasPrefix(s, "query") ||
		strings.HasPrefix(s, "mutation") ||
		strings.HasPrefix(s, "subscription")
}

package jsn

import (
	"fmt"
	"strconv"
	"strings"
	"unsafe"
)

// Validate function validates JSON
func Validate(s string) error {
	s = skipWS(s)

	tail, err := validateValue(s)
	if err != nil {
		return fmt.Errorf("cannot parse JSON: %s; unparsed tail: %q", err, startEndString(tail))
	}
	tail = skipWS(tail)
	if len(tail) > 0 {
		return fmt.Errorf("unexpected tail: %q", startEndString(tail))
	}
	return nil
}

// ValidateBytes validates JSON b.
func ValidateBytes(b []byte) error {
	return Validate(b2s(b))
}

func validateValue(s string) (string, error) {
	if len(s) == 0 {
		return s, fmt.Errorf("cannot parse empty string")
	}

	if s[0] == '{' {
		tail, err := validateObject(s[1:])
		if err != nil {
			return tail, fmt.Errorf("cannot parse object: %s", err)
		}
		return tail, nil
	}
	if s[0] == '[' {
		tail, err := validateArray(s[1:])
		if err != nil {
			return tail, fmt.Errorf("cannot parse array: %s", err)
		}
		return tail, nil
	}
	if s[0] == '"' {
		sv, tail, err := validateString(s[1:])
		if err != nil {
			return tail, fmt.Errorf("cannot parse string: %s", err)
		}
		// Scan the string for control chars.
		for i := 0; i < len(sv); i++ {
			if sv[i] < 0x20 {
				return tail, fmt.Errorf("string cannot contain control char 0x%02X", sv[i])
			}
		}
		return tail, nil
	}
	if s[0] == 't' {
		if len(s) < len("true") || s[:len("true")] != "true" {
			return s, fmt.Errorf("unexpected value found: %q", s)
		}
		return s[len("true"):], nil
	}
	if s[0] == 'f' {
		if len(s) < len("false") || s[:len("false")] != "false" {
			return s, fmt.Errorf("unexpected value found: %q", s)
		}
		return s[len("false"):], nil
	}
	if s[0] == 'n' {
		if len(s) < len("null") || s[:len("null")] != "null" {
			return s, fmt.Errorf("unexpected value found: %q", s)
		}
		return s[len("null"):], nil
	}

	tail, err := validateNumber(s)
	if err != nil {
		return tail, fmt.Errorf("cannot parse number: %s", err)
	}
	return tail, nil
}

func validateArray(s string) (string, error) {
	s = skipWS(s)
	if len(s) == 0 {
		return s, fmt.Errorf("missing ']'")
	}
	if s[0] == ']' {
		return s[1:], nil
	}

	for {
		var err error

		s = skipWS(s)
		s, err = validateValue(s)
		if err != nil {
			return s, fmt.Errorf("cannot parse array value: %s", err)
		}

		s = skipWS(s)
		if len(s) == 0 {
			return s, fmt.Errorf("unexpected end of array")
		}
		if s[0] == ',' {
			s = s[1:]
			continue
		}
		if s[0] == ']' {
			s = s[1:]
			return s, nil
		}
		return s, fmt.Errorf("missing ',' after array value")
	}
}

func validateObject(s string) (string, error) {
	s = skipWS(s)
	if len(s) == 0 {
		return s, fmt.Errorf("missing '}'")
	}
	if s[0] == '}' {
		return s[1:], nil
	}

	for {
		var err error

		// Parse key.
		s = skipWS(s)
		if len(s) == 0 || s[0] != '"' {
			return s, fmt.Errorf(`cannot find opening '"" for object key`)
		}

		var key string
		key, s, err = validateKey(s[1:])
		if err != nil {
			return s, fmt.Errorf("cannot parse object key: %s", err)
		}
		// Scan the key for control chars.
		for i := 0; i < len(key); i++ {
			if key[i] < 0x20 {
				return s, fmt.Errorf("object key cannot contain control char 0x%02X", key[i])
			}
		}
		s = skipWS(s)
		if len(s) == 0 || s[0] != ':' {
			return s, fmt.Errorf("missing ':' after object key")
		}
		s = s[1:]

		// Parse value
		s = skipWS(s)
		s, err = validateValue(s)
		if err != nil {
			return s, fmt.Errorf("cannot parse object value: %s", err)
		}
		s = skipWS(s)
		if len(s) == 0 {
			return s, fmt.Errorf("unexpected end of object")
		}
		if s[0] == ',' {
			s = s[1:]
			continue
		}
		if s[0] == '}' {
			return s[1:], nil
		}
		return s, fmt.Errorf("missing ',' after object value")
	}
}

// validateKey is similar to validateString, but is optimized
// for typical object keys, which are quite small and have no escape sequences.
func validateKey(s string) (string, string, error) {
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			// Fast path - the key doesn't contain escape sequences.
			return s[:i], s[i+1:], nil
		}
		if s[i] == '\\' {
			// Slow path - the key contains escape sequences.
			return validateString(s)
		}
	}
	return "", s, fmt.Errorf(`missing closing '"'`)
}

func validateString(s string) (string, string, error) {
	// Try fast path - a string without escape sequences.
	if n := strings.IndexByte(s, '"'); n >= 0 && strings.IndexByte(s[:n], '\\') < 0 {
		return s[:n], s[n+1:], nil
	}

	// Slow path - escape sequences are present.
	rs, tail, err := parseRawString(s)
	if err != nil {
		return rs, tail, err
	}
	for {
		n := strings.IndexByte(rs, '\\')
		if n < 0 {
			return rs, tail, nil
		}
		n++
		if n >= len(rs) {
			return rs, tail, fmt.Errorf("BUG: parseRawString returned invalid string with trailing backslash: %q", rs)
		}
		ch := rs[n]
		rs = rs[n+1:]
		switch ch {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			// Valid escape sequences - see http://json.org/
			break
		case 'u':
			if len(rs) < 4 {
				return rs, tail, fmt.Errorf(`too short escape sequence: \u%s`, rs)
			}
			xs := rs[:4]
			_, err := strconv.ParseUint(xs, 16, 16)
			if err != nil {
				return rs, tail, fmt.Errorf(`invalid escape sequence \u%s: %s`, xs, err)
			}
			rs = rs[4:]
		default:
			return rs, tail, fmt.Errorf(`unknown escape sequence \%c`, ch)
		}
	}
}

func validateNumber(s string) (string, error) {
	if len(s) == 0 {
		return s, fmt.Errorf("zero-length number")
	}
	if s[0] == '-' {
		s = s[1:]
		if len(s) == 0 {
			return s, fmt.Errorf("missing number after minus")
		}
	}
	i := 0
	for i < len(s) {
		if s[i] < '0' || s[i] > '9' {
			break
		}
		i++
	}
	if i <= 0 {
		return s, fmt.Errorf("expecting 0..9 digit, got %c", s[0])
	}
	if s[0] == '0' && i != 1 {
		return s, fmt.Errorf("unexpected number starting from 0")
	}
	if i >= len(s) {
		return "", nil
	}
	if s[i] == '.' {
		// Validate fractional part
		s = s[i+1:]
		if len(s) == 0 {
			return s, fmt.Errorf("missing fractional part")
		}
		i = 0
		for i < len(s) {
			if s[i] < '0' || s[i] > '9' {
				break
			}
			i++
		}
		if i == 0 {
			return s, fmt.Errorf("expecting 0..9 digit in fractional part, got %c", s[0])
		}
		if i >= len(s) {
			return "", nil
		}
	}
	if s[i] == 'e' || s[i] == 'E' {
		// Validate exponent part
		s = s[i+1:]
		if len(s) == 0 {
			return s, fmt.Errorf("missing exponent part")
		}
		if s[0] == '-' || s[0] == '+' {
			s = s[1:]
			if len(s) == 0 {
				return s, fmt.Errorf("missing exponent part")
			}
		}
		i = 0
		for i < len(s) {
			if s[i] < '0' || s[i] > '9' {
				break
			}
			i++
		}
		if i == 0 {
			return s, fmt.Errorf("expecting 0..9 digit in exponent part, got %c", s[0])
		}
		if i >= len(s) {
			return "", nil
		}
	}
	return s[i:], nil
}

func skipWS(s string) string {
	if len(s) == 0 || s[0] > 0x20 {
		// Fast path.
		return s
	}
	return skipWSSlow(s)
}

func skipWSSlow(s string) string {
	if len(s) == 0 || s[0] != 0x20 && s[0] != 0x0A && s[0] != 0x09 && s[0] != 0x0D {
		return s
	}
	for i := 1; i < len(s); i++ {
		if s[i] != 0x20 && s[i] != 0x0A && s[i] != 0x09 && s[i] != 0x0D {
			return s[i:]
		}
	}
	return ""
}

func b2s(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

const maxStartEndStringLen = 80

func startEndString(s string) string {
	if len(s) <= maxStartEndStringLen {
		return s
	}
	start := s[:40]
	end := s[len(s)-40:]
	return start + "..." + end
}

func parseRawString(s string) (string, string, error) {
	n := strings.IndexByte(s, '"')
	if n < 0 {
		return s, "", fmt.Errorf(`missing closing '"'`)
	}
	if n == 0 || s[n-1] != '\\' {
		// Fast path. No escaped ".
		return s[:n], s[n+1:], nil
	}

	// Slow path - possible escaped " found.
	ss := s
	for {
		i := n - 1
		for i > 0 && s[i-1] == '\\' {
			i--
		}
		if uint(n-i)%2 == 0 {
			return ss[:len(ss)-len(s)+n], s[n+1:], nil
		}
		s = s[n+1:]

		n = strings.IndexByte(s, '"')
		if n < 0 {
			return ss, "", fmt.Errorf(`missing closing '"'`)
		}
		if n == 0 || s[n-1] != '\\' {
			return ss[:len(ss)-len(s)+n], s[n+1:], nil
		}
	}
}

package core

import (
	"strings"
)

func prettify(query, dbInfo string) string {
	var prettified strings.Builder
	// estimated size
	prettified.Grow(len(query) + 200)

	newline := true

	// dbInfo can be complex (e.g., version strings), so simplify check
	// "postgres", "mysql", "mariadb", "sqlite", "oracle"
	isMySQL := strings.Contains(strings.ToLower(dbInfo), "mysql") || strings.Contains(strings.ToLower(dbInfo), "mariadb")

	// State variables
	inString := false     // '...'
	inIdentifier := false // "..." or `...`
	quoteChar := byte(0)
	lastChar := byte(0)

	// Keywords that start a new clause (dedent then indent)
	// We check these case-insensitively
	keywords := map[string]bool{
		"SELECT":      true,
		"FROM":        true,
		"WHERE":       true,
		"AND":         true,
		"OR":          true,
		"HAVING":      true,
		"GROUP BY":    true, 
		"ORDER BY":    true,
		"LIMIT":       true,
		"OFFSET":      true,
		"UNION":       true,
		"WITH":        true,
		"VALUES":      true,
		"SET":         true,
		"UPDATE":      true,
		"INSERT INTO": true,
		"RETURNING":   true,
		"ON CONFLICT": true,
		"JOIN":        true,
		"LEFT JOIN":   true,
		"RIGHT JOIN":  true,
		"INNER JOIN":  true,
		"OUTER JOIN":  true,
		"CROSS JOIN":  true,
	}

	n := len(query)
	for i := 0; i < n; i++ {
		char := query[i]

		// Handle quoting
		if inString {
			prettified.WriteByte(char)
			if char == quoteChar {
                // Check for escaped quote (e.g. 'O''Reilly' or 'foo\'bar')
                // SQL standard uses double single-quote '' for escaping in strings.
                // MySQL also supports backslash escaping.
				if i+1 < n && query[i+1] == quoteChar {
					prettified.WriteByte(quoteChar)
					i++ // skip next
				} else if isMySQL && lastChar == '\\' {
                   // Escaped by backslash, continue (naive check, doesn't handle double backslash)
                } else {
                    inString = false
                }
			}
            lastChar = char
			newline = false
			continue
		}

		if inIdentifier {
			prettified.WriteByte(char)
			if char == quoteChar {
                // Determine if escaped.
                // Identifiers usually double-quote for escape: "" or ``
                if i+1 < n && query[i+1] == quoteChar {
					prettified.WriteByte(quoteChar)
					i++
                } else {
				    inIdentifier = false
                }
			}
            lastChar = char
			newline = false
			continue
		}

		// Check for string start
		if char == '\'' {
			inString = true
			quoteChar = '\''
			prettified.WriteByte(char)
            lastChar = char
			newline = false
			continue
		}

		// Check for identifier start
		if char == '"' {
			inIdentifier = true
			quoteChar = '"'
			prettified.WriteByte(char)
            lastChar = char
			newline = false
			continue
		}
		if isMySQL && char == '`' {
			inIdentifier = true
			quoteChar = '`'
			prettified.WriteByte(char)
            lastChar = char
			newline = false
			continue
		}

		// Normal code processing
		// Check for keywords
        // Check for multi-word keywords first
        if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') {
            // Find end of current word
            j := i
            for j < n {
                c := query[j]
                if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' {
                    j++
                } else {
                    break
                }
            }
            
            // Check strictly for single word or lookahead for double word
            word := query[i:j]
            upperWord := strings.ToUpper(word)
            
            // Check double word
            if j < n && query[j] == ' ' {
                // Potential double word
                k := j + 1
                for k < n && query[k] == ' ' { k++ } // skip spaces
                // find next word
                l := k
                for l < n {
                    c := query[l]
                    if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' {
                        l++
                    } else {
                        break
                    }
                }
                if l > k {
                    secondWord := query[k:l]
                    combined := upperWord + " " + strings.ToUpper(secondWord)
                    if keywords[combined] {
                        prettified.WriteByte('\n')
                        prettified.WriteString(combined)
                        prettified.WriteByte(' ')
                        i = l - 1
                        newline = false
                        lastChar = ' '
                        continue
                    }
                }
            }
            
            if keywords[upperWord] {
               prettified.WriteByte('\n')
               prettified.WriteString(upperWord)
               prettified.WriteByte(' ') // force space?
               i = j - 1
               newline = false
               lastChar = ' '
               continue
            }
        }

        // Just write the char if no keyword/special handling
        // maybe collapse multiple spaces?
        if char == ' ' || char == '\t' || char == '\n' || char == '\r' {
            if !newline && lastChar != ' ' && lastChar != '\n' {
                 prettified.WriteByte(' ')
            }
            lastChar = ' '
            continue
        } else {
		    prettified.WriteByte(char)
            newline = false
        }
        lastChar = char
	}
    
    // Quick cleanup
    res := strings.TrimSpace(prettified.String())
    return res
}

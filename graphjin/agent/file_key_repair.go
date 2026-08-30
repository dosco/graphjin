package agent

import (
	"context"
	"fmt"
	"strings"
)

// maxListedFileKeys bounds the key list a miss reports. A file source can hold
// far more objects than a model can read, and the point of the list is to name
// the vocabulary, not to enumerate the bucket.
const maxListedFileKeys = 24

// fileRootKeyRead is one root selection reading a file source by key: the
// literal that was asked for, its span in the original query, and whether the
// selection already asked for the object's bytes.
type fileRootKeyRead struct {
	root       string
	key        string
	keyStart   int
	keyEnd     int
	argOpen    int
	argClose   int
	inlineData bool
	wantsBody  bool
}

// fileRootKeyReads finds the root fields of a read that fetch one object by
// key. Only depth-1 roots with a literal key argument qualify; a key supplied
// through a variable is left alone, since the repair would have to rewrite the
// variables rather than the query.
func fileRootKeyReads(query string, args map[string]any) []fileRootKeyRead {
	clean := graphQLStructure(query)
	lower := strings.ToLower(clean)
	var out []fileRootKeyRead
	for _, root := range QueryRootFields(query) {
		target := strings.ToLower(strings.TrimSpace(root))
		if target == "" {
			continue
		}
		for offset := 0; offset < len(lower); {
			relative := strings.Index(lower[offset:], target)
			if relative < 0 {
				break
			}
			start := offset + relative
			end := start + len(target)
			offset = end
			if (start > 0 && isGraphQLNameContinue(lower[start-1])) || (end < len(lower) && isGraphQLNameContinue(lower[end])) {
				continue
			}
			open := skipGraphQLSpace(clean, end)
			if open >= len(clean) || clean[open] != '(' {
				continue
			}
			close := matchingGraphQLDelimiter(clean, open, '(', ')')
			if close < 0 {
				continue
			}
			read := fileRootKeyRead{root: root, argOpen: open, argClose: close}
			if span, ok := graphQLStringFieldSpans(query[open+1 : close])["key"]; ok {
				read.key = span.value
				read.keyStart, read.keyEnd = open+1+span.start, open+1+span.end
			} else if span, ok := whereKeyEqualitySpan(query, clean, open+1, close); ok {
				// The same miss arrives as a where filter — half the recorded
				// SLA queries wrote where: { key: { eq: ... } } — and the
				// bridge answers both forms with the same silent empty list.
				read.key = span.value
				read.keyStart, read.keyEnd = span.start, span.end
			}
			if strings.TrimSpace(read.key) == "" {
				continue
			}
			for _, name := range topLevelGraphQLArgumentNames(clean[open+1 : close]) {
				if strings.EqualFold(strings.TrimSpace(name), "inline_data") {
					read.inlineData = true
				}
			}
			// The selection body decides whether the repair needs inline_data:
			// it is what turns a listing row into the object's contents.
			if body := selectionBodyAfter(clean, close); body != "" {
				fields := strings.ToLower(body)
				read.wantsBody = strings.Contains(fields, "data") || strings.Contains(fields, "text")
			}
			out = append(out, read)
			break
		}
	}
	return out
}

// whereKeyEqualitySpan finds the literal of a where: { key: { eq: "..." } }
// filter directly under a root's arguments, returning its span in the original
// query so a repair can substitute the real key in place. Only the direct form
// is recognized; a key buried under and/or composition stays untouched.
func whereKeyEqualitySpan(query, clean string, start, end int) (stringLiteralSpan, bool) {
	whereOpen, whereClose, ok := namedObjectSpan(clean, start, end, "where")
	if !ok {
		return stringLiteralSpan{}, false
	}
	keyOpen, keyClose, ok := namedObjectSpan(clean, whereOpen+1, whereClose, "key")
	if !ok {
		return stringLiteralSpan{}, false
	}
	span, ok := graphQLStringFieldSpans(query[keyOpen+1 : keyClose])["eq"]
	if !ok {
		return stringLiteralSpan{}, false
	}
	return stringLiteralSpan{value: span.value, start: keyOpen + 1 + span.start, end: keyOpen + 1 + span.end}, true
}

// namedObjectSpan locates `name: { ... }` inside [start,end) of the blanked
// query, returning the braces' offsets.
func namedObjectSpan(clean string, start, end int, name string) (int, int, bool) {
	lower := strings.ToLower(clean[start:end])
	for offset := 0; offset < len(lower); {
		relative := strings.Index(lower[offset:], name)
		if relative < 0 {
			return 0, 0, false
		}
		fieldStart := start + offset + relative
		fieldEnd := fieldStart + len(name)
		offset += relative + len(name)
		if (fieldStart > start && isGraphQLNameContinue(clean[fieldStart-1])) || (fieldEnd < end && isGraphQLNameContinue(clean[fieldEnd])) {
			continue
		}
		colon := skipGraphQLSpace(clean, fieldEnd)
		if colon >= end || clean[colon] != ':' {
			continue
		}
		open := skipGraphQLSpace(clean, colon+1)
		if open >= end || clean[open] != '{' {
			continue
		}
		close := matchingGraphQLDelimiter(clean, open, '{', '}')
		if close <= open || close > end {
			continue
		}
		return open, close, true
	}
	return 0, 0, false
}

// selectionBodyAfter returns the `{ ... }` selection that follows a root's
// argument list, blanked of string contents by the caller's clean copy.
func selectionBodyAfter(clean string, argClose int) string {
	open := skipGraphQLSpace(clean, argClose+1)
	if open >= len(clean) || clean[open] != '{' {
		return ""
	}
	end := matchingGraphQLDelimiter(clean, open, '{', '}')
	if end <= open {
		return ""
	}
	return clean[open+1 : end]
}

// rootReturnedNothing reports whether a root came back with no rows. A file
// key miss is not an engine error — the bridge returns an empty list, exactly
// as a row-level filter that matched nothing would.
func rootReturnedNothing(out any, root string) bool {
	data, ok := normalizeValue(mapValue(normalizeValue(out))["data"]).(map[string]any)
	if !ok {
		return false
	}
	value, present := data[root]
	if !present {
		return false
	}
	switch typed := normalizeValue(value).(type) {
	case nil:
		return true
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}

// fileSourceKeys lists the keys a file source actually holds, by running the
// listing read the source's own catalog card documents. The call is local — a
// filesystem walk behind the same engine — so it costs no provider traffic.
func (r *protocolRuntime) fileSourceKeys(ctx context.Context, root string) []string {
	if r == nil || r.base == nil {
		return nil
	}
	if cached, ok := r.state.fileSourceKeys[root]; ok {
		return cached
	}
	var keys []string
	out, err := r.base.ExecuteGraphQL(ctx, map[string]any{
		"query": fmt.Sprintf("query { %s(prefix: \"\", limit: %d) { key } }", root, maxListedFileKeys+1),
	})
	if err == nil && !executionFailed(out) {
		if data, ok := normalizeValue(mapValue(normalizeValue(out))["data"]).(map[string]any); ok {
			rows, _ := normalizeValue(data[root]).([]any)
			for _, row := range rows {
				if key := strings.TrimSpace(stringFromMap(mapValue(normalizeValue(row)), "key")); key != "" {
					keys = append(keys, key)
				}
			}
		}
	}
	if r.state.fileSourceKeys == nil {
		r.state.fileSourceKeys = map[string][]string{}
	}
	r.state.fileSourceKeys[root] = keys
	return keys
}

// fileKeyMissRepair turns a read that asked a file source for a key it does not
// have into the corrected read.
//
// The bridge answers a missing key with an empty list and no error, which is
// right for a row filter and wrong for an identity lookup: the caller cannot
// tell "no such object" from "the object is empty". Every SLA episode in run r4
// guessed docs/support-sla.md — a path that exists nowhere in this repo — got
// silence, and answered from the model's own knowledge instead. One of them
// scored a full pass with a fabricated figure.
//
// The correction rides the same channel as the value repair, for the same
// reason: executor code is straight-line JavaScript, so an exception is the one
// thing guaranteed to be read, and it carries the key that exists rather than
// only the news that this one does not. Nothing is discarded — the read
// returned no usable rows for this root, and the repaired query keeps every
// other root the original asked for.
func (r *protocolRuntime) fileKeyMissRepair(ctx context.Context, query string, args map[string]any, out any) (string, string, bool) {
	for _, read := range fileRootKeyReads(query, args) {
		if !rootReturnedNothing(out, read.root) {
			continue
		}
		keys := r.fileSourceKeys(ctx, read.root)
		if len(keys) == 0 {
			continue
		}
		known := map[string]bool{}
		for _, key := range keys {
			known[key] = true
		}
		if known[read.key] {
			// The key exists and the read still came back empty — a where
			// clause or a paging offset excluded it. Not a vocabulary problem,
			// and not this repair's business.
			continue
		}
		repaired, described := "", ""
		if candidate, ok := nearestFileKey(read.key, keys); ok {
			repaired = repairFileKeyRead(query, read, candidate)
			described = fmt.Sprintf("%s has no object with key %q; the key it does have closest to that is %q", read.root, read.key, candidate)
		} else {
			described = fmt.Sprintf("%s has no object with key %q; the keys it holds are: %s",
				read.root, read.key, strings.Join(boundedKeyList(keys), ", "))
		}
		return repaired, described, true
	}
	return "", "", false
}

// repairFileKeyRead substitutes the real key and, when the selection asks for
// the object's contents, the inline_data argument that returns them. The card
// documents both together; a repair that fixed only the key would hand back a
// listing row and no policy text.
func repairFileKeyRead(query string, read fileRootKeyRead, key string) string {
	if read.keyStart <= 0 || read.keyEnd > len(query) || read.keyStart >= read.keyEnd {
		return ""
	}
	repaired := query[:read.keyStart] + key + query[read.keyEnd:]
	shift := len(key) - (read.keyEnd - read.keyStart)
	if read.wantsBody && !read.inlineData {
		insert := read.argClose + shift
		if insert > 0 && insert <= len(repaired) {
			repaired = repaired[:insert] + ", inline_data: true" + repaired[insert:]
		}
	}
	return repaired
}

// nearestFileKey picks the one key a miss most likely meant. Keys are paths, so
// the basename carries the intent and the directory prefix is the usual
// invention — docs/support-sla.md for support-sla-policy.md. A single candidate
// wins outright; a tie names nothing and the caller lists instead.
func nearestFileKey(want string, keys []string) (string, bool) {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return "", false
	}
	wantBase := want
	if index := strings.LastIndex(wantBase, "/"); index >= 0 {
		wantBase = wantBase[index+1:]
	}
	var matches []string
	for _, key := range keys {
		base := strings.ToLower(key)
		if index := strings.LastIndex(base, "/"); index >= 0 {
			base = base[index+1:]
		}
		if base == wantBase || strings.HasPrefix(base, wantBase) || strings.HasPrefix(wantBase, base) {
			matches = append(matches, key)
			continue
		}
		if stem := strings.TrimSuffix(wantBase, pathExtension(wantBase)); stem != "" {
			if other := strings.TrimSuffix(base, pathExtension(base)); other != "" {
				if strings.Contains(other, stem) || strings.Contains(stem, other) {
					matches = append(matches, key)
				}
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

func pathExtension(name string) string {
	if index := strings.LastIndex(name, "."); index > 0 {
		return name[index:]
	}
	return ""
}

func boundedKeyList(keys []string) []string {
	if len(keys) <= maxListedFileKeys {
		return keys
	}
	out := append([]string(nil), keys[:maxListedFileKeys]...)
	return append(out, fmt.Sprintf("(%d more)", len(keys)-maxListedFileKeys))
}

// firstFileRootName names the root a miss was reported for, so the listing
// instruction is written against the caller's own source rather than a
// placeholder.
func firstFileRootName(query string, args map[string]any) string {
	for _, read := range fileRootKeyReads(query, args) {
		return read.root
	}
	return "the source"
}

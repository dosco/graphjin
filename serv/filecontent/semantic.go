package filecontent

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"path"
	"sort"
	"strings"
	"unicode"
)

// Embedder turns texts into vectors, one per text, in order.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Space is the embedding space passages are stored in. Vectors made under a
// different Fingerprint are never compared with this space's queries.
type Space struct {
	// Fingerprint names the model, endpoint, dimensions and prefixes.
	Fingerprint string

	// DocumentPrefix is prepended to each passage. A "title: none" in it is
	// replaced with "title: <file name>", so a prefix like
	// "title: none | text: " gives the model the file name as the title.
	DocumentPrefix string

	Embedder Embedder

	// MaxRequestChars caps the total text of one embedding request, so a
	// batch of long passages stays within what the service can embed before
	// its proxy times out. The first passage is always sent. 0 means no cap.
	MaxRequestChars int
}

// MaxEmbedBatch caps the passages sent in one embedding request.
const MaxEmbedBatch = 64

// maxEmbedChars caps the text of one passage sent to the embedder.
const maxEmbedChars = 32000

// Document returns the text embedded for a passage of the file key.
func (s Space) Document(key, body string) string {
	text := body
	if prefix := s.DocumentPrefix; prefix != "" {
		if strings.Contains(prefix, "title: none") {
			prefix = strings.Replace(prefix, "title: none", "title: "+path.Base(key), 1)
		}
		text = prefix + body
	}
	if len(text) > maxEmbedChars {
		text = text[:maxEmbedChars]
	}
	return text
}

// embeddable selects the units that get vectors: passages of the file, not
// its indexed name, and not blank ones.
const embeddable = `location <> '` + nameLocation + `' and trim(body) <> ''`

// EmbedPending embeds up to batch passages that have no vector in space,
// within space.MaxRequestChars of text, and stores them, returning how many it stored. Vectors from any other space
// are dropped first. It is safe to run while the index syncs: a passage that
// changed or went away while it was being embedded is not stored.
func (ix *Index) EmbedPending(ctx context.Context, space Space, batch int) (int, error) {
	if space.Embedder == nil || space.Fingerprint == "" {
		return 0, errors.New("filecontent: embedding space is not configured")
	}
	if batch <= 0 || batch > MaxEmbedBatch {
		batch = MaxEmbedBatch
	}
	res, err := ix.db.ExecContext(ctx, `delete from unit_vectors where fingerprint <> ?`, space.Fingerprint)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		ix.generation.Add(1)
	}
	rows, err := ix.db.QueryContext(ctx, `
		select rowid, key, body from units
		where `+embeddable+` and rowid not in (select unit from unit_vectors)
		order by key, rowid limit ?`, batch)
	if err != nil {
		return 0, err
	}
	type pending struct {
		unit      int64
		key, body string
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.unit, &p.key, &p.body); err != nil {
			rows.Close()
			return 0, err
		}
		todo = append(todo, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(todo) == 0 {
		return 0, err
	}

	texts := make([]string, 0, len(todo))
	chars := 0
	for i, p := range todo {
		text := space.Document(p.key, p.body)
		if i > 0 && space.MaxRequestChars > 0 && chars+len(text) > space.MaxRequestChars {
			todo = todo[:i]
			break
		}
		chars += len(text)
		texts = append(texts, text)
	}
	vectors, err := space.Embedder.Embed(ctx, texts)
	if err != nil {
		return 0, err
	}
	if len(vectors) != len(todo) {
		return 0, fmt.Errorf("filecontent: embedding returned %d vectors for %d passages", len(vectors), len(todo))
	}

	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for i, p := range todo {
		if len(vectors[i]) == 0 {
			return 0, fmt.Errorf("filecontent: embedding %d is empty", i)
		}
		// Only when the unit row still holds exactly this passage: a sync
		// may have replaced the file while the request was out.
		if _, err := tx.ExecContext(ctx, `
			insert or replace into unit_vectors (unit, key, fingerprint, vector)
			select ?, ?, ?, ? where exists (select 1 from units where rowid = ? and key = ? and body = ?)`,
			p.unit, p.key, space.Fingerprint, encodeVector(normalized(vectors[i])), p.unit, p.key, p.body); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	ix.generation.Add(1)
	// Count what was attempted, not what was stored, so a passage that a
	// sync replaced mid-request does not read as "nothing left to do".
	return len(todo), nil
}

// SkipPending marks the next passage EmbedPending would send as one the
// embedding service rejects, so it stops holding up the passages after it.
// The mark is an empty vector: the passage keeps its full-text match, its
// file is never ranked by embeddings, and a change to the file clears it.
func (ix *Index) SkipPending(ctx context.Context, space Space) error {
	res, err := ix.db.ExecContext(ctx, `
		insert into unit_vectors (unit, key, fingerprint, vector)
		select rowid, key, ?, x'' from units
		where `+embeddable+` and rowid not in (select unit from unit_vectors)
		order by key, rowid limit 1`, space.Fingerprint)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		ix.generation.Add(1)
	}
	return nil
}

// EmbeddingProgress reports how many passages need a vector in the space
// fingerprint names and how many have one.
func (ix *Index) EmbeddingProgress(ctx context.Context, fingerprint string) (embedded, total int, err error) {
	if err := ix.db.QueryRowContext(ctx, `select count(*) from units where `+embeddable).Scan(&total); err != nil {
		return 0, 0, err
	}
	err = ix.db.QueryRowContext(ctx, `select count(*) from unit_vectors where fingerprint = ? and length(vector) > 0`, fingerprint).Scan(&embedded)
	return embedded, total, err
}

// vectorSet is the in-memory copy of one space's vectors, for files whose
// every passage is embedded.
type vectorSet struct {
	generation  uint64
	fingerprint string
	dim         int
	units       []int64
	keys        []string
	vectors     []float32 // len(units) * dim, unit length each
	complete    map[string]bool
}

// loadVectors returns the vectors of space fingerprint, reading them again
// only when the index changed since the last read.
func (ix *Index) loadVectors(ctx context.Context, fingerprint string) (*vectorSet, error) {
	ix.vecMu.Lock()
	defer ix.vecMu.Unlock()
	gen := ix.generation.Load()
	if v := ix.vecs; v != nil && v.generation == gen && v.fingerprint == fingerprint {
		return v, nil
	}

	need := map[string]int{}
	rows, err := ix.db.QueryContext(ctx, `select key, count(*) from units where `+embeddable+` group by key`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			rows.Close()
			return nil, err
		}
		need[key] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	set := &vectorSet{generation: gen, fingerprint: fingerprint, complete: map[string]bool{}}
	have := map[string]int{}
	rows, err = ix.db.QueryContext(ctx, `select unit, key, vector from unit_vectors where fingerprint = ? order by key, unit`, fingerprint)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var unit int64
		var key string
		var blob []byte
		if err := rows.Scan(&unit, &key, &blob); err != nil {
			return nil, err
		}
		vec, err := decodeVector(blob)
		if err != nil {
			return nil, err
		}
		if len(vec) == 0 {
			// A passage the service rejected; its file reads as not embedded.
			continue
		}
		if set.dim == 0 {
			set.dim = len(vec)
		}
		if len(vec) != set.dim {
			// A provider that changed width mid-space; the odd vectors are
			// skipped and their file reads as not embedded.
			continue
		}
		set.units = append(set.units, unit)
		set.keys = append(set.keys, key)
		set.vectors = append(set.vectors, vec...)
		have[key]++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for key, n := range need {
		if have[key] >= n {
			set.complete[key] = true
		}
	}
	ix.vecs = set
	return set, nil
}

// SearchSemantic ranks files by how close their passages are to the query
// vector, made in the space fingerprint names from the same query text.
// Files whose passages are not all embedded yet keep their full-text rank,
// so a new file is findable before the embedder reaches it. semantic is
// false when no file is embedded, in which case the result is exactly
// Search's.
func (ix *Index) SearchSemantic(ctx context.Context, query string, vector []float32, fingerprint string, limit int) (matches []Match, semantic bool, err error) {
	if limit <= 0 {
		limit = 20
	}
	lexical, err := ix.Search(ctx, query, limit)
	if err != nil {
		return nil, false, err
	}
	set, err := ix.loadVectors(ctx, fingerprint)
	if err != nil {
		return nil, false, err
	}
	if len(set.complete) == 0 || set.dim == 0 || len(vector) != set.dim {
		return lexical, false, nil
	}
	q := normalized(append([]float32(nil), vector...))

	type scored struct {
		unit  int64
		score float64
	}
	best := map[string][]scored{} // up to three best passages per file
	for i, unit := range set.units {
		key := set.keys[i]
		if !set.complete[key] {
			continue
		}
		item := scored{unit, dot(q, set.vectors[i*set.dim:(i+1)*set.dim])}
		top := best[key]
		if len(top) == 3 && item.score <= top[2].score {
			continue
		}
		if len(top) < 3 {
			top = append(top, item)
		} else {
			top[2] = item
		}
		for j := len(top) - 1; j > 0 && top[j].score > top[j-1].score; j-- {
			top[j], top[j-1] = top[j-1], top[j]
		}
		best[key] = top
	}
	ranked := make([]Match, 0, len(best))
	for key, top := range best {
		ranked = append(ranked, Match{Key: key, Score: top[0].score})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].Key < ranked[j].Key
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	for i := range ranked {
		for _, s := range best[ranked[i].Key] {
			var loc, body string
			err := ix.db.QueryRowContext(ctx, `select location, body from units where rowid = ?`, s.unit).Scan(&loc, &body)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, false, err
			}
			ranked[i].Passages = append(ranked[i].Passages, Passage{Location: loc, Snippet: snippet(body, query)})
		}
	}

	// Files not fully embedded keep the place full-text search gave them.
	type fill struct {
		rank  int
		match Match
	}
	var fills []fill
	for rank, m := range lexical {
		if !set.complete[m.Key] {
			fills = append(fills, fill{rank, m})
		}
	}
	out := make([]Match, 0, limit)
	for len(out) < limit && (len(ranked) > 0 || len(fills) > 0) {
		if len(fills) > 0 && (fills[0].rank <= len(out) || len(ranked) == 0) {
			out = append(out, fills[0].match)
			fills = fills[1:]
			continue
		}
		out = append(out, ranked[0])
		ranked = ranked[1:]
	}
	return out, true, nil
}

// snippet picks the stretch of about 32 words of body that holds the most
// query words, marking them the way full-text snippets are marked.
func snippet(body, query string) string {
	const window = 32
	words := strings.Fields(body)
	if len(words) == 0 {
		return ""
	}
	terms := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if !stopWords[w] {
			terms[w] = true
		}
	}
	hit := func(w string) bool {
		return terms[strings.ToLower(strings.TrimFunc(w, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}))]
	}
	start, bestHits, hits := 0, -1, 0
	for i := range words {
		if hit(words[i]) {
			hits++
		}
		if i >= window && hit(words[i-window]) {
			hits--
		}
		if s := i - window + 1; hits > bestHits {
			bestHits = hits
			start = max(s, 0)
		}
	}
	end := min(start+window, len(words))
	parts := make([]string, 0, end-start+2)
	if start > 0 {
		parts = append(parts, "…")
	}
	for _, w := range words[start:end] {
		if hit(w) {
			w = "«" + w + "»"
		}
		parts = append(parts, w)
	}
	if end < len(words) {
		parts = append(parts, "…")
	}
	return strings.Join(parts, " ")
}

func normalized(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	scale := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= scale
	}
	return v
}

func dot(a, b []float32) float64 {
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return float64(sum)
}

func encodeVector(v []float32) []byte {
	out := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(x))
	}
	return out
}

func decodeVector(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, errors.New("filecontent: stored vector is not float32 aligned")
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out, nil
}

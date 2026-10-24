package serv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/dosco/graphjin/serv/v3/filecontent"
)

// Semantic file search ranks passages by embedding similarity when
// catalog_search.semantic is enabled. Passages are embedded in the
// background, one batch at a time, into the file content index next to the
// full-text table; a file is ranked by full-text search until all of its
// passages have vectors, and every search falls back to full-text search
// when the embedding service cannot answer in time.

const (
	// fileQueryEmbedTimeout bounds the query embedding a search waits for
	// before answering with full-text results.
	fileQueryEmbedTimeout = 3 * time.Second

	// fileEmbedIdlePoll is how often an idle embedder looks for passages a
	// sync it was not told about added.
	fileEmbedIdlePoll = time.Minute

	// fileEmbedRequestChars caps the text of one passage embedding request.
	// The local service embeds about 2048 tokens (~8k characters) per
	// forward pass and its proxy gives up after two minutes, so requests are
	// sized by length, not only by count. After a failure the cap halves,
	// down to a single passage, and it grows back as requests succeed.
	fileEmbedRequestChars = 16000

	// fileEmbedRequestTimeout bounds one embedding request.
	fileEmbedRequestTimeout = 3 * time.Minute

	// fileEmbedSkipAfter is how many failed batches in a row lead the
	// embedder to check whether one passage, not the service, is the cause.
	fileEmbedSkipAfter = 4
)

// defaultFileEmbedBackoffs are the delays between failed embedding batches,
// the last repeating. The service answers 429 when its queue is full and 503
// while it fails, and both clear on their own.
var defaultFileEmbedBackoffs = []time.Duration{
	2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute,
}

// fileSemantic is one running configuration of semantic file search.
type fileSemantic struct {
	space  *fileSemanticSpace
	cache  *semanticQueryLRU
	wake   chan struct{}
	cancel context.CancelFunc
	done   chan struct{}
}

func (fs *fileSemantic) filecontentSpace() filecontent.Space {
	return filecontent.Space{
		Fingerprint:    fs.space.fingerprint,
		DocumentPrefix: fs.space.conf.DocumentPrefix,
		Embedder:       fs.space,
	}
}

// setSemantic starts semantic search in space, replacing the running one,
// or stops it when space is nil. Stored vectors are kept: turning search
// back on embeds only passages that changed meanwhile.
func (f *fileContent) setSemantic(space *fileSemanticSpace) {
	if f == nil {
		return
	}
	f.semMu.Lock()
	defer f.semMu.Unlock()
	old := f.sem.Load()
	if old == nil && space == nil {
		return
	}
	if old != nil && space != nil && old.space.conf == space.conf {
		return
	}
	var next *fileSemantic
	if space != nil {
		ctx, cancel := context.WithCancel(context.Background())
		next = &fileSemantic{
			space:  space,
			cache:  newSemanticQueryLRU(),
			wake:   make(chan struct{}, 1),
			cancel: cancel,
			done:   make(chan struct{}),
		}
		f.sem.Store(next)
		go f.embedLoop(ctx, next)
	} else {
		f.sem.Store(nil)
	}
	if old != nil {
		old.cancel()
		<-old.done
	}
}

// wakeEmbedder tells the embedder that a sync added passages.
func (f *fileContent) wakeEmbedder() {
	if sem := f.sem.Load(); sem != nil {
		select {
		case sem.wake <- struct{}{}:
		default:
		}
	}
}

// embedLoop embeds every source's missing passages, one batch per request
// and one request at a time, until ctx ends. It resumes where the index
// says it left off, so a restart or a toggle repeats no work.
func (f *fileContent) embedLoop(ctx context.Context, sem *fileSemantic) {
	defer close(sem.done)
	space := sem.filecontentSpace()
	backoffs := f.backoffs
	if len(backoffs) == 0 {
		backoffs = defaultFileEmbedBackoffs
	}
	failures := 0
	space.MaxRequestChars = fileEmbedRequestChars
	for {
		progressed := false
		var failure error
		var failedIx *filecontent.Index
		for _, source := range f.sources() {
			ix, err := f.index(source)
			if err != nil {
				continue
			}
			rctx, cancel := context.WithTimeout(ctx, fileEmbedRequestTimeout)
			n, err := ix.EmbedPending(rctx, space, filecontent.MaxEmbedBatch)
			cancel()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				failure, failedIx = err, ix
				break
			}
			if n > 0 {
				progressed = true
			}
		}

		var wait <-chan time.Time
		switch {
		case failure != nil && failures >= fileEmbedSkipAfter && f.skipRejectedPassage(ctx, failedIx, space):
			failures = 0
			space.MaxRequestChars = fileEmbedRequestChars
			continue
		case failure != nil:
			if failures == 0 && f.log != nil {
				f.log.Warnf("file passage embedding paused; full-text search covers files not yet embedded: %s", redactRuntimeError(failure))
			}
			delay := backoffs[min(failures, len(backoffs)-1)]
			failures++
			// A timeout or gateway error often means the request was too
			// long to finish; retry with less.
			space.MaxRequestChars = max(space.MaxRequestChars/2, 1)
			wait = time.After(delay)
		case progressed:
			if failures > 0 && f.log != nil {
				f.log.Infof("file passage embedding resumed")
			}
			failures = 0
			space.MaxRequestChars = min(space.MaxRequestChars*2, fileEmbedRequestChars)
			continue
		default:
			failures = 0
			wait = time.After(fileEmbedIdlePoll)
		}
		select {
		case <-ctx.Done():
			return
		case <-wait:
		case <-sem.wake:
			if failure != nil {
				// New files do not make a failing service recover; keep
				// waiting out the delay.
				select {
				case <-ctx.Done():
					return
				case <-wait:
				}
			}
		}
	}
}

// skipRejectedPassage tells a passage the service will never embed from a
// service that is down. When a probe embeds but the next passage alone still
// fails, that passage is marked skipped and true is returned; when the probe
// fails too, nothing changes and the loop keeps backing off.
func (f *fileContent) skipRejectedPassage(ctx context.Context, ix *filecontent.Index, space filecontent.Space) bool {
	if ix == nil {
		return false
	}
	rctx, cancel := context.WithTimeout(ctx, fileEmbedRequestTimeout)
	defer cancel()
	if _, err := space.Embedder.Embed(rctx, []string{"ok"}); err != nil {
		return false
	}
	if _, err := ix.EmbedPending(rctx, space, 1); err == nil {
		return true
	}
	if err := ix.SkipPending(rctx, space); err != nil {
		return false
	}
	if f.log != nil {
		f.log.Warnf("skipped a file passage the embedding service rejects; full-text search still finds it")
	}
	return true
}

// search ranks a source's files for query: by passage embeddings when
// semantic search is on and the query can be embedded in time, otherwise by
// full-text search exactly as before.
func (f *fileContent) search(ctx context.Context, ix *filecontent.Index, query string, limit int) ([]filecontent.Match, error) {
	sem := f.sem.Load()
	if sem == nil || strings.TrimSpace(query) == "" {
		return ix.Search(ctx, query, limit)
	}
	vector, err := sem.queryVector(ctx, query)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return ix.Search(ctx, query, limit)
	}
	matches, _, err := ix.SearchSemantic(ctx, query, vector, sem.space.fingerprint, limit)
	return matches, err
}

// queryVector embeds a search query with the query prefix, reusing recent
// ones.
func (fs *fileSemantic) queryVector(ctx context.Context, query string) ([]float32, error) {
	text := fs.space.conf.QueryPrefix + strings.TrimSpace(query)
	sum := sha256.Sum256([]byte(text))
	key := hex.EncodeToString(sum[:])
	if vector, ok := fs.cache.Get(key); ok {
		return vector, nil
	}
	qctx, cancel := context.WithTimeout(ctx, fileQueryEmbedTimeout)
	defer cancel()
	vectors, err := fs.space.Embed(qctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 || len(vectors[0]) == 0 {
		return nil, errors.New("query embedding returned no vector")
	}
	fs.cache.Put(key, vectors[0])
	return vectors[0], nil
}

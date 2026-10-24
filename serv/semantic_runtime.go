package serv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

// semanticCatalog returns the running semantic catalog index, or nil when
// semantic search is off. The index is swapped live by
// reconfigureSemanticSearch, so callers load it once per operation and use
// that value throughout.
func (s *graphjinService) semanticCatalog() *semanticCatalogIndex {
	if s == nil {
		return nil
	}
	return s.liveService().semantic.Load()
}

// reconfigureSemanticSearch brings semantic catalog and file search in line
// with catalog_search.semantic. It runs at start and after gj_config changes
// that section, without a restart: turning it on builds and starts an index
// (lexical results until it is ready), turning it off closes the index and
// stops the file passage embedder, and changing the provider, model,
// endpoint, dimensions or prefixes replaces both. Queries already holding the
// old index finish against it; new ones see the new one.
func (s *graphjinService) reconfigureSemanticSearch() {
	if s == nil {
		return
	}
	live := s.liveService()
	live.semanticMu.Lock()
	defer live.semanticMu.Unlock()
	if live.conf == nil {
		return
	}
	conf := live.conf.CatalogSearch.Semantic

	old := live.semantic.Load()
	if !(conf.Enabled && old != nil && old.conf == conf) {
		var next *semanticCatalogIndex
		if conf.Enabled && live.gj != nil && live.fs != nil {
			index, err := newSemanticCatalogIndex(live)
			if err != nil {
				live.log.Warnf("semantic catalog initialization failed; using lexical search: %s", redactRuntimeError(err))
			} else {
				next = index
			}
		}
		live.semantic.Store(next)
		if old != nil {
			old.Close()
		}
		if next != nil {
			next.Start()
		}
	}

	if fc := live.fileContentReader(); fc != nil {
		fc.setSemantic(live.fileSemanticSpace())
	}
}

// fileSemanticSpace returns the embedding space for file passages, or nil when
// semantic search is off. It shares the catalog's provider settings; the
// fingerprint is the catalog's, qualified so the two never mix.
func (s *graphjinService) fileSemanticSpace() *fileSemanticSpace {
	if s == nil || s.conf == nil || !s.conf.CatalogSearch.Semantic.Enabled {
		return nil
	}
	conf := s.conf.CatalogSearch.Semantic
	fingerprint, err := semanticEmbeddingFingerprint(conf)
	if err != nil {
		if s.log != nil {
			s.log.Warnf("semantic file search unavailable; using full-text search: %s", redactRuntimeError(err))
		}
		return nil
	}
	sum := sha256.Sum256([]byte(fingerprint + "|file-passages|" + "1"))
	requested, named, _ := conf.dimensionCount()
	var dimensions *int
	if named {
		dimensions = &requested
	}
	client := s.liveService().semanticEmbedder
	if client == nil {
		client = newAxSemanticEmbeddingClient(conf)
	}
	return &fileSemanticSpace{
		conf:        conf,
		fingerprint: hex.EncodeToString(sum[:]),
		client:      client,
		dimensions:  dimensions,
	}
}

// fileSemanticSpace is one configuration of semantic file search.
type fileSemanticSpace struct {
	conf        SemanticCatalogSearchConfig
	fingerprint string
	client      SemanticEmbeddingClient
	dimensions  *int
}

// Embed adapts the semantic embedding client to filecontent.Embedder.
func (sp *fileSemanticSpace) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return sp.client.Embed(ctx, texts, sp.dimensions)
}

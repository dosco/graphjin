package serv

import (
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	"github.com/dosco/graphjin/core/v3"
	"github.com/redis/go-redis/v9"
)

const (
	semanticIndexFormatVersion    = 1
	semanticDocumentFormatVersion = 1
	semanticEmbeddingBatchSize    = 64
	semanticEmbeddingConcurrency  = 2
	semanticQueryCacheSize        = 1024
	semanticQueryCacheTTL         = 10 * time.Minute
)

// SemanticEmbeddingClient is the narrow embedding seam used by semantic
// catalog indexing. Tests can supply a deterministic fake without any provider
// calls; production uses the Ax adapter below.
type SemanticEmbeddingClient interface {
	Embed(ctx context.Context, texts []string, dimensions *int) ([][]float32, error)
}

type axSemanticEmbeddingClient struct {
	client ax.AIClient
	model  string
}

func newAxSemanticEmbeddingClient(conf SemanticCatalogSearchConfig) SemanticEmbeddingClient {
	options := map[string]ax.Value{"embed_model": conf.EmbeddingModel}
	if env := strings.TrimSpace(conf.APIKeyEnv); env != "" {
		// Set the configured environment variable even when it is empty so Ax
		// does not silently fall back to a different provider's default key.
		options["api_key"] = os.Getenv(env)
	}
	if baseURL := strings.TrimSpace(conf.BaseURL); baseURL != "" {
		options["base_url"] = baseURL
	}
	return &axSemanticEmbeddingClient{
		client: ax.NewAI(conf.Provider, options),
		model:  conf.EmbeddingModel,
	}
}

func (c *axSemanticEmbeddingClient) Embed(ctx context.Context, texts []string, dimensions *int) ([][]float32, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("Ax embedding client is not initialized")
	}
	items := make([]ax.Value, len(texts))
	for i := range texts {
		items[i] = texts[i]
	}
	request := map[string]ax.Value{
		"texts":       items,
		"embed_model": c.model,
	}
	if dimensions != nil {
		request["dimensions"] = *dimensions
	}
	value, err := c.client.Embed(ctx, request, nil)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Ax embedding response has type %T, want object", value)
	}
	raw, ok := object["embeddings"]
	if !ok {
		return nil, errors.New("Ax embedding response is missing embeddings")
	}
	rows, err := semanticEmbeddingRows(raw)
	if err != nil {
		return nil, err
	}
	out := make([][]float32, len(rows))
	for i, row := range rows {
		vector, err := semanticFloat32Vector(row)
		if err != nil {
			return nil, fmt.Errorf("Ax embedding %d: %w", i, err)
		}
		out[i] = vector
	}
	return out, nil
}

func semanticEmbeddingRows(value any) ([]any, error) {
	switch values := value.(type) {
	case []any:
		return values, nil
	case *ax.AxArray:
		if values == nil {
			return nil, nil
		}
		return []any(values.Items), nil
	default:
		return nil, fmt.Errorf("Ax embeddings have type %T, want array", value)
	}
}

func semanticFloat32Vector(value any) ([]float32, error) {
	switch values := value.(type) {
	case []float32:
		return append([]float32(nil), values...), nil
	case []float64:
		out := make([]float32, len(values))
		for i := range values {
			out[i] = float32(values[i])
		}
		return out, nil
	case []any:
		out := make([]float32, len(values))
		for i, item := range values {
			switch number := item.(type) {
			case float64:
				out[i] = float32(number)
			case float32:
				out[i] = number
			case int:
				out[i] = float32(number)
			default:
				return nil, fmt.Errorf("component %d has type %T", i, item)
			}
		}
		return out, nil
	case *ax.AxArray:
		if values == nil {
			return nil, nil
		}
		return semanticFloat32Vector([]any(values.Items))
	default:
		return nil, fmt.Errorf("vector has type %T", value)
	}
}

type semanticDocument struct {
	Hash          string
	Kind          string
	Text          string
	TargetCardIDs []string
	MemberColumns []string
}

type semanticDocumentMap struct {
	Hash          string   `json:"hash"`
	Kind          string   `json:"kind"`
	TargetCardIDs []string `json:"target_card_ids"`
	MemberColumns []string `json:"member_columns,omitempty"`
	VectorOffset  int      `json:"vector_offset"`
}

type semanticIndexManifest struct {
	FormatVersion         int       `json:"format_version"`
	DocumentFormatVersion int       `json:"document_format_version"`
	GenerationID          string    `json:"generation_id"`
	CreatedAt             time.Time `json:"created_at"`
	Fingerprint           string    `json:"fingerprint"`
	CatalogRevision       string    `json:"catalog_revision"`
	DimensionPreset       string    `json:"dimension_preset"`
	ActualDimension       int       `json:"actual_dimension"`
	DocumentCount         int       `json:"document_count"`
	MapSHA256             string    `json:"map_sha256"`
	MapSize               int       `json:"map_size"`
	VectorsSHA256         string    `json:"vectors_sha256"`
	VectorsSize           int       `json:"vectors_size"`
}

type semanticPersistedIndex struct {
	manifest semanticIndexManifest
	docs     []semanticDocumentMap
	vectors  []float32
}

type semanticCacheItem struct {
	key       string
	vector    []float32
	expiresAt time.Time
}

type semanticQueryLRU struct {
	mu    sync.Mutex
	items map[string]*list.Element
	order *list.List
}

func newSemanticQueryLRU() *semanticQueryLRU {
	return &semanticQueryLRU{items: make(map[string]*list.Element), order: list.New()}
}

func (c *semanticQueryLRU) Get(key string) ([]float32, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.items[key]
	if !ok {
		return nil, false
	}
	item := element.Value.(*semanticCacheItem)
	if time.Now().After(item.expiresAt) {
		delete(c.items, key)
		c.order.Remove(element)
		return nil, false
	}
	c.order.MoveToFront(element)
	return append([]float32(nil), item.vector...), true
}

func (c *semanticQueryLRU) Put(key string, vector []float32) {
	if c == nil || key == "" || len(vector) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.items[key]; ok {
		item := element.Value.(*semanticCacheItem)
		item.vector = append(item.vector[:0], vector...)
		item.expiresAt = time.Now().Add(semanticQueryCacheTTL)
		c.order.MoveToFront(element)
		return
	}
	item := &semanticCacheItem{key: key, vector: append([]float32(nil), vector...), expiresAt: time.Now().Add(semanticQueryCacheTTL)}
	element := c.order.PushFront(item)
	c.items[key] = element
	for c.order.Len() > semanticQueryCacheSize {
		oldest := c.order.Back()
		delete(c.items, oldest.Value.(*semanticCacheItem).key)
		c.order.Remove(oldest)
	}
}

func (c *semanticQueryLRU) Reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.items = make(map[string]*list.Element)
	c.order.Init()
	c.mu.Unlock()
}

type semanticCatalogIndex struct {
	service     *graphjinService
	conf        SemanticCatalogSearchConfig
	embedder    SemanticEmbeddingClient
	fingerprint string
	base        string
	prefix      string
	redis       *redis.Client
	redisErr    error
	ownsRedis   bool

	mu               sync.RWMutex
	active           *semanticPersistedIndex
	forceFullRebuild bool
	dirty            chan struct{}
	cancel           context.CancelFunc
	cache            *semanticQueryLRU
}

var localSemanticBuildMu sync.Mutex

func newSemanticCatalogIndex(s *graphjinService) (*semanticCatalogIndex, error) {
	if s == nil || s.conf == nil || s.fs == nil {
		return nil, errors.New("semantic catalog index requires initialized service state")
	}
	conf := s.conf.CatalogSearch.Semantic
	fingerprint, err := semanticEmbeddingFingerprint(conf)
	if err != nil {
		return nil, err
	}
	embedder := s.semanticEmbedder
	if embedder == nil {
		embedder = newAxSemanticEmbeddingClient(conf)
	}
	base := path.Join(s.conf.DiscoveryCache.Path, "semantic", fingerprint)
	index := &semanticCatalogIndex{
		service:     s,
		conf:        conf,
		embedder:    embedder,
		fingerprint: fingerprint,
		base:        base,
		prefix:      "gj:semantic:" + cleanRuntimeScope(runtimeScope(s.conf, s.metadataDB, s.namespace)) + ":" + fingerprint[:16],
		dirty:       make(chan struct{}, 1),
		cache:       newSemanticQueryLRU(),
	}
	// Semantic coordination owns its Redis client so discovery coordinator
	// rotation after a config fingerprint change cannot invalidate PubSub or
	// lease operations in an already-running semantic index.
	if rawURL := strings.TrimSpace(s.conf.Redis.URL); rawURL != "" {
		opts, err := redis.ParseURL(rawURL)
		if err != nil {
			index.redisErr = fmt.Errorf("invalid redis URL: %w", err)
			return index, nil
		}
		index.redis = redis.NewClient(opts)
		index.ownsRedis = true
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := index.redis.Ping(ctx).Err(); err != nil {
			index.redisErr = fmt.Errorf("redis connection failed: %w", err)
			_ = index.redis.Close()
			index.redis = nil
			index.ownsRedis = false
		}
	}
	return index, nil
}

func (i *semanticCatalogIndex) Start() {
	if i == nil {
		return
	}
	if warm, err := i.newestValidIndex(); err == nil {
		i.setActive(warm)
	}
	ctx, cancel := context.WithCancel(context.Background())
	i.cancel = cancel
	go i.buildLoop(ctx)
	i.CatalogChanged()
}

func (i *semanticCatalogIndex) Close() {
	if i == nil {
		return
	}
	if i.cancel != nil {
		i.cancel()
	}
	if i.ownsRedis && i.redis != nil {
		_ = i.redis.Close()
	}
}

func (i *semanticCatalogIndex) CatalogChanged() {
	if i == nil {
		return
	}
	select {
	case i.dirty <- struct{}{}:
	default:
	}
	if i.redis != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = i.redis.Incr(ctx, i.prefix+":dirty").Err()
		}()
	}
}

func (i *semanticCatalogIndex) buildLoop(ctx context.Context) {
	var pubsub *redis.PubSub
	var messages <-chan *redis.Message
	if i.redis != nil {
		pubsub = i.redis.Subscribe(ctx, i.prefix+":events")
		defer pubsub.Close() //nolint:errcheck
		messages = pubsub.Channel()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-i.dirty:
			i.ensureCurrent(ctx)
		case message := <-messages:
			if message == nil || message.Payload == "failed" {
				continue
			}
			if current := i.current(); current != nil && current.manifest.GenerationID == message.Payload {
				continue
			}
			if loaded, err := i.load(message.Payload); err == nil {
				i.setActive(loaded)
			} else {
				i.warnFallback(fmt.Errorf("semantic generation %q is unavailable on the shared filesystem: %w", message.Payload, err))
			}
		}
	}
}

func (i *semanticCatalogIndex) ensureCurrent(ctx context.Context) {
	if i == nil || i.service == nil || i.service.gj == nil {
		return
	}
	snapshot, err := i.service.gj.CatalogSnapshot()
	if err != nil {
		return
	}
	if active := i.current(); active != nil && active.manifest.CatalogRevision == snapshot.Revision {
		return
	}
	if i.redisErr != nil {
		return
	}
	if i.redis != nil {
		if activeID, err := i.redisActive(ctx); err == nil && activeID != "" {
			loaded, loadErr := i.load(activeID)
			if loadErr != nil {
				i.warnFallback(fmt.Errorf("active semantic generation %q is unavailable on the shared filesystem: %w", activeID, loadErr))
				return
			}
			i.setActive(loaded)
			if loaded.manifest.CatalogRevision == snapshot.Revision {
				return
			}
		}
	}

	if i.redis == nil {
		localSemanticBuildMu.Lock()
		defer localSemanticBuildMu.Unlock()
		if warm, err := i.newestValidIndex(); err == nil && warm.manifest.CatalogRevision == snapshot.Revision {
			i.setActive(warm)
			return
		}
		built, err := i.build(ctx, snapshot)
		if err != nil {
			i.warnFallback(err)
			return
		}
		if err := i.writeReceipt(built.manifest.GenerationID, time.Now().UnixNano()); err != nil {
			i.warnFallback(err)
			return
		}
		i.setActive(built)
		i.cleanupOldGenerations()
		return
	}

	lease, won, err := i.acquire(ctx, 60*time.Second)
	if err != nil {
		i.warnFallback(err)
		return
	}
	if !won {
		if activeID, err := i.redisActive(ctx); err == nil && activeID != "" {
			if loaded, err := i.load(activeID); err == nil {
				i.setActive(loaded)
				if loaded.manifest.CatalogRevision != snapshot.Revision {
					time.AfterFunc(time.Second, i.CatalogChanged)
				}
			}
		}
		time.AfterFunc(time.Second, i.CatalogChanged)
		return
	}
	defer i.release(context.Background(), lease) //nolint:errcheck
	buildCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	i.setBuilderStatus(buildCtx, lease, "building", map[string]any{"catalog_revision": snapshot.Revision})
	lost := make(chan struct{}, 1)
	go i.renewLease(buildCtx, cancel, lease, 60*time.Second, lost)
	built, err := i.build(buildCtx, snapshot)
	select {
	case <-lost:
		if err == nil {
			err = errors.New("semantic lease lost while building index")
		}
	default:
	}
	if err != nil {
		i.publishFailure(ctx, err)
		i.warnFallback(err)
		return
	}
	if err := i.activate(ctx, lease, built.manifest.GenerationID); err != nil {
		i.publishFailure(ctx, err)
		i.warnFallback(err)
		return
	}
	i.setActive(built)
	i.cleanupOldGenerations()
}

func (i *semanticCatalogIndex) build(ctx context.Context, snapshot *core.CatalogSnapshot) (*semanticPersistedIndex, error) {
	metadata, err := i.service.gj.MetadataSnapshot()
	if err != nil {
		return nil, err
	}
	documents := buildSemanticDocuments(metadata, snapshot)
	if len(documents) == 0 {
		return nil, errors.New("semantic document builder produced no safe documents")
	}
	previous, forceFull := i.reusableCurrent()
	if previous == nil && !forceFull {
		previous, _ = i.newestValidIndex()
	}

	requested, named, err := i.conf.dimensionCount()
	if err != nil {
		return nil, err
	}
	reused := make(map[string][]float32)
	if previous != nil && previous.manifest.Fingerprint == i.fingerprint {
		for _, doc := range previous.docs {
			start := doc.VectorOffset
			end := start + previous.manifest.ActualDimension
			if start >= 0 && end <= len(previous.vectors) {
				reused[doc.Hash] = previous.vectors[start:end]
			}
		}
	}

	vectors := make([][]float32, len(documents))
	missing := make([]int, 0, len(documents))
	actualDimension := 0
	if previous != nil {
		actualDimension = previous.manifest.ActualDimension
	}
	for index, document := range documents {
		if vector, ok := reused[document.Hash]; ok {
			vectors[index] = append([]float32(nil), vector...)
			if actualDimension == 0 {
				actualDimension = len(vector)
			}
			continue
		}
		missing = append(missing, index)
	}
	if len(missing) != 0 {
		var dimension *int
		if named {
			dimension = &requested
		}
		embedded, err := i.embedMissing(ctx, documents, missing, dimension)
		if err != nil {
			return nil, err
		}
		for n, index := range missing {
			vector := embedded[n]
			if actualDimension == 0 {
				actualDimension = len(vector)
			}
			if len(vector) != actualDimension {
				return nil, fmt.Errorf("embedding response dimension changed within one generation: got %d, want %d", len(vector), actualDimension)
			}
			vectors[index] = vector
		}
	}
	if named && actualDimension != requested {
		return nil, fmt.Errorf("embedding response dimension %d does not match configured %s dimension %d", actualDimension, i.conf.Dimensions, requested)
	}
	for n := range vectors {
		if len(vectors[n]) != actualDimension {
			return nil, fmt.Errorf("semantic vector %d has dimension %d, want %d", n, len(vectors[n]), actualDimension)
		}
		normalizeSemanticVector(vectors[n])
	}

	id, err := newDiscoveryGenerationID()
	if err != nil {
		return nil, err
	}
	docs := make([]semanticDocumentMap, len(documents))
	flat := make([]float32, 0, len(documents)*actualDimension)
	for n, document := range documents {
		docs[n] = semanticDocumentMap{
			Hash:          document.Hash,
			Kind:          document.Kind,
			TargetCardIDs: append([]string(nil), document.TargetCardIDs...),
			MemberColumns: append([]string(nil), document.MemberColumns...),
			VectorOffset:  len(flat),
		}
		flat = append(flat, vectors[n]...)
	}
	mapData, err := json.Marshal(docs)
	if err != nil {
		return nil, err
	}
	vectorData := encodeSemanticVectors(flat)
	mapSum := sha256.Sum256(mapData)
	vectorSum := sha256.Sum256(vectorData)
	manifest := semanticIndexManifest{
		FormatVersion:         semanticIndexFormatVersion,
		DocumentFormatVersion: semanticDocumentFormatVersion,
		GenerationID:          id,
		CreatedAt:             time.Now().UTC(),
		Fingerprint:           i.fingerprint,
		CatalogRevision:       snapshot.Revision,
		DimensionPreset:       strings.ToLower(strings.TrimSpace(i.conf.Dimensions)),
		ActualDimension:       actualDimension,
		DocumentCount:         len(docs),
		MapSHA256:             hex.EncodeToString(mapSum[:]),
		MapSize:               len(mapData),
		VectorsSHA256:         hex.EncodeToString(vectorSum[:]),
		VectorsSize:           len(vectorData),
	}
	dir := i.generationDir(id)
	if err := i.service.fs.Put(path.Join(dir, "documents.json"), mapData); err != nil {
		return nil, err
	}
	if err := i.service.fs.Put(path.Join(dir, "vectors.f32"), vectorData); err != nil {
		return nil, err
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	// Manifest last: neither partial vectors nor a partial document map are
	// loadable as a semantic generation.
	if err := i.service.fs.Put(path.Join(dir, "manifest.json"), manifestData); err != nil {
		return nil, err
	}
	return &semanticPersistedIndex{manifest: manifest, docs: docs, vectors: flat}, nil
}

func (i *semanticCatalogIndex) embedMissing(ctx context.Context, documents []semanticDocument, missing []int, dimensions *int) ([][]float32, error) {
	type batch struct {
		start   int
		indices []int
	}
	type result struct {
		start   int
		vectors [][]float32
		err     error
	}
	jobs := make(chan batch)
	results := make(chan result, semanticEmbeddingConcurrency)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	for n := 0; n < semanticEmbeddingConcurrency; n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				texts := make([]string, len(job.indices))
				for j, index := range job.indices {
					texts[j] = documents[index].Text
				}
				vectors, err := i.embedder.Embed(workerCtx, texts, dimensions)
				if err == nil && len(vectors) != len(texts) {
					err = fmt.Errorf("embedding batch returned %d vectors for %d documents", len(vectors), len(texts))
				}
				select {
				case results <- result{start: job.start, vectors: vectors, err: err}:
				case <-workerCtx.Done():
					return
				}
			}
		}()
	}
	batches := (len(missing) + semanticEmbeddingBatchSize - 1) / semanticEmbeddingBatchSize
	go func() {
		defer close(jobs)
		for start := 0; start < len(missing); start += semanticEmbeddingBatchSize {
			end := start + semanticEmbeddingBatchSize
			if end > len(missing) {
				end = len(missing)
			}
			select {
			case jobs <- batch{start: start, indices: append([]int(nil), missing[start:end]...)}:
			case <-workerCtx.Done():
				return
			}
		}
	}()
	out := make([][]float32, len(missing))
	for n := 0; n < batches; n++ {
		select {
		case <-ctx.Done():
			cancel()
			return nil, ctx.Err()
		case item := <-results:
			if item.err != nil {
				cancel()
				return nil, item.err
			}
			copy(out[item.start:], item.vectors)
		}
	}
	workers.Wait()
	return out, nil
}

func (i *semanticCatalogIndex) load(id string) (*semanticPersistedIndex, error) {
	dir := i.generationDir(id)
	manifestData, err := i.service.fs.Get(path.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var manifest semanticIndexManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, err
	}
	if manifest.FormatVersion != semanticIndexFormatVersion ||
		manifest.DocumentFormatVersion != semanticDocumentFormatVersion ||
		manifest.GenerationID != id || manifest.Fingerprint != i.fingerprint ||
		manifest.ActualDimension <= 0 || manifest.DocumentCount < 0 {
		return nil, errors.New("incompatible semantic index manifest")
	}
	requested, named, err := i.conf.dimensionCount()
	if err != nil {
		return nil, err
	}
	if named && manifest.ActualDimension != requested {
		return nil, fmt.Errorf("semantic index dimension %d does not match configured %d", manifest.ActualDimension, requested)
	}
	mapData, err := i.service.fs.Get(path.Join(dir, "documents.json"))
	if err != nil {
		return nil, err
	}
	vectorData, err := i.service.fs.Get(path.Join(dir, "vectors.f32"))
	if err != nil {
		return nil, err
	}
	if len(mapData) != manifest.MapSize || len(vectorData) != manifest.VectorsSize {
		return nil, errors.New("semantic index file size mismatch")
	}
	mapSum := sha256.Sum256(mapData)
	vectorSum := sha256.Sum256(vectorData)
	if hex.EncodeToString(mapSum[:]) != manifest.MapSHA256 || hex.EncodeToString(vectorSum[:]) != manifest.VectorsSHA256 {
		return nil, errors.New("semantic index checksum mismatch")
	}
	var docs []semanticDocumentMap
	if err := json.Unmarshal(mapData, &docs); err != nil {
		return nil, err
	}
	vectors, err := decodeSemanticVectors(vectorData)
	if err != nil {
		return nil, err
	}
	if len(docs) != manifest.DocumentCount || len(vectors) != len(docs)*manifest.ActualDimension {
		return nil, errors.New("semantic index vector/document count mismatch")
	}
	for _, doc := range docs {
		if doc.VectorOffset < 0 || doc.VectorOffset+manifest.ActualDimension > len(vectors) {
			return nil, errors.New("semantic index vector offset is out of range")
		}
	}
	return &semanticPersistedIndex{manifest: manifest, docs: docs, vectors: vectors}, nil
}

func (i *semanticCatalogIndex) newestValidIndex() (*semanticPersistedIndex, error) {
	entries, err := i.service.fs.List(path.Join(i.base, "activations"))
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(entries)))
	for _, name := range entries {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := i.service.fs.Get(path.Join(i.base, "activations", name))
		if err != nil {
			continue
		}
		var receipt discoveryActivationReceipt
		if json.Unmarshal(data, &receipt) != nil ||
			receipt.FormatVersion != semanticIndexFormatVersion || receipt.Fingerprint != i.fingerprint {
			continue
		}
		if loaded, err := i.load(receipt.GenerationID); err == nil {
			return loaded, nil
		}
	}
	return nil, errors.New("no compatible semantic index")
}

func (i *semanticCatalogIndex) writeReceipt(id string, fence int64) error {
	receipt := discoveryActivationReceipt{
		FormatVersion: semanticIndexFormatVersion,
		GenerationID:  id,
		Fingerprint:   i.fingerprint,
		Fence:         fence,
		ActivatedAt:   time.Now().UTC(),
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	return i.service.fs.Put(path.Join(i.base, "activations", id+".json"), data)
}

func (i *semanticCatalogIndex) current() *semanticPersistedIndex {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.active
}

func (i *semanticCatalogIndex) reusableCurrent() (*semanticPersistedIndex, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.active, i.forceFullRebuild
}

func (i *semanticCatalogIndex) setActive(index *semanticPersistedIndex) {
	i.mu.Lock()
	i.active = index
	i.forceFullRebuild = false
	i.mu.Unlock()
}

func (i *semanticCatalogIndex) generationDir(id string) string {
	return path.Join(i.base, "generations", id)
}

func (i *semanticCatalogIndex) warnFallback(err error) {
	if i != nil && i.service != nil && i.service.log != nil && err != nil {
		i.service.log.Warnf("semantic catalog index unavailable; using lexical search: %s", redactRuntimeError(err))
	}
}

func (i *semanticCatalogIndex) acquire(ctx context.Context, ttl time.Duration) (discoveryLease, bool, error) {
	if i.redis == nil {
		return discoveryLease{fence: time.Now().UnixNano(), value: defaultRuntimeNodeID()}, true, nil
	}
	qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	value, err := i.redis.Eval(qctx, discoveryAcquireScript,
		[]string{i.prefix + ":lease", i.prefix + ":fence"},
		defaultRuntimeNodeID(), int64(ttl/time.Millisecond)).Int64()
	if err != nil || value <= 0 {
		return discoveryLease{}, false, err
	}
	return discoveryLease{fence: value, value: defaultRuntimeNodeID() + "|" + fmt.Sprint(value)}, true, nil
}

func (i *semanticCatalogIndex) release(ctx context.Context, lease discoveryLease) error {
	if i.redis == nil {
		return nil
	}
	qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return i.redis.Eval(qctx, discoveryReleaseScript, []string{i.prefix + ":lease"}, lease.value).Err()
}

func (i *semanticCatalogIndex) renewLease(ctx context.Context, cancelBuild context.CancelFunc, lease discoveryLease, ttl time.Duration, lost chan<- struct{}) {
	if i.redis == nil {
		return
	}
	ticker := time.NewTicker(ttl / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			value, err := i.redis.Eval(qctx, discoveryRenewScript, []string{i.prefix + ":lease"}, lease.value, int64(ttl/time.Millisecond)).Int64()
			cancel()
			if err != nil || value != 1 {
				select {
				case lost <- struct{}{}:
				default:
				}
				cancelBuild()
				return
			}
		}
	}
}

func (i *semanticCatalogIndex) activate(ctx context.Context, lease discoveryLease, id string) error {
	if i.redis != nil {
		status, _ := json.Marshal(map[string]any{"status": "active", "generation_id": id, "fence": lease.fence, "updated_at": time.Now().UTC()})
		qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		value, err := i.redis.Eval(qctx, discoveryActivateScript,
			[]string{i.prefix + ":lease", i.prefix + ":active", i.prefix + ":status", i.prefix + ":events"},
			lease.value, id, string(status)).Int64()
		if err != nil {
			return err
		}
		if value != 1 {
			return errors.New("semantic activation rejected by fencing token")
		}
	}
	return i.writeReceipt(id, lease.fence)
}

func (i *semanticCatalogIndex) redisActive(ctx context.Context) (string, error) {
	if i.redis == nil {
		return "", nil
	}
	qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	value, err := i.redis.Get(qctx, i.prefix+":active").Result()
	if err == redis.Nil {
		return "", nil
	}
	return value, err
}

func (i *semanticCatalogIndex) setBuilderStatus(ctx context.Context, lease discoveryLease, status string, extra map[string]any) {
	if i == nil || i.redis == nil {
		return
	}
	value := map[string]any{
		"status":     status,
		"fence":      lease.fence,
		"node_id":    defaultRuntimeNodeID(),
		"updated_at": time.Now().UTC(),
	}
	for key, item := range extra {
		value[key] = item
	}
	data, _ := json.Marshal(value)
	qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = i.redis.Set(qctx, i.prefix+":status", data, 0).Err()
}

func (i *semanticCatalogIndex) publishFailure(ctx context.Context, cause error) {
	if i == nil || i.redis == nil {
		return
	}
	data, _ := json.Marshal(map[string]any{
		"status":     "failed",
		"updated_at": time.Now().UTC(),
		"error":      redactRuntimeError(cause),
	})
	qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = i.redis.Set(qctx, i.prefix+":status", data, 0).Err()
	_ = i.redis.Publish(qctx, i.prefix+":events", "failed").Err()
}

func (i *semanticCatalogIndex) cleanupOldGenerations() {
	if i == nil || i.service == nil || i.service.fs == nil {
		return
	}
	deleter, ok := i.service.fs.(interface{ Delete(string) error })
	retain := i.service.conf.DiscoveryCache.RetainGenerations
	if !ok || retain <= 0 {
		return
	}
	entries, err := i.service.fs.List(path.Join(i.base, "activations"))
	if err != nil {
		return
	}
	var receipts []string
	for _, name := range entries {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := i.service.fs.Get(path.Join(i.base, "activations", name))
		if err != nil {
			continue
		}
		var receipt discoveryActivationReceipt
		if json.Unmarshal(data, &receipt) != nil ||
			receipt.FormatVersion != semanticIndexFormatVersion ||
			receipt.Fingerprint != i.fingerprint ||
			receipt.GenerationID != strings.TrimSuffix(name, ".json") {
			continue
		}
		if _, err := i.load(receipt.GenerationID); err == nil {
			receipts = append(receipts, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(receipts)))
	if len(receipts) <= retain {
		return
	}
	for _, name := range receipts[retain:] {
		id := strings.TrimSuffix(name, ".json")
		_ = deleter.Delete(path.Join(i.generationDir(id), "documents.json"))
		_ = deleter.Delete(path.Join(i.generationDir(id), "vectors.f32"))
		_ = deleter.Delete(path.Join(i.generationDir(id), "manifest.json"))
		_ = deleter.Delete(path.Join(i.base, "activations", name))
	}
}

func semanticEmbeddingFingerprint(conf SemanticCatalogSearchConfig) (string, error) {
	if _, _, err := conf.dimensionCount(); err != nil {
		return "", err
	}
	data, err := json.Marshal(map[string]string{
		"provider":         strings.ToLower(strings.TrimSpace(conf.Provider)),
		"model":            strings.TrimSpace(conf.EmbeddingModel),
		"base_url":         strings.TrimRight(strings.TrimSpace(conf.BaseURL), "/"),
		"dimension_preset": strings.ToLower(strings.TrimSpace(conf.Dimensions)),
		"document_format":  fmt.Sprint(semanticDocumentFormatVersion),
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeSemanticVector(vector []float32) {
	var norm float64
	for _, value := range vector {
		norm += float64(value) * float64(value)
	}
	if norm == 0 {
		return
	}
	scale := float32(1 / math.Sqrt(norm))
	for n := range vector {
		vector[n] *= scale
	}
}

func encodeSemanticVectors(vectors []float32) []byte {
	var buf bytes.Buffer
	buf.Grow(len(vectors) * 4)
	_ = binary.Write(&buf, binary.LittleEndian, vectors)
	return buf.Bytes()
}

func decodeSemanticVectors(data []byte) ([]float32, error) {
	if len(data)%4 != 0 {
		return nil, errors.New("semantic vector file length is not float32 aligned")
	}
	out := make([]float32, len(data)/4)
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &out); err != nil {
		return nil, err
	}
	return out, nil
}

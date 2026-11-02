package serv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
)

// Memory is an artifact kind: a short fact a user wants the agent to keep
// across conversations. It reuses the annotation tiers: an "observed" memory
// is personal (owner-only); "approved" publishes it to the owner's account
// as team memory. Recall runs on the server before every agent run, so the
// model is handed relevant memories rather than having to search for them.

const (
	artifactKindMemory = "memory"

	maxMemoryContentBytes = 4 * 1024
	// maxRecalledMemoryBytes caps what recall adds to a run's context.
	maxRecalledMemoryBytes = 2 * 1024
	memoryEmbedCacheSize   = 4096
)

var errMemoryNotFound = errors.New("memory_not_found")

func (s *graphjinService) memoryEnabled() bool {
	return s != nil && s.conf != nil && s.conf.Core.Artifacts.Enabled && s.conf.Memory.Enabled
}

func (h artifactControlPlane) upsertMemory(ctx context.Context, root core.ManagedMutationRoot, existing map[string]any) (map[string]any, error) {
	s := h.service
	if !s.memoryEnabled() {
		return nil, fmt.Errorf("agent memory is disabled: set memory.enabled")
	}
	actorID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("memory write requires user identity")
	}
	if err := s.checkArtifactKindWritable(artifactKindMemory); err != nil {
		return nil, err
	}
	for field := range root.Input {
		switch field {
		case "kind", "content", "tier", "id":
		default:
			return nil, fmt.Errorf("memory %s is not writable", field)
		}
	}
	admin := s.identityRoleIsAdmin(ctx)
	if existing != nil {
		if normalizeArtifactKind(stringMapValue(existing, "kind")) != artifactKindMemory {
			return nil, fmt.Errorf("memory update target is not a memory")
		}
		if !admin && stringMapValue(existing, "owner_id") != actorID {
			return nil, errMemoryNotFound
		}
	} else if root.Operation == "update" {
		return nil, errMemoryNotFound
	}

	content := strings.TrimSpace(stringInput(root.Input, "content", ""))
	if _, supplied := root.Input["content"]; !supplied && existing != nil {
		content = stringMapValue(existing, "content")
	}
	if content == "" {
		return nil, fmt.Errorf("memory content is required")
	}
	if len(content) > maxMemoryContentBytes {
		return nil, fmt.Errorf("memory content exceeds %d bytes", maxMemoryContentBytes)
	}
	tier := annotationTierObserved
	if existing != nil {
		tier = normalizeAnnotationTier(stringMapValue(existing, "tier"))
	}
	if _, supplied := root.Input["tier"]; supplied {
		var err error
		if tier, err = parseAnnotationTier(stringInput(root.Input, "tier", "")); err != nil {
			return nil, err
		}
	} else if existing != nil && content != stringMapValue(existing, "content") {
		// Editing a team memory takes it back to personal until republished.
		tier = annotationTierObserved
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	ownerID := actorID
	accountID, _ := identityVarString(ctx, "account_id")
	id, createdAt, revision := "", now, int64(1)
	approvedBy, approvedAt := "", ""
	if existing != nil {
		ownerID = stringMapValue(existing, "owner_id")
		accountID = stringMapValue(existing, "account_id")
		id = stringMapValue(existing, "id")
		createdAt = stringMapValue(existing, "created_at")
		revision = int64MapValue(existing, "revision") + 1
		approvedBy = stringMapValue(existing, "approved_by")
		approvedAt = stringMapValue(existing, "approved_at")
	} else {
		if err := s.enforceMemoryLimit(ctx, actorID); err != nil {
			return nil, err
		}
		id = "memory:" + taskRandomHex()
	}
	visibility := "user"
	if tier == annotationTierApproved {
		visibility = "account"
		if approvedBy == "" {
			approvedBy, approvedAt = actorID, now
		}
	} else {
		approvedBy, approvedAt = "", ""
	}
	input := map[string]any{
		"id": id, "name": id, "kind": artifactKindMemory, "path": "", "source": "database",
		"visibility": visibility, "read_only": false, "account_id": accountID, "owner_id": ownerID,
		"content": content, "content_json": nil, "metadata_json": nil,
		"content_hash": hashString(content), "status": "approved", "target_ref": "", "tier": tier,
		"task_id": "", "approved_by": approvedBy, "approved_at": approvedAt,
		"revision": revision, "created_at": createdAt, "updated_at": now,
	}
	var rows []map[string]any
	var err error
	if existing == nil {
		rows, err = s.internalStoreMutationRows(ctx, "artifacts", `insert: $input`, artifactStoreFields, map[string]any{"input": input})
	} else {
		update := cloneMap(input)
		for _, field := range []string{"id", "account_id", "owner_id", "created_at"} {
			delete(update, field)
		}
		rows, err = s.internalStoreMutationRows(ctx, "artifacts", `where: { id: { eq: $id } }, update: $input`, artifactStoreFields, map[string]any{"id": id, "input": update})
	}
	if err != nil {
		return nil, err
	}
	if err := s.bumpArtifactRevision(ctx, "artifacts"); err != nil {
		return nil, err
	}
	s.markArtifactChanged("memory mutation")
	if len(rows) != 0 {
		return memoryAPIRow(rows[0], actorID), nil
	}
	return memoryAPIRow(input, actorID), nil
}

func (s *graphjinService) enforceMemoryLimit(ctx context.Context, ownerID string) error {
	limit := s.conf.Memory.effectiveMaxPerOwner()
	rows, err := s.internalStoreRows(ctx, "artifacts",
		fmt.Sprintf(`where: { kind: { eq: "memory" }, owner_id: { eq: $owner_id } }, limit: %d`, limit),
		"id", map[string]any{"owner_id": ownerID})
	if err != nil {
		return err
	}
	if len(rows) >= limit {
		return fmt.Errorf("memory limit reached: delete a memory before adding another (limit %d)", limit)
	}
	return nil
}

// visibleMemoryRows returns the caller's own memories plus the team memories
// published to the caller's account.
func (s *graphjinService) visibleMemoryRows(ctx context.Context) ([]map[string]any, error) {
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, nil
	}
	own, err := s.internalStoreAllRows(ctx, "artifacts", `where: { kind: { eq: "memory" }, owner_id: { eq: $owner_id } }`,
		artifactStoreFields, map[string]any{"owner_id": ownerID})
	if err != nil {
		return nil, err
	}
	rows := own
	if accountID, _ := identityVarString(ctx, "account_id"); accountID != "" {
		team, err := s.internalStoreAllRows(ctx, "artifacts",
			`where: { kind: { eq: "memory" }, tier: { eq: "approved" }, account_id: { eq: $account_id } }`,
			artifactStoreFields, map[string]any{"account_id": accountID})
		if err != nil {
			return nil, err
		}
		for _, row := range team {
			if stringMapValue(row, "owner_id") != ownerID {
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

func memoryAPIRow(row map[string]any, viewerID string) map[string]any {
	scope := "personal"
	if normalizeAnnotationTier(stringMapValue(row, "tier")) == annotationTierApproved {
		scope = "team"
	}
	return map[string]any{
		"id":         stringMapValue(row, "id"),
		"content":    stringMapValue(row, "content"),
		"scope":      scope,
		"tier":       normalizeAnnotationTier(stringMapValue(row, "tier")),
		"mine":       stringMapValue(row, "owner_id") == viewerID,
		"owner_ref":  safeArtifactIdentity(stringMapValue(row, "owner_id"), false),
		"created_at": stringMapValue(row, "created_at"),
		"updated_at": stringMapValue(row, "updated_at"),
	}
}

// --- recall ------------------------------------------------------------------

// withMemoryRecall wraps a runner so each run starts with the caller's most
// relevant memories in its context.
func (s *graphjinService) withMemoryRecall(runner graphjinAgentRunner) graphjinAgentRunner {
	if !s.memoryEnabled() || runner == nil {
		return runner
	}
	return memoryRecallRunner{s: s, inner: runner}
}

type memoryRecallRunner struct {
	s     *graphjinService
	inner graphjinAgentRunner
}

func (r memoryRecallRunner) Run(ctx context.Context, req gjagent.Request) (gjagent.Response, error) {
	memories, err := r.s.recallMemories(ctx, req.Instruction)
	if err != nil && r.s.log != nil {
		r.s.log.Warnf("memory recall skipped: %v", err)
	}
	for _, memory := range memories {
		req.Memories = append(req.Memories, stringMapValue(memory, "content"))
	}
	return r.inner.Run(ctx, req)
}

// recallMemories ranks the caller's visible memories against the request.
// With an embedding client it ranks by cosine similarity; without one it falls
// back to shared-word overlap and returns only memories that share a word.
func (s *graphjinService) recallMemories(ctx context.Context, query string) ([]map[string]any, error) {
	rows, err := s.visibleMemoryRows(ctx)
	if err != nil || len(rows) == 0 || strings.TrimSpace(query) == "" {
		return nil, err
	}
	limit := s.conf.Memory.effectiveRecallLimit()
	type scored struct {
		row   map[string]any
		score float64
	}
	ranked := make([]scored, 0, len(rows))
	if embedder := s.memoryEmbedder(); embedder != nil {
		texts := make([]string, 0, len(rows))
		for _, row := range rows {
			texts = append(texts, stringMapValue(row, "content"))
		}
		vectors, err := memoryEmbeddings.embed(ctx, embedder, append([]string{query}, texts...))
		if err != nil {
			return nil, fmt.Errorf("embed memories: %w", err)
		}
		for i, row := range rows {
			ranked = append(ranked, scored{row, cosineSimilarity(vectors[0], vectors[i+1])})
		}
	} else {
		words := memoryWords(query)
		for _, row := range rows {
			if score := keywordOverlap(words, memoryWords(stringMapValue(row, "content"))); score > 0 {
				ranked = append(ranked, scored{row, score})
			}
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	out := make([]map[string]any, 0, limit)
	used := 0
	for _, item := range ranked {
		if len(out) == limit {
			break
		}
		content := stringMapValue(item.row, "content")
		if used+len(content) > maxRecalledMemoryBytes {
			continue
		}
		used += len(content)
		scope := "personal"
		if normalizeAnnotationTier(stringMapValue(item.row, "tier")) == annotationTierApproved {
			scope = "team"
		}
		out = append(out, map[string]any{"content": content, "scope": scope})
	}
	return out, nil
}

// memoryEmbedder reuses the catalog's semantic embedding client: an injected
// client first, then the Ax adapter when catalog_search.semantic is enabled.
func (s *graphjinService) memoryEmbedder() SemanticEmbeddingClient {
	if s.semanticEmbedder != nil {
		return s.semanticEmbedder
	}
	conf := s.conf.CatalogSearch.Semantic
	if !conf.Enabled || strings.TrimSpace(conf.EmbeddingModel) == "" {
		return nil
	}
	memoryEmbedderOnce.Do(func() { memoryAxEmbedder = newAxSemanticEmbeddingClient(conf) })
	return memoryAxEmbedder
}

var (
	memoryEmbedderOnce sync.Once
	memoryAxEmbedder   SemanticEmbeddingClient
	memoryEmbeddings   = &embeddingCache{vectors: make(map[string][]float32)}
)

// embeddingCache is a process-local cache of embeddings keyed by text hash.
// It holds no state of record: any replica can rebuild it from the store.
type embeddingCache struct {
	mu      sync.Mutex
	vectors map[string][]float32
}

func (c *embeddingCache) embed(ctx context.Context, client SemanticEmbeddingClient, texts []string) ([][]float32, error) {
	keys := make([]string, len(texts))
	out := make([][]float32, len(texts))
	var missing []string
	var missingIdx []int
	c.mu.Lock()
	for i, text := range texts {
		sum := sha256.Sum256([]byte(text))
		keys[i] = hex.EncodeToString(sum[:])
		if v, ok := c.vectors[keys[i]]; ok {
			out[i] = v
		} else {
			missing = append(missing, text)
			missingIdx = append(missingIdx, i)
		}
	}
	c.mu.Unlock()
	if len(missing) == 0 {
		return out, nil
	}
	vectors, err := client.Embed(ctx, missing, nil)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(missing) {
		return nil, fmt.Errorf("embedding client returned %d vectors for %d texts", len(vectors), len(missing))
	}
	c.mu.Lock()
	if len(c.vectors)+len(vectors) > memoryEmbedCacheSize {
		c.vectors = make(map[string][]float32)
	}
	for j, i := range missingIdx {
		out[i] = vectors[j]
		c.vectors[keys[i]] = vectors[j]
	}
	c.mu.Unlock()
	return out, nil
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func memoryWords(text string) map[string]struct{} {
	words := make(map[string]struct{})
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(word) >= 3 {
			words[word] = struct{}{}
		}
	}
	return words
}

func keywordOverlap(query, memory map[string]struct{}) float64 {
	shared := 0
	for word := range query {
		if _, ok := memory[word]; ok {
			shared++
		}
	}
	return float64(shared)
}

// --- REST --------------------------------------------------------------------

func (s1 *HttpService) AgentMemories(ah auth.HandlerFunc) http.Handler {
	return apiV1Handler(s1, nil, s1.apiV1AgentMemories(), ah)
}

func (s1 *HttpService) AgentMemoriesWithNS(ah auth.HandlerFunc, ns string) http.Handler {
	return apiV1Handler(s1, &ns, s1.apiV1AgentMemories(), ah)
}

// apiV1AgentMemories serves the console's memory page:
//
//	GET    /api/v1/agent/memories        the caller's memories and team memories
//	POST   /api/v1/agent/memories        add a personal memory {content}
//	PATCH  /api/v1/agent/memories/{id}   edit {content} or publish {tier: approved|observed}
//	DELETE /api/v1/agent/memories/{id}   delete
func (s1 *HttpService) apiV1AgentMemories() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*graphjinService)
		w.Header().Set("Content-Type", "application/json")
		if !s.memoryEnabled() {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "agent memory is disabled"})
			return
		}
		if err := s.checkGraphJinInitialized(); err != nil {
			renderErr(w, err)
			return
		}
		ctx := s.applyIdentityContext(r.Context())
		viewerID, ok := artifactUserID(ctx)
		if !ok {
			writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": "agent memory requires a signed-in user"})
			return
		}
		cp := newArtifactControlPlane(s)
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, routeAgentMemories), "/")
		switch {
		case id == "" && r.Method == http.MethodGet:
			rows, err := s.visibleMemoryRows(ctx)
			out := make([]map[string]any, 0, len(rows))
			for _, row := range rows {
				out = append(out, memoryAPIRow(row, viewerID))
			}
			sort.SliceStable(out, func(i, j int) bool {
				return stringMapValue(out[i], "updated_at") > stringMapValue(out[j], "updated_at")
			})
			writeMemoryResult(w, http.StatusOK, out, err)
		case id == "" && r.Method == http.MethodPost:
			input, err := readJSONMap(r)
			if err == nil {
				var row map[string]any
				row, err = cp.upsertMemory(ctx, core.ManagedMutationRoot{Operation: "insert",
					Input: map[string]any{"content": input["content"]}}, nil)
				writeMemoryResult(w, http.StatusCreated, row, err)
				return
			}
			writeMemoryResult(w, http.StatusBadRequest, nil, err)
		case id != "" && r.Method == http.MethodPatch:
			input, err := readJSONMap(r)
			if err == nil {
				var existing, row map[string]any
				existing, err = s.internalArtifactStoreRow(ctx, id)
				if err == nil {
					if existing == nil {
						err = errMemoryNotFound
					} else {
						row, err = cp.upsertMemory(ctx, core.ManagedMutationRoot{Operation: "update", Input: input}, existing)
					}
				}
				writeMemoryResult(w, http.StatusOK, row, err)
				return
			}
			writeMemoryResult(w, http.StatusBadRequest, nil, err)
		case id != "" && r.Method == http.MethodDelete:
			existing, err := s.internalArtifactStoreRow(ctx, id)
			if err == nil && (existing == nil || normalizeArtifactKind(stringMapValue(existing, "kind")) != artifactKindMemory ||
				(stringMapValue(existing, "owner_id") != viewerID && !s.identityRoleIsAdmin(ctx))) {
				err = errMemoryNotFound
			}
			if err == nil {
				_, err = cp.deleteArtifact(ctx, core.ManagedMutationRoot{Operation: "delete", Input: map[string]any{"id": id}})
			}
			writeMemoryResult(w, http.StatusOK, map[string]any{"id": id, "deleted": err == nil}, err)
		default:
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "not found"})
		}
	})
}

func writeMemoryResult(w http.ResponseWriter, status int, data any, err error) {
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, errMemoryNotFound) {
			code = http.StatusNotFound
		}
		writeJSONStatus(w, code, map[string]any{"error": err.Error()})
		return
	}
	writeJSONStatus(w, status, map[string]any{"data": data})
}

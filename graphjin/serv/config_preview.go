package serv

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

const (
	configPreviewTTL = 10 * time.Minute
	configPreviewMax = 64
)

type configPreviewRecord struct {
	ID                  string
	PatchHash           string
	BaseCatalogRevision string
	ExpiresAt           time.Time
	ChangeSummaryJSON   string
	FindingsJSON        string
	ErrorsJSON          string
}

type configPreviewStore struct {
	mu    sync.Mutex
	ttl   time.Duration
	max   int
	items map[string]configPreviewRecord
	order []string
}

func newConfigPreviewStore() *configPreviewStore {
	return &configPreviewStore{
		ttl:   configPreviewTTL,
		max:   configPreviewMax,
		items: make(map[string]configPreviewRecord),
	}
}

func (s *configPreviewStore) put(rec configPreviewRecord) configPreviewRecord {
	if s == nil {
		return rec
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	s.pruneLocked(now)
	if strings.TrimSpace(rec.ID) == "" {
		rec.ID = newConfigPreviewID()
	}
	if rec.ExpiresAt.IsZero() {
		rec.ExpiresAt = now.Add(s.ttl)
	}
	s.items[rec.ID] = rec
	s.order = append(s.order, rec.ID)
	s.pruneLocked(now)
	return rec
}

func (s *configPreviewStore) get(id string) (configPreviewRecord, bool) {
	if s == nil || strings.TrimSpace(id) == "" {
		return configPreviewRecord{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	s.pruneLocked(now)
	rec, ok := s.items[id]
	if !ok || !rec.ExpiresAt.After(now) {
		delete(s.items, id)
		return configPreviewRecord{}, false
	}
	return rec, true
}

func (s *configPreviewStore) delete(id string) {
	if s == nil || strings.TrimSpace(id) == "" {
		return
	}
	s.mu.Lock()
	delete(s.items, id)
	s.mu.Unlock()
}

func (s *configPreviewStore) pruneLocked(now time.Time) {
	if s.items == nil {
		s.items = make(map[string]configPreviewRecord)
	}
	filtered := s.order[:0]
	for _, id := range s.order {
		rec, ok := s.items[id]
		if !ok {
			continue
		}
		if !rec.ExpiresAt.After(now) {
			delete(s.items, id)
			continue
		}
		if len(filtered) != 0 && filtered[len(filtered)-1] == id {
			continue
		}
		filtered = append(filtered, id)
	}
	s.order = filtered
	for len(s.items) > s.max && len(s.order) != 0 {
		id := s.order[0]
		s.order = s.order[1:]
		delete(s.items, id)
	}
}

func newConfigPreviewID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return "cfgprev_" + hex.EncodeToString(buf[:])
	}
	return "cfgprev_" + hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
}

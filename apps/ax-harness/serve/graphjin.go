package serve

import (
	"container/list"
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/open-neko/openneko/apps/ax-harness/harness"
)

const (
	historyTurns   = 8
	historyThreads = 500
)

// historyStore keeps each thread's recent data questions and answers, so a
// follow-up such as "and last month?" resolves. Least recently asked threads
// go first.
type historyStore struct {
	mu    sync.Mutex
	order *list.List
	byKey map[string]*list.Element
}

type historyEntry struct {
	key   string
	turns []HistoryTurn
}

func (h *historyStore) get(key string) []HistoryTurn {
	h.mu.Lock()
	defer h.mu.Unlock()
	if el, ok := h.byKey[key]; ok {
		return append([]HistoryTurn(nil), el.Value.(*historyEntry).turns...)
	}
	return nil
}

func (h *historyStore) add(key string, turns ...HistoryTurn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.byKey == nil {
		h.order, h.byKey = list.New(), map[string]*list.Element{}
	}
	el, ok := h.byKey[key]
	if !ok {
		el = h.order.PushBack(&historyEntry{key: key})
		h.byKey[key] = el
	}
	entry := el.Value.(*historyEntry)
	entry.turns = append(entry.turns, turns...)
	if len(entry.turns) > historyTurns {
		entry.turns = entry.turns[len(entry.turns)-historyTurns:]
	}
	h.order.MoveToBack(el)
	for h.order.Len() > historyThreads {
		oldest := h.order.Front()
		h.order.Remove(oldest)
		delete(h.byKey, oldest.Value.(*historyEntry).key)
	}
}

// graphJinAsk gives a turn GraphJin's agent as the user its token names.
func (s *Server) graphJinAsk(identity GraphJinIdentity) harness.GraphJinAsk {
	key := ""
	if identity.ThreadID != "" {
		key = identity.OrgID + "\x00" + identity.ThreadID
	}
	return func(ctx context.Context, instruction string, maxSteps int, onStep func(string)) (json.RawMessage, int64, error) {
		req := AskRequest{Instruction: instruction, MaxSteps: maxSteps, OnStep: onStep}
		if key != "" {
			req.History = s.history.get(key)
		}
		result, err := s.Ask(ctx, identity.Token, req)
		if err != nil {
			return nil, 0, err
		}
		if key != "" {
			turns := []HistoryTurn{{Role: "user", Content: instruction}}
			if strings.TrimSpace(result.Answer) != "" {
				turns = append(turns, HistoryTurn{Role: "assistant", Content: result.Answer, Status: result.Status})
			}
			s.history.add(key, turns...)
		}
		return result.Response, result.Tokens, nil
	}
}

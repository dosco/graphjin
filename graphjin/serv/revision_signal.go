package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

const (
	revisionSignalRootTable  = "gj_internal_revision"
	internalRecoveryInterval = time.Hour
)

type revisionSignalHandler struct {
	service *graphjinService
}

type revisionSignal struct {
	Domain   string
	Revision int64
}

func (h revisionSignalHandler) ManagedQueryTables() []core.ManagedTable {
	if h.service == nil || h.service.conf == nil || !h.service.conf.Artifacts.Enabled {
		return nil
	}
	return []core.ManagedTable{managedTable(revisionSignalRootTable, []core.ManagedColumn{
		cpCol("domain", "text", true),
		cpCol("revision", "integer", false),
		cpCol("updated_at", "text", false),
	})}
}

func (h revisionSignalHandler) ExecuteManagedQuery(
	ctx context.Context,
	req core.ManagedQueryRequest,
) (json.RawMessage, error) {
	if h.service == nil {
		return nil, fmt.Errorf("revision signal service is unavailable")
	}
	role, _ := ctx.Value(core.UserRoleKey).(string)
	if role != graphjinInternalStoreRole {
		return nil, fmt.Errorf("revision signal access denied")
	}
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		if root.Table != revisionSignalRootTable {
			return nil, fmt.Errorf("unsupported GraphJin revision signal root: %s", root.Table)
		}
		rows, err := h.service.internalStoreRows(ctx, "revisions", "", revisionStoreFields, nil)
		if err != nil {
			return nil, err
		}
		rows = h.revisionRows(rows)
		out[root.FieldName] = filterRows(applyManagedQuery(rows, root), root.Fields)
	}
	return json.Marshal(out)
}

func (h revisionSignalHandler) revisionRows(stored []map[string]any) []map[string]any {
	domains := []string{"artifacts"}
	if h.service != nil && h.service.watchesEnabled() {
		domains = append(domains, "watches", "watch_events")
	}
	byDomain := make(map[string]map[string]any, len(stored))
	for _, row := range stored {
		domain := strings.TrimSpace(stringMapValue(row, "domain"))
		if domain != "" {
			byDomain[domain] = row
		}
	}
	rows := make([]map[string]any, 0, len(domains)+len(stored))
	for _, domain := range domains {
		if row := byDomain[domain]; row != nil {
			rows = append(rows, row)
			delete(byDomain, domain)
			continue
		}
		rows = append(rows, map[string]any{
			"domain":     domain,
			"revision":   int64(0),
			"updated_at": "",
		})
	}
	for _, row := range stored {
		domain := strings.TrimSpace(stringMapValue(row, "domain"))
		if byDomain[domain] != nil {
			rows = append(rows, row)
			delete(byDomain, domain)
		}
	}
	return rows
}

func (s *graphjinService) revisionSignals(
	ctx context.Context,
	domain string,
	emitInitial bool,
	retryInterval time.Duration,
) <-chan revisionSignal {
	out := make(chan revisionSignal, 1)
	if ctx == nil {
		ctx = context.Background()
	}
	if retryInterval < watchRunnerMinInterval {
		retryInterval = watchRunnerMinInterval
	}
	domain = strings.TrimSpace(domain)
	if s == nil {
		close(out)
		return out
	}
	s.revisionSignalWG.Add(1)
	go func() {
		defer s.revisionSignalWG.Done()
		s.runRevisionSignals(ctx, domain, emitInitial, retryInterval, out)
	}()
	return out
}

func (s *graphjinService) runRevisionSignals(
	ctx context.Context,
	domain string,
	emitInitial bool,
	retryInterval time.Duration,
	out chan<- revisionSignal,
) {
	defer close(out)
	if s == nil || s.gj == nil || domain == "" {
		return
	}

	var lastRevision int64
	initialized := false
	for {
		member, err := s.subscribeRevisionSignal(ctx, domain)
		if err != nil {
			s.logRevisionSignalError(domain, "subscribe", err)
			current, readErr := s.artifactRevision(ctx, domain)
			if readErr != nil {
				s.logRevisionSignalError(domain, "fallback read", readErr)
			} else {
				initialized = publishRevisionSignal(
					out,
					revisionSignal{Domain: domain, Revision: current},
					emitInitial,
					initialized,
					&lastRevision,
				)
			}
			if !waitRevisionSignalRetry(ctx, retryInterval) {
				return
			}
			continue
		}

		closed := false
		for !closed {
			select {
			case <-ctx.Done():
				member.Unsubscribe()
				return
			case <-member.Done():
				closed = true
			case result, ok := <-member.Result:
				// Result is not closed today, but retain this guard so a future
				// core change cannot turn a closed result channel into a hot loop.
				if !ok {
					closed = true
					continue
				}
				signal, ok := decodeRevisionSignal(result, domain)
				if !ok {
					continue
				}
				initialized = publishRevisionSignal(
					out,
					signal,
					emitInitial,
					initialized,
					&lastRevision,
				)
			}
		}
		member.Unsubscribe()
		if !waitRevisionSignalRetry(ctx, retryInterval) {
			return
		}
	}
}

func (s *graphjinService) subscribeRevisionSignal(
	ctx context.Context,
	domain string,
) (*core.Member, error) {
	vars, err := json.Marshal(map[string]any{"domain": domain})
	if err != nil {
		return nil, err
	}
	return s.gj.Subscribe(
		s.internalStoreContext(ctx),
		`subscription graphjin_revision_signal($domain: String!) {
			gj_internal_revision(where: { domain: { eq: $domain } }) {
				domain
				revision
				updated_at
			}
		}`,
		vars,
		nil,
	)
}

func decodeRevisionSignal(result *core.Result, domain string) (revisionSignal, bool) {
	if result == nil || len(result.Errors) != 0 || len(result.Data) == 0 {
		return revisionSignal{}, false
	}
	var payload struct {
		Rows []struct {
			Domain   string `json:"domain"`
			Revision int64  `json:"revision"`
		} `json:"gj_internal_revision"`
	}
	if err := json.Unmarshal(result.Data, &payload); err != nil || len(payload.Rows) == 0 {
		return revisionSignal{}, false
	}
	row := payload.Rows[0]
	if row.Domain != domain {
		return revisionSignal{}, false
	}
	return revisionSignal{Domain: row.Domain, Revision: row.Revision}, true
}

func publishRevisionSignal(
	out chan<- revisionSignal,
	signal revisionSignal,
	emitInitial bool,
	initialized bool,
	lastRevision *int64,
) bool {
	if lastRevision == nil {
		return initialized
	}
	if !initialized {
		*lastRevision = signal.Revision
		if emitInitial {
			select {
			case out <- signal:
			default:
			}
		}
		return true
	}
	if signal.Revision == *lastRevision {
		return true
	}
	*lastRevision = signal.Revision
	select {
	case out <- signal:
	default:
	}
	return true
}

func waitRevisionSignalRetry(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *graphjinService) logRevisionSignalError(domain, action string, err error) {
	if s == nil || err == nil || s.log == nil {
		return
	}
	s.log.Warnf("revision signal %s: %s failed: %v", domain, action, err)
}

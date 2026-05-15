package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type managedMutationMux struct {
	handlers []ManagedMutationHandler
	byTable  map[string]ManagedMutationHandler
}

func combineManagedMutationHandlers(existing, next ManagedMutationHandler) (ManagedMutationHandler, error) {
	if existing == nil {
		return next, nil
	}
	if next == nil {
		return existing, nil
	}
	mux := &managedMutationMux{byTable: make(map[string]ManagedMutationHandler)}
	if err := mux.add(existing); err != nil {
		return nil, err
	}
	if err := mux.add(next); err != nil {
		return nil, err
	}
	return mux, nil
}

func (m *managedMutationMux) add(handler ManagedMutationHandler) error {
	if nested, ok := handler.(*managedMutationMux); ok {
		for _, h := range nested.handlers {
			if err := m.add(h); err != nil {
				return err
			}
		}
		return nil
	}
	for _, table := range handler.ManagedMutationTables() {
		key := strings.ToLower(table)
		if key == "" {
			return fmt.Errorf("managed mutation handler registered an empty table name")
		}
		if _, ok := m.byTable[key]; ok {
			return fmt.Errorf("managed mutation table %q registered by multiple handlers", table)
		}
		m.byTable[key] = handler
	}
	m.handlers = append(m.handlers, handler)
	return nil
}

func (m *managedMutationMux) ManagedMutationTables() []string {
	tables := make([]string, 0, len(m.byTable))
	for table := range m.byTable {
		tables = append(tables, table)
	}
	return tables
}

func (m *managedMutationMux) ExecuteManagedMutation(ctx context.Context, req ManagedMutationRequest) (json.RawMessage, error) {
	grouped := make(map[ManagedMutationHandler][]ManagedMutationRoot)
	for _, root := range req.Roots {
		handler := m.byTable[strings.ToLower(root.Table)]
		if handler == nil {
			return nil, fmt.Errorf("managed mutation table %q has no handler", root.Table)
		}
		grouped[handler] = append(grouped[handler], root)
	}

	out := make(map[string]json.RawMessage, len(req.Roots))
	for _, handler := range m.handlers {
		roots := grouped[handler]
		if len(roots) == 0 {
			continue
		}
		data, err := handler.ExecuteManagedMutation(ctx, ManagedMutationRequest{
			Database:  req.Database,
			Operation: req.Operation,
			Roots:     roots,
		})
		if err != nil {
			return nil, err
		}
		var partial map[string]json.RawMessage
		if err := json.Unmarshal(data, &partial); err != nil {
			return nil, fmt.Errorf("managed mutation handler returned invalid JSON object: %w", err)
		}
		for key, value := range partial {
			out[key] = value
		}
	}
	return json.Marshal(out)
}

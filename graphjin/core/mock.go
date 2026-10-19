package core

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func (s *gstate) executeMock(c context.Context) (err error) {
	if err = s.validateAndUpdateVars(c); err != nil {
		return
	}

	qc := s.cs.st.qc
	data := make(map[string]interface{})

	for _, id := range qc.Roots {
		sel := qc.Selects[id]
		val, err := s.generateMockValue(&sel, s.cs.st.qc)
		if err != nil {
			return err
		}
		data[sel.FieldName] = val
	}

	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	s.data = b

	return nil
}

func (s *gstate) generateMockValue(sel *qcode.Select, qc *qcode.QCode) (interface{}, error) {
	// If it's a list, generate a list of objects
	// For mutations, it might be singular even if not explicitly marked, but usually mutations return the modified object(s).
	// Let's rely on sel.Singular.
	if !sel.Singular {
		list := make([]interface{}, 0, 3) 
		// Generate variable number of items for realism, say 1 to 3
		count := 1 + rand.Intn(3)
		for i := 0; i < count; i++ {
			item, err := s.generateMockItem(sel, qc, i)
			if err != nil {
				return nil, err
			}
			list = append(list, item)
		}
		return list, nil
	} else {
		return s.generateMockItem(sel, qc, 0)
	}
}

func (s *gstate) generateMockItem(sel *qcode.Select, qc *qcode.QCode, idx int) (map[string]interface{}, error) {
	item := make(map[string]interface{})

	for _, f := range sel.Fields {
		// Skip if skipped
		if f.SkipRender != qcode.SkipTypeNone {
			continue
		}

		switch f.Type {
		case qcode.FieldTypeCol:
			val := s.mockColumnValue(f.Col, f.FieldName, idx)
			item[f.FieldName] = val
		case qcode.FieldTypeFunc:
			item[f.FieldName] = 42 
		}
	}

	// Also handle children (relationships)
	for _, childID := range sel.Children {
		childSel := qc.Selects[childID]
		
		// If SkipRender is set on the child select, skip it
		if childSel.SkipRender != qcode.SkipTypeNone {
			continue
		}

		val, err := s.generateMockValue(&childSel, qc)
		if err != nil {
			return nil, err
		}
		item[childSel.FieldName] = val
	}

	return item, nil
}

func (s *gstate) mockColumnValue(col sdata.DBColumn, name string, idx int) interface{} {
	if col.Array {
		return []interface{}{
			fmt.Sprintf("mock_%s_%d_a", name, idx+1),
			fmt.Sprintf("mock_%s_%d_b", name, idx+1),
		}
	}

	typeName := strings.ToLower(strings.TrimSpace(col.Type))

	switch typeName {
	case "integer", "int", "int2", "int4", "int8", "smallint", "bigint", "serial", "bigserial":
		return idx + 1
	case "numeric", "decimal", "real", "double precision", "float", "float4", "float8":
		return 12.34 + float64(idx)
	case "boolean", "bool":
		return idx%2 == 0
	case "json", "jsonb":
		return map[string]interface{}{"mock_key": "mock_value"}
	case "timestamp", "timestamp with time zone", "date", "timestamptz":
		return time.Now().UTC().Format(time.RFC3339)
	}

	if strings.Contains(typeName, "numeric") {
		return 12.34 + float64(idx)
	}
	if strings.Contains(typeName, "int") {
		return idx + 1
	}

	// Text, varchar, etc.
	return fmt.Sprintf("mock_%s_%d", name, idx+1)
}

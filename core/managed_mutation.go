package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// systemRootsMixedError refuses a mutation that names GraphJin system roots
// together with tables in another database.
//
// The two are written by different means, a service handler and SQL, and no
// transaction spans them. The system half used to be refused as "mutations
// blocked: database ... is read-only", which is true of SQL and false of the
// root: sent alone, the same write is accepted. A caller reading it took the
// write for forbidden rather than misshapen, so this names the roots and says
// what to do. It avoids the words the service reads as an access denial.
func (s *gstate) systemRootsMixedError(databases []string) error {
	var system, tables []string
	seen := make(map[string]struct{})
	for _, name := range databases {
		managed := make(map[string]struct{})
		if handler := s.gj.managedMutationHandlers[name]; handler != nil {
			for _, table := range handler.ManagedMutationTables() {
				managed[strings.ToLower(table)] = struct{}{}
			}
		}
		for _, root := range s.dbGroups[name] {
			if _, ok := seen[root]; ok {
				continue
			}
			seen[root] = struct{}{}
			if _, ok := managed[strings.ToLower(root)]; ok {
				system = append(system, root)
			} else {
				tables = append(tables, root)
			}
		}
	}
	if len(system) == 0 || len(tables) == 0 {
		return nil
	}
	return fmt.Errorf("GraphJin system roots (%s) cannot be written in the same mutation as database tables (%s); send them as separate mutations",
		strings.Join(system, ", "), strings.Join(tables, ", "))
}

func (s *gstate) executeManagedMutation(c context.Context) (bool, error) {
	if s.r.operation != qcode.QTMutation || s.cs == nil || s.cs.st.qc == nil {
		return false, nil
	}

	dbName := s.database
	if dbName == "" {
		dbName = s.gj.defaultDB
	}
	handler := s.gj.managedMutationHandlers[dbName]
	if handler == nil {
		return false, nil
	}

	managedTables := make(map[string]struct{})
	for _, table := range handler.ManagedMutationTables() {
		managedTables[strings.ToLower(table)] = struct{}{}
	}

	qc := s.cs.st.qc
	var managedRoots, normalRoots int
	for _, rootID := range qc.Roots {
		sel := qc.Selects[rootID]
		if _, ok := managedTables[strings.ToLower(sel.Ti.Name)]; ok {
			managedRoots++
		} else {
			normalRoots++
		}
	}

	if managedRoots == 0 {
		return false, nil
	}
	if normalRoots != 0 {
		return true, fmt.Errorf("managed mutations cannot be mixed with normal table mutations")
	}

	req := ManagedMutationRequest{
		Database:  dbName,
		Operation: mutationOperationName(qc.SType),
		Roots:     make([]ManagedMutationRoot, 0, len(qc.Roots)),
	}

	rootMutates := make(map[int32]*qcode.Mutate)
	for i := range qc.Mutates {
		m := &qc.Mutates[i]
		if m.ParentID == -1 {
			rootMutates[m.SelID] = m
		}
	}

	for _, rootID := range qc.Roots {
		sel := qc.Selects[rootID]
		root := ManagedMutationRoot{
			FieldName: sel.FieldName,
			Table:     sel.Ti.Name,
			Operation: req.Operation,
			Where:     managedFilterToValue(sel.Where.Exp, s.vmap),
			Fields:    make([]ManagedMutationField, 0, len(sel.Fields)),
		}
		for _, f := range sel.Fields {
			if f.Type != qcode.FieldTypeCol {
				continue
			}
			root.Fields = append(root.Fields, ManagedMutationField{
				Name:   f.FieldName,
				Column: f.Col.Name,
			})
		}
		if m := rootMutates[rootID]; m != nil && m.Data != nil {
			v, err := managedNodeToValue(m.Data, s.vmap)
			if err != nil {
				return true, err
			}
			if obj, ok := v.(map[string]interface{}); ok {
				root.Input = obj
			} else {
				return true, fmt.Errorf("managed mutation input for %s must be an object", root.FieldName)
			}
		}
		req.Roots = append(req.Roots, root)
	}

	data, err := handler.ExecuteManagedMutation(c, req)
	if err != nil {
		return true, err
	}
	s.data = data
	s.dhash = sha256.Sum256(s.data)
	s.data, err = encryptValues(s.data,
		s.gj.printFormat, decPrefix, s.dhash[:], s.gj.encryptionKey)
	return true, err
}

func mutationOperationName(qt qcode.QType) string {
	switch qt {
	case qcode.QTInsert:
		return "insert"
	case qcode.QTUpdate:
		return "update"
	case qcode.QTUpsert:
		return "upsert"
	case qcode.QTDelete:
		return "delete"
	case qcode.QTOpenAPICall:
		return "call"
	default:
		return qt.String()
	}
}

func managedNodeToValue(node *graph.Node, vars map[string]json.RawMessage) (interface{}, error) {
	if node == nil {
		return nil, nil
	}
	switch node.Type {
	case graph.NodeStr:
		return managedStringValue(node.Val), nil
	case graph.NodeLabel:
		if node.Val == "null" {
			return nil, nil
		}
		return managedStringValue(node.Val), nil
	case graph.NodeNum:
		if i, err := strconv.ParseInt(node.Val, 10, 64); err == nil {
			return i, nil
		}
		f, err := strconv.ParseFloat(node.Val, 64)
		if err != nil {
			return nil, err
		}
		return f, nil
	case graph.NodeBool:
		return strconv.ParseBool(node.Val)
	case graph.NodeVar:
		if vars == nil {
			return nil, fmt.Errorf("variable not found: %s", node.Val)
		}
		raw, ok := vars[node.Val]
		if !ok || len(raw) == 0 {
			return nil, fmt.Errorf("variable not found: %s", node.Val)
		}
		var v interface{}
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		return v, nil
	case graph.NodeObj:
		obj := make(map[string]interface{}, len(node.Children))
		for _, child := range node.Children {
			v, err := managedNodeToValue(child, vars)
			if err != nil {
				return nil, err
			}
			obj[child.Name] = v
		}
		return obj, nil
	case graph.NodeList:
		items := make([]interface{}, 0, len(node.Children))
		for _, child := range node.Children {
			v, err := managedNodeToValue(child, vars)
			if err != nil {
				return nil, err
			}
			items = append(items, v)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unsupported managed mutation input node: %s", node.Type.String())
	}
}

func managedStringValue(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	quoted := `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	if v, err := strconv.Unquote(quoted); err == nil {
		return v
	}
	return s
}

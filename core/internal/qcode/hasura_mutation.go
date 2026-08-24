package qcode

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// GraphJin already speaks most of the Hasura dialect on the read side: the
// operator parser strips a leading underscore so `_eq` and `eq` are the same
// token, and `<table>_aggregate` roots are lowered to native aggregate fields
// and reshaped back on the way out. Mutations were the hole, and they failed
// at the root: `update_support_tickets(where: …, _set: …)` is simply not a
// field GraphJin has.
//
// This closes it the same way the aggregate support does — lower the dialect
// into native form before ordinary compilation, and carry a plan that restores
// the Hasura response shape afterwards. Writes make the second rule in
// hasura_aggregate.go far more important than it is for reads: a mis-lowered
// `where` or `_set` writes the wrong columns to the wrong rows, so every shape
// this file does not implement is refused by name rather than approximated.

const (
	hasuraByPKSuffix = "_by_pk"
	// hasuraReturning wraps the rows a Hasura mutation selects; GraphJin
	// returns them directly under the root.
	hasuraReturning = "returning"
	// hasuraAffectedRows has no native equivalent — it is counted from the
	// returned rows when the response is reshaped.
	hasuraAffectedRows = "affected_rows"
)

// hasuraMutationVerbs maps a Hasura root prefix to the native GraphJin
// argument that performs the same write.
var hasuraMutationVerbs = map[string]string{
	"insert": "insert",
	"update": "update",
	"delete": "delete",
}

// hasuraMutationInputArgs maps a Hasura input argument to its native name. The
// singular `object` is Hasura's `insert_<table>_one` spelling and models emit
// it as often as the plural.
var hasuraMutationInputArgs = map[string]string{
	"objects": "insert",
	"object":  "insert",
	"_set":    "update",
}

// HasuraMutationRoot describes one lowered insert_/update_/delete_ root, and
// how its native response is restored to the shape the caller asked for.
type HasuraMutationRoot struct {
	ResponseKey string
	// Returning is true when the caller wrapped its selection in
	// `returning { … }`, so the rows must be nested back under that key.
	Returning bool
	// AffectedRows is true when the caller selected `affected_rows`, which is
	// synthesised from the number of rows the write returned.
	AffectedRows bool
	// Single is true for a `_by_pk` root, which returns one object rather
	// than a list.
	Single bool
}

// rewriteHasuraMutations lowers Hasura's mutation dialect into GraphJin's
// native form before ordinary qcode compilation.
func (co *Compiler) rewriteHasuraMutations(op *graph.Operation) ([]HasuraMutationRoot, error) {
	if op == nil || op.Type != graph.OpMutate {
		return nil, nil
	}

	var plans []HasuraMutationRoot
	for i := range op.Fields {
		root := &op.Fields[i]
		if root.ParentID != -1 {
			continue
		}
		verb, baseName, ok := splitHasuraMutationRoot(root.Name)
		if !ok {
			continue
		}

		// An exact schema object always wins. This keeps the compatibility
		// syntax from shadowing a real table named, for example,
		// update_requests.
		if _, err := co.Find("", co.ParseName(root.Name)); err == nil {
			continue
		}

		table, err := co.Find("", co.ParseName(baseName))
		if err != nil {
			return nil, co.unknownHasuraMutationRootError(root.Name, baseName, verb)
		}

		plan, err := co.rewriteHasuraMutationRoot(op, root, table, verb, baseName)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

// splitHasuraMutationRoot recognises insert_/update_/delete_<table> with an
// optional _by_pk suffix, and reports the native verb and the table name.
func splitHasuraMutationRoot(name string) (verb, baseName string, ok bool) {
	for prefix, native := range hasuraMutationVerbs {
		if !strings.HasPrefix(name, prefix+"_") {
			continue
		}
		baseName = strings.TrimPrefix(name, prefix+"_")
		baseName = strings.TrimSuffix(baseName, hasuraByPKSuffix)
		if baseName == "" {
			return "", "", false
		}
		return native, baseName, true
	}
	return "", "", false
}

func (co *Compiler) rewriteHasuraMutationRoot(
	op *graph.Operation, root *graph.Field, table sdata.DBTable, verb, baseName string,
) (HasuraMutationRoot, error) {
	requestedRoot := root.Name
	responseKey := requestedRoot
	if root.Alias != "" {
		responseKey = root.Alias
	}
	unsupported := func(detail string) error {
		return hasuraMutationSupportError(requestedRoot, baseName, verb, detail)
	}

	byPK := strings.HasSuffix(requestedRoot, hasuraByPKSuffix)
	plan := HasuraMutationRoot{ResponseKey: responseKey, Single: byPK}

	// --- arguments -------------------------------------------------------
	args := make([]graph.Arg, 0, len(root.Args)+1)
	seenNative := make(map[string]bool)
	sawInput := false
	for _, arg := range root.Args {
		switch {
		case arg.Name == "where":
			if byPK {
				return plan, unsupported("where and pk_columns cannot be combined; a _by_pk root is addressed by its primary key")
			}
			args = append(args, arg)

		case arg.Name == "pk_columns":
			if !byPK {
				return plan, unsupported("pk_columns is only valid on a _by_pk root; use where instead")
			}
			where, err := hasuraPKColumnsToWhere(arg.Val, table)
			if err != nil {
				return plan, unsupported(err.Error())
			}
			args = append(args, graph.Arg{Name: "where", Val: where})

		case arg.Name == "on_conflict":
			return plan, unsupported("on_conflict is not supported; use the native upsert argument")

		default:
			native, ok := hasuraMutationInputArgs[arg.Name]
			if !ok {
				if strings.HasPrefix(arg.Name, "_") {
					return plan, unsupported(fmt.Sprintf("argument %q is not supported; only _set is", arg.Name))
				}
				// Ordinary GraphJin arguments (limit, order_by, …) pass through.
				args = append(args, arg)
				continue
			}
			if native != verb {
				return plan, unsupported(fmt.Sprintf("argument %q does not belong on a %s root", arg.Name, verb))
			}
			if seenNative[native] {
				return plan, unsupported(fmt.Sprintf("%s input is supplied more than once", verb))
			}
			seenNative[native] = true
			sawInput = true
			args = append(args, graph.Arg{Name: native, Val: arg.Val})
		}
	}

	switch verb {
	case "insert", "update":
		if !sawInput {
			return plan, unsupported(fmt.Sprintf("a %s root requires %s", requestedRoot, hasuraInputArgName(verb)))
		}
	case "delete":
		// GraphJin spells the delete action as an argument rather than a root.
		args = append(args, graph.Arg{Name: "delete", Val: &graph.Node{Type: graph.NodeBool, Val: "true"}})
	}
	root.Args = args

	// --- selection -------------------------------------------------------
	nativeChildren := make([]int32, 0, len(root.Children))
	affectedRowsID := int32(-1)
	for _, childID := range root.Children {
		child := &op.Fields[childID]
		switch child.Name {
		case hasuraReturning:
			if plan.Returning {
				return plan, unsupported("returning may be selected only once")
			}
			if child.Alias != "" || len(child.Args) != 0 || len(child.Directives) != 0 {
				return plan, unsupported("an alias, arguments or directives on returning are not supported")
			}
			if len(child.Children) == 0 {
				return plan, unsupported("returning requires at least one column")
			}
			plan.Returning = true
			// Hoist the wrapped selection onto the root: GraphJin returns the
			// written rows directly, and the reshape re-nests them.
			for _, grandChildID := range child.Children {
				op.Fields[grandChildID].ParentID = root.ID
				nativeChildren = append(nativeChildren, grandChildID)
			}

		case hasuraAffectedRows:
			if len(child.Children) != 0 || len(child.Args) != 0 {
				return plan, unsupported("affected_rows must be a plain scalar selection")
			}
			plan.AffectedRows = true
			affectedRowsID = childID

		default:
			nativeChildren = append(nativeChildren, childID)
		}
	}
	if len(nativeChildren) == 0 {
		// A write that selects only affected_rows still has to return
		// something for the count to be taken from. Rename that field in place
		// rather than appending: growing op.Fields would move its backing
		// array and invalidate the *graph.Field pointers this rewrite holds.
		if affectedRowsID < 0 {
			return plan, unsupported("a selection is required")
		}
		column, err := hasuraCountColumn(table)
		if err != nil {
			return plan, unsupported("a selection is required")
		}
		counted := &op.Fields[affectedRowsID]
		counted.Name = column
		counted.Alias = ""
		counted.ParentID = root.ID
		nativeChildren = append(nativeChildren, affectedRowsID)
	}

	root.Name = baseName
	root.Alias = responseKey
	root.Children = nativeChildren
	return plan, nil
}

func hasuraInputArgName(verb string) string {
	if verb == "insert" {
		return "objects (or object)"
	}
	return "_set"
}

// hasuraPKColumnsToWhere turns `pk_columns: {id: 3}` into the equivalent
// `where: {id: {eq: 3}}`, which is how GraphJin addresses a single row.
func hasuraPKColumnsToWhere(pk *graph.Node, table sdata.DBTable) (*graph.Node, error) {
	if pk == nil || pk.Type != graph.NodeObj || len(pk.Children) == 0 {
		return nil, fmt.Errorf("pk_columns must be an object naming at least one primary key column")
	}
	where := &graph.Node{Type: graph.NodeObj, CMap: map[string]*graph.Node{}}
	for _, column := range pk.Children {
		if _, ok := table.ColumnExists(column.Name); !ok {
			return nil, fmt.Errorf("pk_columns names %q, which is not a column on %q", column.Name, table.Name)
		}
		if column.Type == graph.NodeObj || column.Type == graph.NodeList {
			return nil, fmt.Errorf("pk_columns value for %q must be a scalar", column.Name)
		}
		eq := &graph.Node{Type: column.Type, Name: "eq", Val: column.Val}
		filter := &graph.Node{
			Type:     graph.NodeObj,
			Name:     column.Name,
			Parent:   where,
			Children: []*graph.Node{eq},
			CMap:     map[string]*graph.Node{"eq": eq},
		}
		eq.Parent = filter
		where.Children = append(where.Children, filter)
		where.CMap[column.Name] = filter
	}
	return where, nil
}

func (co *Compiler) unknownHasuraMutationRootError(requestedRoot, baseName, verb string) error {
	want := strings.ToLower(co.ParseName(baseName))
	var suggestions []string
	seen := make(map[string]bool)
	for _, table := range co.s.GetTables() {
		name := strings.ToLower(table.Name)
		if want == "" || (!strings.Contains(name, want) && !strings.Contains(want, name) && !strings.HasSuffix(name, "_"+want)) {
			continue
		}
		suggestion := verb + "_" + table.Name
		if !seen[suggestion] {
			seen[suggestion] = true
			suggestions = append(suggestions, suggestion)
		}
	}
	sort.Strings(suggestions)
	if len(suggestions) > 3 {
		suggestions = suggestions[:3]
	}
	if len(suggestions) == 1 {
		return fmt.Errorf("unknown Hasura-compatible mutation root %q: table %q was not found; did you mean %q?", requestedRoot, baseName, suggestions[0])
	}
	if len(suggestions) > 1 {
		return fmt.Errorf("unknown Hasura-compatible mutation root %q: table %q was not found; did you mean one of %q?", requestedRoot, baseName, suggestions)
	}
	return fmt.Errorf("unknown Hasura-compatible mutation root %q: table %q was not found", requestedRoot, baseName)
}

func hasuraMutationSupportError(requestedRoot, baseName, verb, detail string) error {
	var supported, native string
	switch verb {
	case "insert":
		supported = fmt.Sprintf("%s(objects: {<column>: <value>}) { returning { <column> } affected_rows }", requestedRoot)
		native = fmt.Sprintf("%s(insert: {<column>: <value>}) { <column> }", baseName)
	case "update":
		supported = fmt.Sprintf("%s(where: {<column>: {_eq: <value>}}, _set: {<column>: <value>}) { returning { <column> } affected_rows }", requestedRoot)
		native = fmt.Sprintf("%s(where: {<column>: {eq: <value>}}, update: {<column>: <value>}) { <column> }", baseName)
	default:
		supported = fmt.Sprintf("%s(where: {<column>: {_eq: <value>}}) { returning { <column> } affected_rows }", requestedRoot)
		native = fmt.Sprintf("%s(delete: true, where: {<column>: {eq: <value>}}) { <column> }", baseName)
	}
	return fmt.Errorf("Hasura-compatible mutation root %q: %s. Supported form: %s. Native equivalent: %s",
		requestedRoot, detail, supported, native)
}

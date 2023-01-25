package qcode

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

type Schema struct {
	Type      string
	Version   int
	Schema    string
	Columns   []sdata.DBColumn
	Functions []sdata.DBFunction
}

func ParseSchema(b []byte) (ds Schema, err error) {
	var s graph.Schema
	s, err = graph.ParseSchema(b)
	if err != nil {
		return
	}
	ds.Type = s.Type
	ds.Schema = s.Schema

	if v, err1 := strconv.Atoi(s.Version); err == nil {
		ds.Version = v
	} else if s.Version != "" && err1 != nil {
		err = err1
		return
	}

	for _, t := range s.Types {
		var ti typeInfo

		ti, err = parseTypeDirectives(t.Directives)
		if err != nil {
			err = fmt.Errorf("%s: %w", t.Name, err)
			return
		}
		if ti.Schema == "" {
			ti.Schema = s.Schema
		}

		if ti.ReturnType != "" {
			df := sdata.DBFunction{
				Schema: ti.Schema,
				Name:   t.Name,
				Type:   ti.ReturnType,
			}
			if err = parseTFieldsFunction(&df, t.Fields); err != nil {
				break
			}
			ds.Functions = append(ds.Functions, df)

		} else {
			var cols []sdata.DBColumn
			cols, err = parseTFieldsColumns(ti.Schema, t.Name, t.Fields)
			if err != nil {
				break
			}
			ds.Columns = append(ds.Columns, cols...)
		}
		if err != nil {
			err = fmt.Errorf("%s: %w", t.Name, err)
		}
	}
	return
}

func parseTFieldsColumns(tableSchema, tableName string, fields []graph.TField) (
	cols []sdata.DBColumn, err error,
) {
	var dir tfieldInfo
	for i, f := range fields {
		dir, err = parseTFieldDirectives(f.Type, f.Directives)
		if err != nil {
			return
		}
		isRecursive := (dir.RelatedSchema == tableSchema &&
			dir.RelatedType == tableName)

		colType := pascalToSnakeSpace(f.Type)
		if dir.TypeSuffix != "" {
			colType += "(" + dir.TypeSuffix + ")"
		}

		col := sdata.DBColumn{
			ID:          int32(i),
			Schema:      tableSchema,
			Table:       tableName,
			Name:        f.Name,
			Type:        colType,
			Array:       f.List,
			NotNull:     f.Required,
			PrimaryKey:  dir.ID,
			UniqueKey:   dir.Unique,
			FullText:    dir.Search,
			Blocked:     dir.Blocked,
			FKeySchema:  dir.RelatedSchema,
			FKeyTable:   dir.RelatedType,
			FKeyCol:     dir.RelatedField,
			FKRecursive: isRecursive,
		}
		cols = append(cols, col)
	}
	return
}

func parseTFieldsFunction(fn *sdata.DBFunction, fields []graph.TField) (
	err error,
) {
	for i, f := range fields {
		var dir tfieldInfo
		dir, err = parseTFieldDirectives(f.Type, f.Directives)
		if err != nil {
			return
		}
		p := sdata.DBFuncParam{
			ID:   i,
			Name: f.Name,
			Type: pascalToSnakeSpace(f.Type),
		}
		switch {
		case dir.Input:
			fn.Inputs = append(fn.Inputs, p)
		case dir.Output:
			fn.Outputs = append(fn.Outputs, p)
		default:
			err = fmt.Errorf("%s: @input or @output directive required", p.Name)
			return
		}
	}
	return
}

type typeInfo struct {
	Schema     string
	ReturnType string
}

func parseTypeDirectives(dir []graph.Directive) (ti typeInfo, err error) {
	for _, d := range dir {
		var arg graph.Arg
		switch d.Name {
		case "schema":
			arg, err = getArg(d.Args, "name", graph.NodeStr, graph.NodeLabel)
			if err != nil {
				break
			}
			ti.Schema = arg.Val.Val

		case "function":
			arg, err = getArg(d.Args, "return_type", graph.NodeStr, graph.NodeLabel)
			if err != nil {
				break
			}
			ti.ReturnType = arg.Val.Val
		}
		if err != nil {
			err = fmt.Errorf("type: %w", err)
			return
		}
	}
	return
}

type tfieldInfo struct {
	ID            bool
	Unique        bool
	Search        bool
	Blocked       bool
	TypeSuffix    string
	RelatedType   string
	RelatedField  string
	RelatedSchema string
	Input         bool
	Output        bool
}

func parseTFieldDirectives(ft string, dir []graph.Directive) (tfi tfieldInfo, err error) {
	for _, d := range dir {
		var arg graph.Arg

		switch d.Name {
		case "id":
			tfi.ID = true

		case "unique":
			tfi.Unique = true

		case "search":
			tfi.Search = true

		case "blocked":
			tfi.Blocked = true

		case "type":
			arg, err = getArg(d.Args, "args", graph.NodeStr, graph.NodeLabel)
			if err != nil {
				break
			}
			tfi.TypeSuffix = arg.Val.Val

		case "relation":
			arg, err = getArg(d.Args, "type", graph.NodeStr, graph.NodeLabel)
			if err != nil {
				break
			}
			tfi.RelatedType = arg.Val.Val

			required := (ft != "Json")
			var ok bool

			if required {
				arg, err = getArg(d.Args, "field", graph.NodeStr, graph.NodeLabel)
			} else {
				arg, ok, err = getOptionalArg(d.Args, "field", graph.NodeStr, graph.NodeLabel)
			}
			if err != nil {
				break
			}
			if required || ok {
				tfi.RelatedField = arg.Val.Val
			}

			arg, ok, err = getOptionalArg(d.Args, "schema", graph.NodeStr, graph.NodeLabel)
			if err != nil {
				break
			}
			if ok {
				tfi.RelatedSchema = arg.Val.Val
			}

		case "input":
			tfi.Input = true

		case "output":
			tfi.Output = true

		default:
			err = fmt.Errorf("unknown schema field directive: %s", d.Name)
			return
		}

		if err != nil {
			err = fmt.Errorf("type field: %s: %w", d.Name, err)
			return
		}
	}
	return
}

func pascalToSnakeSpace(s string) string {
	var result string
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) {
			result += " "
		}
		result += strings.ToLower(string(r))
	}
	return result
}

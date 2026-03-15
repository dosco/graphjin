package serv

import (
	"fmt"
	"sort"
	"strings"
)

func optionWithTemplate(opt NextOption, template map[string]any) NextOption {
	opt.ArgsTemplate = template
	return opt
}

func mergeArgTemplates(base, override map[string]any) map[string]any {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}

	out := make(map[string]any, len(base)+len(override))
	for k, v := range base {
		out[k] = cloneTemplateValue(v)
	}
	for k, v := range override {
		out[k] = cloneTemplateValue(v)
	}
	return out
}

func cloneTemplateValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = cloneTemplateValue(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = cloneTemplateValue(val)
		}
		return out
	default:
		return x
	}
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if s, ok := args[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func carryArgs(template map[string]any, args map[string]any, keys ...string) map[string]any {
	out := mergeArgTemplates(nil, template)
	for _, key := range keys {
		if args == nil {
			continue
		}
		val, ok := args[key]
		if !ok || val == nil {
			continue
		}
		if s, ok := val.(string); ok && strings.TrimSpace(s) == "" {
			continue
		}
		if out == nil {
			out = make(map[string]any, len(keys))
		}
		out[key] = cloneTemplateValue(val)
	}
	return out
}

func (ms *mcpServer) enrichNextOptionTemplate(opt NextOption) map[string]any {
	base := ms.toolArgTemplate(opt.Tool, opt.RequiredArgs, opt.OptionalArgs)
	return mergeArgTemplates(base, opt.ArgsTemplate)
}

func (ms *mcpServer) toolArgTemplate(tool string, required, optional []string) map[string]any {
	if ms.srv == nil {
		return nil
	}

	st, ok := ms.srv.ListTools()[tool]
	if !ok {
		return nil
	}

	names := make([]string, 0, len(required)+len(optional))
	seen := make(map[string]struct{}, len(required)+len(optional))

	addName := func(name string) {
		if name == "" {
			return
		}
		if _, exists := seen[name]; exists {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	if len(required) == 0 && len(optional) == 0 {
		for _, name := range st.Tool.InputSchema.Required {
			addName(name)
		}
	} else {
		for _, name := range required {
			addName(name)
		}
		for _, name := range optional {
			addName(name)
		}
	}

	if len(names) == 0 {
		return nil
	}

	sort.Strings(names)
	out := make(map[string]any, len(names))
	for _, name := range names {
		schema, ok := st.Tool.InputSchema.Properties[name]
		if !ok {
			out[name] = "<" + name + ">"
			continue
		}
		out[name] = buildSchemaTemplateValue(name, schema)
	}
	return out
}

func buildSchemaTemplateValue(name string, schema any) any {
	m, ok := schema.(map[string]any)
	if !ok {
		return "<" + name + ">"
	}

	typeName, _ := m["type"].(string)
	switch typeName {
	case "string":
		return "<" + name + ">"
	case "number", "integer":
		return 0
	case "boolean":
		return false
	case "array":
		if itemSchema, ok := m["items"]; ok {
			return []any{buildSchemaTemplateValue(name+"_item", itemSchema)}
		}
		return []any{}
	case "object":
		if props, ok := m["properties"].(map[string]any); ok && len(props) > 0 {
			required, _ := m["required"].([]string)
			if len(required) == 0 {
				keys := make([]string, 0, len(props))
				for key := range props {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				limit := len(keys)
				if limit > 2 {
					limit = 2
				}
				required = keys[:limit]
			}

			out := make(map[string]any, len(required))
			for _, key := range required {
				out[key] = buildSchemaTemplateValue(key, props[key])
			}
			return out
		}
		if additional, ok := m["additionalProperties"]; ok {
			return map[string]any{"<key>": buildSchemaTemplateValue(name+"_value", additional)}
		}
		return map[string]any{}
	default:
		return "<" + name + ">"
	}
}

func queryDraftFromArgs(args map[string]any) string {
	table := stringArg(args, "table")
	if table == "" {
		table = "<table_name>"
	}

	fields := strings.TrimSpace(stringArg(args, "fields"))
	switch {
	case fields == "":
		fields = "id"
	case strings.EqualFold(fields, "all"):
		fields = "id name"
	default:
		fields = strings.ReplaceAll(fields, ",", " ")
		fields = strings.Join(strings.Fields(fields), " ")
	}

	pagination := "limit: 10"
	if strings.EqualFold(stringArg(args, "pagination"), "cursor") {
		pagination = "first: 10, after: $cursor"
	}

	var where string
	if strings.TrimSpace(stringArg(args, "filter_intent")) != "" {
		where = ", where: { /* validated filter */ }"
	}

	var relLines strings.Builder
	for _, rel := range strings.Split(stringArg(args, "relationships"), ",") {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		relLines.WriteString("\n    ")
		relLines.WriteString(rel)
		relLines.WriteString(" { id }")
	}

	return fmt.Sprintf("{\n  %s(%s%s) {\n    %s%s\n  }\n}", table, pagination, where, fields, relLines.String())
}

func mutationDraftFromArgs(args map[string]any) string {
	table := stringArg(args, "table")
	if table == "" {
		table = "<table_name>"
	}

	switch strings.ToLower(stringArg(args, "operation")) {
	case "update":
		return fmt.Sprintf("mutation {\n  %s(id: $id, update: {\n    /* fields */\n  }) {\n    id\n  }\n}", table)
	case "upsert":
		return fmt.Sprintf("mutation {\n  %s(upsert: {\n    id: $id\n    /* fields */\n  }) {\n    id\n  }\n}", table)
	case "delete":
		return fmt.Sprintf("mutation {\n  %s(delete: true, where: { id: { eq: $id } }) {\n    id\n  }\n}", table)
	default:
		return fmt.Sprintf("mutation {\n  %s(insert: {\n    /* fields */\n  }) {\n    id\n  }\n}", table)
	}
}

func (ms *mcpServer) nextForToolCall(tool string, args map[string]any, payload any) *NextGuidance {
	switch tool {
	case "get_query_syntax":
		return ms.newNextGuidance("query_syntax_ready", []NextOption{
			nextOption("list_tables", 1, "Start from the live schema surface before drafting a query.", "Discover available tables first.", nil, []string{"database"}),
			optionWithTemplate(
				nextOption("write_query", 2, "Use the guided query author instead of drafting raw DSL from scratch.", "Generate a starter query for a specific table.", []string{"table"}, []string{"fields", "relationships", "filter_intent", "pagination", "database"}),
				map[string]any{"table": "<table_name>", "fields": "id, name", "pagination": "limit"},
			),
			optionWithTemplate(
				nextOption("validate_where_clause", 3, "Validate filter objects before running a full query.", "Useful when you already know the target table and where clause.", []string{"table", "where"}, []string{"database"}),
				map[string]any{"table": "<table_name>", "where": map[string]any{"id": map[string]any{"eq": "<value>"}}},
			),
		})

	case "get_mutation_syntax":
		return ms.newNextGuidance("mutation_syntax_ready", []NextOption{
			optionWithTemplate(
				nextOption("describe_table", 1, "Inspect the target schema before building a mutation.", "Check settable columns and relationships.", []string{"table"}, []string{"database"}),
				map[string]any{"table": "<table_name>"},
			),
			optionWithTemplate(
				nextOption("write_mutation", 2, "Use the guided mutation author to produce an insert/update/upsert/delete skeleton.", "Generate a mutation draft tied to one table.", []string{"operation", "table"}, []string{"data_intent", "nested", "database"}),
				map[string]any{"operation": "insert", "table": "<table_name>"},
			),
		})

	case "get_js_runtime_api":
		return ms.newNextGuidance("workflow_runtime_ready", []NextOption{
			nextOption("list_workflows", 1, "Check whether a reusable workflow already exists.", "Reuse before authoring a new script.", nil, nil),
			optionWithTemplate(
				nextOption("save_workflow", 2, "Persist a reusable JS workflow once you know the runtime API.", "Author and save a workflow for repeated execution.", []string{"name", "description", "code"}, []string{"tags", "variables"}),
				map[string]any{
					"name":        "<workflow_name>",
					"description": "<what this workflow does>",
					"code":        "function main(input) {\n  return gj.tools.listTables();\n}\n",
				},
			),
			optionWithTemplate(
				nextOption("execute_workflow", 3, "Run an existing workflow with input variables.", "Execute a saved workflow by name.", []string{"name"}, []string{"variables", "namespace"}),
				map[string]any{"name": "<workflow_name>", "variables": map[string]any{}},
			),
		})

	case "write_query":
		return ms.newNextGuidance("query_guide_ready", []NextOption{
			optionWithTemplate(
				nextOption("validate_where_clause", 1, "Validate the filter object before executing the drafted query.", "Use when the query needs a where clause.", []string{"table", "where"}, []string{"database"}),
				carryArgs(map[string]any{
					"table": "<table_name>",
					"where": map[string]any{"id": map[string]any{"eq": "<value>"}},
				}, args, "table", "database"),
			),
			optionWithTemplate(
				nextOption("execute_graphql", 2, "Run the drafted query once the shape looks correct.", "Execute the GraphJin DSL against the configured database.", []string{"query"}, []string{"variables", "namespace"}),
				map[string]any{"query": queryDraftFromArgs(args)},
			),
			optionWithTemplate(
				nextOption("describe_table", 3, "Return to schema details if the draft needs more columns or relationships.", "Inspect the target table again.", []string{"table"}, []string{"database"}),
				carryArgs(map[string]any{"table": "<table_name>"}, args, "table", "database"),
			),
		})

	case "write_mutation":
		return ms.newNextGuidance("mutation_guide_ready", []NextOption{
			optionWithTemplate(
				nextOption("execute_graphql", 1, "Run the drafted mutation once the payload is ready.", "Execute the GraphJin mutation DSL.", []string{"query"}, []string{"variables", "namespace"}),
				map[string]any{"query": mutationDraftFromArgs(args)},
			),
			optionWithTemplate(
				nextOption("describe_table", 2, "Inspect the table again if required fields or relationships are unclear.", "Double-check settable columns.", []string{"table"}, []string{"database"}),
				carryArgs(map[string]any{"table": "<table_name>"}, args, "table", "database"),
			),
			nextOption("get_mutation_syntax", 3, "Return to the mutation reference if you need exact DSL semantics.", "Use the syntax reference for edge cases.", nil, nil),
		})

	case "fix_query_error":
		return ms.newNextGuidance("query_error_guided", []NextOption{
			optionWithTemplate(
				nextOption("execute_graphql", 1, "Retry execution after correcting the query.", "Run the revised query or mutation.", []string{"query"}, []string{"variables", "namespace"}),
				carryArgs(map[string]any{"query": stringArg(args, "query")}, args, "namespace"),
			),
			nextOption("get_query_syntax", 2, "Use the syntax reference to correct operator or structure issues.", "Helpful when the error points to DSL syntax.", nil, nil),
			optionWithTemplate(
				nextOption("describe_table", 3, "Inspect the schema again if the error mentions unknown columns or tables.", "Use when the problem is likely schema-related.", []string{"table"}, []string{"database"}),
				map[string]any{"table": "<table_name>"},
			),
		})

	default:
		return ms.nextForExistingToolCall(tool, args, payload)
	}
}

func (ms *mcpServer) nextForExistingToolCall(tool string, args map[string]any, payload any) *NextGuidance {
	switch tool {
	case "list_tables":
		return ms.newNextGuidance("tables_listed", []NextOption{
			optionWithTemplate(
				nextOption("describe_table", 1, "Inspect one table in detail before drafting queries.", "Choose a table from the list to see columns and relationships.", []string{"table"}, []string{"database"}),
				carryArgs(map[string]any{"table": "<table_name>"}, args, "database"),
			),
			optionWithTemplate(
				nextOption("write_query", 2, "Draft a starter query now that table names are known.", "Use guided query authoring for one table.", []string{"table"}, []string{"fields", "relationships", "filter_intent", "pagination", "database"}),
				carryArgs(map[string]any{"table": "<table_name>", "fields": "id, name", "pagination": "limit"}, args, "database"),
			),
			nextOption("list_saved_queries", 3, "Check for reusable allow-listed queries before writing a new one.", "Prefer saved queries when they already cover the use case.", nil, []string{"namespace"}),
		})

	case "describe_table":
		return ms.newNextGuidance("table_described", []NextOption{
			optionWithTemplate(
				nextOption("write_query", 1, "Turn the schema details into a starter query.", "Draft a query using the described table.", []string{"table"}, []string{"fields", "relationships", "filter_intent", "pagination", "database"}),
				carryArgs(map[string]any{"fields": "id, name", "pagination": "limit"}, args, "table", "database"),
			),
			optionWithTemplate(
				nextOption("validate_where_clause", 2, "Validate filters against the exact column types you just inspected.", "Use before running a complex query.", []string{"table", "where"}, []string{"database"}),
				carryArgs(map[string]any{"where": map[string]any{"id": map[string]any{"eq": "<value>"}}}, args, "table", "database"),
			),
			optionWithTemplate(
				nextOption("write_mutation", 3, "Draft a mutation against this table if the task is write-oriented.", "Generate an insert/update/upsert/delete starter.", []string{"operation", "table"}, []string{"data_intent", "nested", "database"}),
				carryArgs(map[string]any{"operation": "insert"}, args, "table", "database"),
			),
		})

	case "find_path":
		return ms.newNextGuidance("relationship_path_found", []NextOption{
			optionWithTemplate(
				nextOption("write_query", 1, "Use the discovered path to draft a nested query.", "Generate a query rooted at the source table.", []string{"table"}, []string{"relationships", "fields", "database"}),
				carryArgs(map[string]any{"table": "<from_table>", "relationships": stringArg(args, "to_table"), "fields": "id"}, map[string]any{
					"table":    stringArg(args, "from_table"),
					"database": stringArg(args, "database"),
				}, "table", "database"),
			),
			optionWithTemplate(
				nextOption("describe_table", 2, "Inspect the target table after finding the join path.", "Review fields and aggregations on the destination table.", []string{"table"}, []string{"database"}),
				carryArgs(map[string]any{"table": "<to_table>"}, map[string]any{
					"table":    stringArg(args, "to_table"),
					"database": stringArg(args, "database"),
				}, "table", "database"),
			),
		})

	case "validate_where_clause":
		result, _ := payload.(WhereValidationResult)
		if result.Valid {
			return ms.newNextGuidance("where_clause_valid", []NextOption{
				optionWithTemplate(
					nextOption("execute_graphql", 1, "The filter validates, so the next step is to run the full query.", "Execute a query that includes the validated where clause.", []string{"query"}, []string{"variables", "namespace"}),
					map[string]any{"query": queryDraftFromArgs(args)},
				),
				optionWithTemplate(
					nextOption("write_query", 2, "Turn the validated filter into a full query draft.", "Use guided authoring for the full selection set.", []string{"table"}, []string{"fields", "relationships", "filter_intent", "pagination", "database"}),
					carryArgs(map[string]any{"fields": "id, name", "pagination": "limit"}, args, "table", "database"),
				),
			})
		}
		return ms.newNextGuidance("where_clause_invalid", []NextOption{
			optionWithTemplate(
				nextOption("describe_table", 1, "Recheck column names and types to fix the invalid filter.", "Inspect the schema that validation used.", []string{"table"}, []string{"database"}),
				carryArgs(map[string]any{"table": "<table_name>"}, args, "table", "database"),
			),
			optionWithTemplate(
				nextOption("write_query", 2, "Use guided authoring if the filter problem reflects a broader query-shape issue.", "Draft the query again from schema context.", []string{"table"}, []string{"fields", "relationships", "filter_intent", "pagination", "database"}),
				carryArgs(map[string]any{"fields": "id, name", "pagination": "limit"}, args, "table", "database"),
			),
			nextOption("get_query_syntax", 3, "Return to the DSL reference for operator semantics.", "Use when the error is about operators or structure.", nil, nil),
		})

	case "get_workflow_guide":
		return ms.newNextGuidance("workflow_guide_ready", []NextOption{
			nextOption("list_tables", 1, "Begin schema discovery from the live database.", "This is the usual first step for new tasks.", nil, []string{"database"}),
			nextOption("list_saved_queries", 2, "Check for a reusable saved query before drafting a new one.", "Prefer allow-listed queries when possible.", nil, []string{"namespace"}),
			nextOption("list_workflows", 3, "Check for reusable JS workflows before authoring a new one.", "Useful for orchestration-heavy tasks.", nil, nil),
		})

	case "explore_relationships":
		return ms.newNextGuidance("relationship_graph_ready", []NextOption{
			optionWithTemplate(
				nextOption("describe_table", 1, "Inspect one table from the relationship graph in detail.", "Look deeper at columns and aggregations.", []string{"table"}, []string{"database"}),
				carryArgs(map[string]any{"table": "<table_name>"}, args, "database"),
			),
			optionWithTemplate(
				nextOption("find_path", 2, "Find an exact join path between two tables from the explored neighborhood.", "Use when you know the endpoints but need the nesting route.", []string{"from_table", "to_table"}, []string{"database"}),
				carryArgs(map[string]any{"from_table": stringArg(args, "table"), "to_table": "<related_table>"}, args, "database"),
			),
			optionWithTemplate(
				nextOption("write_query", 3, "Draft a query once you know the reachable tables.", "Generate a starter query from the explored graph.", []string{"table"}, []string{"relationships", "fields", "database"}),
				carryArgs(map[string]any{"table": "<table_name>", "relationships": "<related_table>", "fields": "id, name"}, args, "table", "database"),
			),
		})

	case "execute_graphql":
		result, _ := payload.(ExecuteResult)
		if len(result.Errors) > 0 {
			return ms.newNextGuidance("graphql_execution_error", []NextOption{
				optionWithTemplate(
					nextOption("fix_query_error", 1, "Use the guided repair tool with the exact failing query and error.", "Best next step after a failed execute_graphql call.", []string{"query", "error"}, nil),
					map[string]any{"query": stringArg(args, "query"), "error": result.Errors[0].Message},
				),
				optionWithTemplate(
					nextOption("describe_table", 2, "Inspect schema details if the error looks table- or column-related.", "Use when the failure mentions unknown fields or tables.", []string{"table"}, []string{"database"}),
					map[string]any{"table": "<table_name>"},
				),
				nextOption("get_query_syntax", 3, "Return to the DSL reference for operator or syntax fixes.", "Use when the failure points to query structure.", nil, nil),
			})
		}
		return ms.newNextGuidance("graphql_execution_success", []NextOption{
			optionWithTemplate(
				nextOption("explain_query", 1, "Inspect the compiled query if you want to optimize or verify the final plan.", "Helpful after a successful run when performance matters.", []string{"query"}, []string{"variables", "role"}),
				carryArgs(map[string]any{"query": stringArg(args, "query")}, args, "variables"),
			),
			optionWithTemplate(
				nextOption("save_workflow", 2, "Persist the successful pattern as a reusable workflow if this needs orchestration or repetition.", "Turn a working flow into a saved automation.", []string{"name", "description", "code"}, []string{"tags", "variables"}),
				map[string]any{
					"name":        "<workflow_name>",
					"description": "<workflow based on successful query>",
					"code":        "function main(input) {\n  return gj.tools.executeGraphql({query: " + fmt.Sprintf("%q", stringArg(args, "query")) + "});\n}\n",
				},
			),
		})

	case "execute_saved_query":
		result, _ := payload.(ExecuteResult)
		baseName := carryArgs(map[string]any{"name": "<saved_query_name>"}, args, "name", "namespace")
		if len(result.Errors) > 0 {
			return ms.newNextGuidance("saved_query_execution_error", []NextOption{
				optionWithTemplate(
					nextOption("get_saved_query", 1, "Inspect the saved query text and variable contract that just failed.", "Check the allow-listed query definition.", []string{"name"}, []string{"namespace"}),
					baseName,
				),
				nextOption("list_saved_queries", 2, "Search for a nearby saved query if this one is the wrong fit.", "Find alternate allow-listed queries.", nil, []string{"namespace"}),
			})
		}
		return ms.newNextGuidance("saved_query_execution_success", []NextOption{
			optionWithTemplate(
				nextOption("get_saved_query", 1, "Inspect the saved query definition and variable schema for repeat calls.", "Review the contract after a successful execution.", []string{"name"}, []string{"namespace"}),
				baseName,
			),
			optionWithTemplate(
				nextOption("execute_saved_query", 2, "Run the same saved query again with different variables.", "Useful for pagination or iterating over inputs.", []string{"name"}, []string{"variables", "namespace"}),
				baseName,
			),
		})

	case "execute_workflow":
		return ms.newNextGuidance("workflow_executed", []NextOption{
			nextOption("list_workflows", 1, "Review other reusable workflows after executing one successfully.", "Check whether adjacent workflows already exist.", nil, nil),
			nextOption("get_js_runtime_api", 2, "Inspect the runtime again if you need to extend or debug workflow code.", "Use before editing workflow scripts.", nil, nil),
		})

	case "list_workflows":
		return ms.newNextGuidance("workflows_listed", []NextOption{
			optionWithTemplate(
				nextOption("execute_workflow", 1, "Run one of the listed workflows by name.", "Execute a reusable workflow instead of authoring a new one.", []string{"name"}, []string{"variables", "namespace"}),
				map[string]any{"name": "<workflow_name>", "variables": map[string]any{}},
			),
			nextOption("get_js_runtime_api", 2, "Load the JS runtime contract before editing or creating scripts.", "See exactly which gj.tools.* calls are available.", nil, nil),
			optionWithTemplate(
				nextOption("save_workflow", 3, "Create a new workflow when no existing one fits.", "Persist a reusable JS script.", []string{"name", "description", "code"}, []string{"tags", "variables"}),
				map[string]any{
					"name":        "<workflow_name>",
					"description": "<what this workflow does>",
					"code":        "function main(input) {\n  return {};\n}\n",
				},
			),
		})

	case "save_workflow":
		return ms.newNextGuidance("workflow_saved", []NextOption{
			optionWithTemplate(
				nextOption("execute_workflow", 1, "Run the workflow you just saved.", "Verify the new workflow end to end.", []string{"name"}, []string{"variables", "namespace"}),
				carryArgs(map[string]any{"name": "<workflow_name>", "variables": map[string]any{}}, args, "name"),
			),
			nextOption("list_workflows", 2, "Refresh the workflow catalog after saving a new script.", "Confirm discoverability metadata.", nil, nil),
			nextOption("get_js_runtime_api", 3, "Revisit the runtime contract if the script needs more capabilities.", "Useful before a follow-up edit.", nil, nil),
		})

	case "list_saved_queries":
		return ms.newNextGuidance("saved_queries_listed", []NextOption{
			optionWithTemplate(
				nextOption("get_saved_query", 1, "Inspect the full query text and variable schema for one saved query.", "Choose a query from the list to inspect.", []string{"name"}, []string{"namespace"}),
				carryArgs(map[string]any{"name": "<saved_query_name>"}, args, "namespace"),
			),
			optionWithTemplate(
				nextOption("execute_saved_query", 2, "Run one of the listed saved queries.", "Prefer allow-listed execution when a matching query exists.", []string{"name"}, []string{"variables", "namespace"}),
				carryArgs(map[string]any{"name": "<saved_query_name>"}, args, "namespace"),
			),
			optionWithTemplate(
				nextOption("search_saved_queries", 3, "Narrow the list when you know the feature or table name.", "Search by fuzzy name match.", []string{"query"}, []string{"limit"}),
				map[string]any{"query": "<feature_or_table_name>", "limit": 10},
			),
		})

	case "search_saved_queries":
		return ms.newNextGuidance("saved_queries_found", []NextOption{
			optionWithTemplate(
				nextOption("get_saved_query", 1, "Inspect the best-matching saved query before execution.", "Review the query text and variable contract.", []string{"name"}, []string{"namespace"}),
				map[string]any{"name": "<saved_query_name>"},
			),
			optionWithTemplate(
				nextOption("execute_saved_query", 2, "Run one of the matched saved queries.", "Use saved execution instead of drafting a raw query.", []string{"name"}, []string{"variables", "namespace"}),
				map[string]any{"name": "<saved_query_name>"},
			),
		})

	case "get_saved_query":
		return ms.newNextGuidance("saved_query_loaded", []NextOption{
			optionWithTemplate(
				nextOption("execute_saved_query", 1, "Run the saved query now that you know its variable schema.", "Execute by name with the required variables.", []string{"name"}, []string{"variables", "namespace"}),
				carryArgs(map[string]any{"name": "<saved_query_name>", "variables": map[string]any{}}, args, "name", "namespace"),
			),
			optionWithTemplate(
				nextOption("write_query", 2, "Draft a variant when the saved query is close but not an exact fit.", "Use guided authoring to adapt the pattern.", []string{"table"}, []string{"fields", "relationships", "filter_intent", "pagination", "database"}),
				map[string]any{"table": "<table_name>", "fields": "id, name", "pagination": "limit"},
			),
		})

	case "list_fragments":
		return ms.newNextGuidance("fragments_listed", []NextOption{
			optionWithTemplate(
				nextOption("get_fragment", 1, "Inspect one fragment's definition and usage example.", "Choose a fragment from the list to review.", []string{"name"}, []string{"namespace"}),
				carryArgs(map[string]any{"name": "<fragment_name>"}, args, "namespace"),
			),
			optionWithTemplate(
				nextOption("write_query", 2, "Draft a query that can import one of these fragments.", "Use fragments to avoid repeating field selections.", []string{"table"}, []string{"fields", "relationships", "database"}),
				map[string]any{"table": "<table_name>", "fields": "id, name"},
			),
		})

	case "search_fragments":
		return ms.newNextGuidance("fragments_found", []NextOption{
			optionWithTemplate(
				nextOption("get_fragment", 1, "Inspect the best-matching fragment before using it.", "Review the import directive and usage example.", []string{"name"}, []string{"namespace"}),
				map[string]any{"name": "<fragment_name>"},
			),
			optionWithTemplate(
				nextOption("write_query", 2, "Draft a query that imports the matched fragment.", "Use guided authoring for the surrounding query.", []string{"table"}, []string{"fields", "relationships", "database"}),
				map[string]any{"table": "<table_name>", "fields": "id, name"},
			),
		})

	case "get_fragment":
		return ms.newNextGuidance("fragment_loaded", []NextOption{
			optionWithTemplate(
				nextOption("write_query", 1, "Draft a query that imports and uses this fragment.", "Build the surrounding query shape around the fragment.", []string{"table"}, []string{"fields", "relationships", "database"}),
				map[string]any{"table": "<table_name>", "fields": "id, name"},
			),
			optionWithTemplate(
				nextOption("execute_graphql", 2, "Run a query that imports the fragment once the full query text is ready.", "Execute the final query using the imported fragment.", []string{"query"}, []string{"variables", "namespace"}),
				map[string]any{"query": "{ <table_name>(limit: 10) { ...<FragmentName> } }"},
			),
		})

	case "get_current_config":
		return ms.newNextGuidance("config_loaded", []NextOption{
			nextOption("update_current_config", 1, "Use the config update tool to apply a focused change.", "Modify databases, roles, functions, or resolvers.", nil, []string{"databases", "tables", "roles", "functions", "resolvers", "create_if_not_exists"}),
			nextOption("get_onboarding_status", 2, "Check current readiness before deciding on config changes.", "Useful when evaluating whether schema reload or onboarding changes are needed.", nil, nil),
			nextOption("reload_schema", 3, "Refresh schema state after config changes that affect discovery.", "Use when the database changed outside the current process.", nil, nil),
		})

	case "reload_schema":
		return ms.newNextGuidance("schema_reloaded", []NextOption{
			nextOption("list_tables", 1, "Inspect the refreshed table list after reload.", "Confirm the new schema state.", nil, []string{"database"}),
			optionWithTemplate(
				nextOption("describe_table", 2, "Inspect a newly visible table in detail.", "Review columns, relationships, and aggregations.", []string{"table"}, []string{"database"}),
				carryArgs(map[string]any{"table": "<table_name>"}, args, "database"),
			),
		})

	case "preview_schema_changes":
		return ms.newNextGuidance("schema_preview_ready", []NextOption{
			optionWithTemplate(
				nextOption("apply_schema_changes", 1, "Apply the reviewed schema diff when the preview looks correct.", "Use the same schema payload to execute the change.", []string{"schema", "database"}, []string{"destructive"}),
				carryArgs(map[string]any{"schema": "<db.graphql schema>"}, args, "schema", "database", "destructive"),
			),
			nextOption("list_tables", 2, "Compare the current schema surface before applying changes.", "Inspect live tables before the DDL run.", nil, []string{"database"}),
		})

	case "apply_schema_changes":
		return ms.newNextGuidance("schema_applied", []NextOption{
			nextOption("list_tables", 1, "Inspect the updated schema surface after applying changes.", "Confirm that new tables are now queryable.", nil, []string{"database"}),
			optionWithTemplate(
				nextOption("describe_table", 2, "Inspect a newly created or changed table in detail.", "Review columns and relationships after the DDL apply.", []string{"table"}, []string{"database"}),
				carryArgs(map[string]any{"table": "<table_name>"}, args, "database"),
			),
			nextOption("reload_schema", 3, "Force a fresh schema load if you want to verify state explicitly.", "Useful after out-of-band schema changes.", nil, nil),
		})

	case "explain_query":
		return ms.newNextGuidance("query_explained", []NextOption{
			optionWithTemplate(
				nextOption("execute_graphql", 1, "Run the query now that the compiled form looks correct.", "Execute the exact query you just explained.", []string{"query"}, []string{"variables", "namespace"}),
				carryArgs(map[string]any{"query": stringArg(args, "query")}, args, "variables"),
			),
			optionWithTemplate(
				nextOption("fix_query_error", 2, "Use guided repair if the explanation surfaced an error or mismatch.", "Pair the original query with the compiler error.", []string{"query", "error"}, nil),
				map[string]any{"query": stringArg(args, "query"), "error": "<explain_or_compile_error>"},
			),
		})

	case "audit_role_permissions":
		return ms.newNextGuidance("role_permissions_audited", []NextOption{
			optionWithTemplate(
				nextOption("describe_table", 1, "Inspect a table alongside the permission matrix.", "Compare schema shape with role restrictions.", []string{"table"}, []string{"database"}),
				map[string]any{"table": "<table_name>"},
			),
			nextOption("get_current_config", 2, "Inspect current config before changing permission rules.", "Review the active roles and table policies.", nil, nil),
			nextOption("update_current_config", 3, "Apply a targeted permission change after reviewing the audit.", "Use config updates to adjust roles or table policies.", nil, []string{"roles"}),
		})

	case "list_databases":
		return ms.newNextGuidance("databases_listed", []NextOption{
			nextOption("get_onboarding_status", 1, "Check readiness after reviewing the configured database list.", "See whether GraphJin is already connected and schema-ready.", nil, nil),
			nextOption("update_current_config", 2, "Point GraphJin at a different database or alias after reviewing the list.", "Use this when the configured DB is not the one you want.", nil, []string{"databases"}),
			nextOption("list_tables", 3, "Inspect tables from the currently active database connection.", "Continue with schema exploration if readiness is already good.", nil, []string{"database"}),
		})

	case "check_health":
		result, _ := payload.(HealthResult)
		switch {
		case result.Status == "healthy" && result.SchemaReady:
			return ms.newNextGuidance("health_ok_schema_ready", []NextOption{
				nextOption("list_tables", 1, "Health is good and schema is ready, so continue with schema exploration.", "Start querying against the healthy database surface.", nil, []string{"database"}),
				nextOption("describe_table", 2, "Inspect a table in detail now that the connection is healthy.", "Review columns and relationships before querying.", []string{"table"}, []string{"database"}),
			})
		case result.Status == "healthy":
			return ms.newNextGuidance("health_ok_schema_not_ready", []NextOption{
				nextOption("reload_schema", 1, "The connection is healthy but schema is not ready yet.", "Force an immediate schema refresh.", nil, nil),
				nextOption("get_onboarding_status", 2, "Inspect onboarding readiness before changing config.", "Check what part of initialization is still missing.", nil, nil),
			})
		default:
			return ms.newNextGuidance("health_check_failed", []NextOption{
				nextOption("get_onboarding_status", 1, "Inspect readiness and configuration after a health failure.", "Check schema readiness and configured databases.", nil, nil),
				nextOption("discover_databases", 2, "Find or re-probe local databases if the current connection is broken.", "Use guided discovery to recover connectivity.", nil, []string{"targets", "user", "password"}),
			})
		}
	}

	return ms.newNextGuidance("continue", []NextOption{
		nextOption("get_workflow_guide", 1, "Use the workflow guide when the next best tool is unclear.", "This is the fallback planner for the MCP surface.", nil, nil),
	})
}

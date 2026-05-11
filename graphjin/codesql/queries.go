//go:build cgo

package codesql

func pack(kind, source string) queryPack {
	return queryPack{Kind: kind, Source: source}
}

func goQueries() []queryPack {
	return []queryPack{
		pack("codesql", `
(function_declaration name: (identifier) @symbol.name) @symbol.def @symbol.kind.function
(method_declaration receiver: (parameter_list)? name: (field_identifier) @symbol.name) @symbol.def @symbol.kind.method
(type_declaration (type_spec name: (type_identifier) @symbol.name type: (_) @symbol.def)) @symbol.kind.type
(const_declaration (const_spec name: (identifier) @symbol.name) @symbol.def) @symbol.kind.const
(var_declaration (var_spec name: (identifier) @symbol.name) @symbol.def) @symbol.kind.var
(import_spec path: (interpreted_string_literal) @import.path) @import.def
(import_spec name: (package_identifier) @import.alias path: (interpreted_string_literal) @import.path) @import.def
(call_expression function: (identifier) @ref.name) @ref.kind.call
(selector_expression field: (field_identifier) @ref.name) @ref.kind.selector
(comment) @doc
`),
		pack("tags", `
(function_declaration name: (identifier) @name) @definition.function
(method_declaration name: (field_identifier) @name) @definition.method
(type_spec name: (type_identifier) @name) @definition.type
(identifier) @reference
`),
		pack("locals", `
(block) @scope
(identifier) @local.name
`),
		pack("highlights", `
(comment) @comment
(interpreted_string_literal) @string
(raw_string_literal) @string
`),
	}
}

func pythonQueries() []queryPack {
	return []queryPack{
		pack("codesql", `
(function_definition name: (identifier) @symbol.name) @symbol.def @symbol.kind.function
(class_definition name: (identifier) @symbol.name) @symbol.def @symbol.kind.class
(import_statement name: (dotted_name) @import.path) @import.def
(import_from_statement module_name: (dotted_name) @import.path) @import.def
(call function: (identifier) @ref.name) @ref.kind.call
(attribute attribute: (identifier) @ref.name) @ref.kind.selector
(comment) @doc
`),
		pack("tags", `
(function_definition name: (identifier) @name) @definition.function
(class_definition name: (identifier) @name) @definition.class
(identifier) @reference
`),
		pack("locals", `
(block) @scope
(identifier) @local.name
`),
		pack("highlights", `
(comment) @comment
(string) @string
`),
	}
}

func javascriptQueries() []queryPack {
	return []queryPack{
		pack("codesql", `
(function_declaration name: (identifier) @symbol.name) @symbol.def @symbol.kind.function
(method_definition name: (property_identifier) @symbol.name) @symbol.def @symbol.kind.method
(class_declaration name: (identifier) @symbol.name) @symbol.def @symbol.kind.class
(lexical_declaration (variable_declarator name: (identifier) @symbol.name)) @symbol.def @symbol.kind.var
(variable_declaration (variable_declarator name: (identifier) @symbol.name)) @symbol.def @symbol.kind.var
(import_statement source: (string) @import.path) @import.def
(call_expression function: (identifier) @ref.name) @ref.kind.call
(member_expression property: (property_identifier) @ref.name) @ref.kind.selector
(comment) @doc
`),
		pack("tags", `
(function_declaration name: (identifier) @name) @definition.function
(class_declaration name: (identifier) @name) @definition.class
(identifier) @reference
`),
		pack("locals", `
(statement_block) @scope
(identifier) @local.name
`),
		pack("highlights", `
(comment) @comment
(string) @string
(template_string) @string
`),
	}
}

func typescriptQueries() []queryPack {
	return []queryPack{
		pack("codesql", `
(function_declaration name: (identifier) @symbol.name) @symbol.def @symbol.kind.function
(method_definition name: (property_identifier) @symbol.name) @symbol.def @symbol.kind.method
(class_declaration name: (type_identifier) @symbol.name) @symbol.def @symbol.kind.class
(interface_declaration name: (type_identifier) @symbol.name) @symbol.def @symbol.kind.interface
(type_alias_declaration name: (type_identifier) @symbol.name) @symbol.def @symbol.kind.type
(lexical_declaration (variable_declarator name: (identifier) @symbol.name)) @symbol.def @symbol.kind.var
(import_statement source: (string) @import.path) @import.def
(call_expression function: (identifier) @ref.name) @ref.kind.call
(member_expression property: (property_identifier) @ref.name) @ref.kind.selector
(comment) @doc
`),
		pack("tags", `
(function_declaration name: (identifier) @name) @definition.function
(class_declaration name: (type_identifier) @name) @definition.class
(interface_declaration name: (type_identifier) @name) @definition.interface
(identifier) @reference
`),
		pack("locals", `
(statement_block) @scope
(identifier) @local.name
`),
		pack("highlights", `
(comment) @comment
(string) @string
(template_string) @string
`),
	}
}

func rustQueries() []queryPack {
	return []queryPack{
		pack("codesql", `
(function_item name: (identifier) @symbol.name) @symbol.def @symbol.kind.function
(struct_item name: (type_identifier) @symbol.name) @symbol.def @symbol.kind.struct
(enum_item name: (type_identifier) @symbol.name) @symbol.def @symbol.kind.enum
(trait_item name: (type_identifier) @symbol.name) @symbol.def @symbol.kind.trait
(mod_item name: (identifier) @symbol.name) @symbol.def @symbol.kind.module
(use_declaration argument: (_) @import.path) @import.def
(call_expression function: (identifier) @ref.name) @ref.kind.call
(field_expression field: (field_identifier) @ref.name) @ref.kind.selector
(line_comment) @doc
(block_comment) @doc
`),
		pack("tags", `
(function_item name: (identifier) @name) @definition.function
(struct_item name: (type_identifier) @name) @definition.struct
(enum_item name: (type_identifier) @name) @definition.enum
(identifier) @reference
`),
		pack("locals", `
(block) @scope
(identifier) @local.name
`),
		pack("highlights", `
(line_comment) @comment
(block_comment) @comment
(string_literal) @string
`),
	}
}

func javaQueries() []queryPack {
	return []queryPack{
		pack("codesql", `
(class_declaration name: (identifier) @symbol.name) @symbol.def @symbol.kind.class
(interface_declaration name: (identifier) @symbol.name) @symbol.def @symbol.kind.interface
(method_declaration name: (identifier) @symbol.name) @symbol.def @symbol.kind.method
(constructor_declaration name: (identifier) @symbol.name) @symbol.def @symbol.kind.constructor
(import_declaration (scoped_identifier) @import.path) @import.def
(method_invocation name: (identifier) @ref.name) @ref.kind.call
(field_access field: (identifier) @ref.name) @ref.kind.selector
(line_comment) @doc
(block_comment) @doc
`),
		pack("tags", `
(class_declaration name: (identifier) @name) @definition.class
(method_declaration name: (identifier) @name) @definition.method
(identifier) @reference
`),
		pack("locals", `
(block) @scope
(identifier) @local.name
`),
		pack("highlights", `
(line_comment) @comment
(block_comment) @comment
(string_literal) @string
`),
	}
}

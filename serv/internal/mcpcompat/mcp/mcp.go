// Package mcp provides the small, GraphJin-owned registration surface used by
// the service. The wire protocol and transports are implemented by the official
// modelcontextprotocol/go-sdk; these types keep GraphJin's existing handlers
// independent from SDK representation churn.
package mcp

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/invopop/jsonschema"
)

type Meta struct {
	ProgressToken    any
	AdditionalFields map[string]any
}

func NewMetaFromMap(values map[string]any) *Meta {
	m := &Meta{AdditionalFields: map[string]any{}}
	for k, v := range values {
		if k == "progressToken" {
			m.ProgressToken = v
			continue
		}
		m.AdditionalFields[k] = v
	}
	return m
}

func (m *Meta) Map() map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m.AdditionalFields)+1)
	for k, v := range m.AdditionalFields {
		out[k] = v
	}
	if m.ProgressToken != nil {
		out["progressToken"] = m.ProgressToken
	}
	return out
}

type CallToolParams struct {
	Name      string
	Arguments any
	Meta      *Meta
}

type CallToolRequest struct {
	Params CallToolParams
}

func (r CallToolRequest) GetArguments() map[string]any {
	v, _ := r.Params.Arguments.(map[string]any)
	return v
}

func (r CallToolRequest) GetString(key, fallback string) string {
	if v, ok := r.GetArguments()[key].(string); ok {
		return v
	}
	return fallback
}

func (r CallToolRequest) RequireString(key string) (string, error) {
	v, ok := r.GetArguments()[key]
	if !ok {
		return "", fmt.Errorf("required argument %q not found", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q is not a string", key)
	}
	return s, nil
}

func (r CallToolRequest) GetInt(key string, fallback int) int {
	switch v := r.GetArguments()[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func (r CallToolRequest) GetBool(key string, fallback bool) bool {
	switch v := r.GetArguments()[key].(type) {
	case bool:
		return v
	case string:
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	case int:
		return v != 0
	case float64:
		return v != 0
	}
	return fallback
}

type Content interface{ isContent() }

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (TextContent) isContent() {}

func NewTextContent(text string) TextContent { return TextContent{Type: "text", Text: text} }

type CallToolResult struct {
	Content           []Content
	StructuredContent any
	IsError           bool
}

func NewToolResultText(text string) *CallToolResult {
	return &CallToolResult{Content: []Content{NewTextContent(text)}}
}

func NewToolResultStructured(structured any, fallbackText string) *CallToolResult {
	return &CallToolResult{Content: []Content{NewTextContent(fallbackText)}, StructuredContent: structured}
}

func NewToolResultError(text string) *CallToolResult {
	return &CallToolResult{Content: []Content{NewTextContent(text)}, IsError: true}
}

type ToolArgumentsSchema struct {
	Defs       map[string]any `json:"$defs,omitempty"`
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties,omitempty"`
	Required   []string       `json:"required,omitempty"`
}

type ToolInputSchema = ToolArgumentsSchema
type ToolOutputSchema = ToolArgumentsSchema

type ToolAnnotation struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

type Tool struct {
	Meta         *Meta            `json:"_meta,omitempty"`
	Name         string           `json:"name"`
	Description  string           `json:"description,omitempty"`
	InputSchema  ToolInputSchema  `json:"inputSchema"`
	OutputSchema ToolOutputSchema `json:"outputSchema,omitempty"`
	Annotations  ToolAnnotation   `json:"annotations,omitempty"`
}

type ToolOption func(*Tool)
type PropertyOption func(map[string]any)

func boolPtr(v bool) *bool { return &v }

func NewTool(name string, opts ...ToolOption) Tool {
	t := Tool{
		Name:        name,
		InputSchema: ToolInputSchema{Type: "object", Properties: map[string]any{}},
		Annotations: ToolAnnotation{
			ReadOnlyHint: boolPtr(false), DestructiveHint: boolPtr(true),
			IdempotentHint: boolPtr(false), OpenWorldHint: boolPtr(true),
		},
	}
	for _, opt := range opts {
		opt(&t)
	}
	return t
}

func WithDescription(description string) ToolOption {
	return func(t *Tool) { t.Description = description }
}

func Required() PropertyOption {
	return func(p map[string]any) { p["required"] = true }
}

func Description(description string) PropertyOption {
	return func(p map[string]any) { p["description"] = description }
}

func Enum(values ...string) PropertyOption {
	return func(p map[string]any) { p["enum"] = values }
}

func Min(value float64) PropertyOption {
	return func(p map[string]any) { p["minimum"] = value }
}

func Max(value float64) PropertyOption {
	return func(p map[string]any) { p["maximum"] = value }
}

func Items(items map[string]any) PropertyOption {
	return func(p map[string]any) { p["items"] = items }
}

func WithStringItems() PropertyOption {
	return Items(map[string]any{"type": "string"})
}

func WithNumberItems() PropertyOption {
	return Items(map[string]any{"type": "number"})
}

func withProperty(name, typ string, opts ...PropertyOption) ToolOption {
	return func(t *Tool) {
		prop := map[string]any{"type": typ}
		for _, opt := range opts {
			opt(prop)
		}
		if required, _ := prop["required"].(bool); required {
			delete(prop, "required")
			t.InputSchema.Required = append(t.InputSchema.Required, name)
		}
		if t.InputSchema.Properties == nil {
			t.InputSchema.Properties = map[string]any{}
		}
		t.InputSchema.Properties[name] = prop
	}
}

func WithString(name string, opts ...PropertyOption) ToolOption {
	return withProperty(name, "string", opts...)
}

func WithNumber(name string, opts ...PropertyOption) ToolOption {
	return withProperty(name, "number", opts...)
}

func WithBoolean(name string, opts ...PropertyOption) ToolOption {
	return withProperty(name, "boolean", opts...)
}

func WithObject(name string, opts ...PropertyOption) ToolOption {
	return withProperty(name, "object", opts...)
}

func WithArray(name string, opts ...PropertyOption) ToolOption {
	return withProperty(name, "array", opts...)
}

func WithOutputSchema[T any]() ToolOption {
	return func(t *Tool) {
		var zero T
		reflector := jsonschema.Reflector{
			DoNotReference: true, Anonymous: true, AllowAdditionalProperties: true,
		}
		schema := reflector.Reflect(zero)
		schema.Version = ""
		data, err := json.Marshal(schema)
		if err != nil {
			return
		}
		var out ToolOutputSchema
		if json.Unmarshal(data, &out) == nil {
			if out.Type == "" && reflect.TypeOf(zero) != nil {
				out.Type = "object"
			}
			t.OutputSchema = out
		}
	}
}

type ReadResourceParams struct{ URI string }
type ReadResourceRequest struct{ Params ReadResourceParams }

type ResourceContents interface{ isResourceContents() }

type TextResourceContents struct {
	URI      string
	MIMEType string
	Text     string
}

func (TextResourceContents) isResourceContents() {}

type Resource struct {
	URI, Name, Description, MIMEType string
}

type ResourceOption func(*Resource)

func NewResource(uri, name string, opts ...ResourceOption) Resource {
	r := Resource{URI: uri, Name: name}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func WithResourceDescription(value string) ResourceOption {
	return func(r *Resource) { r.Description = value }
}

func WithMIMEType(value string) ResourceOption {
	return func(r *Resource) { r.MIMEType = value }
}

type ResourceTemplate struct {
	URITemplate, Name, Description, MIMEType string
}

type ResourceTemplateOption func(*ResourceTemplate)

func NewResourceTemplate(uri, name string, opts ...ResourceTemplateOption) ResourceTemplate {
	r := ResourceTemplate{URITemplate: uri, Name: name}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func WithTemplateDescription(value string) ResourceTemplateOption {
	return func(r *ResourceTemplate) { r.Description = value }
}

func WithTemplateMIMEType(value string) ResourceTemplateOption {
	return func(r *ResourceTemplate) { r.MIMEType = value }
}

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type PromptMessage struct {
	Role    Role
	Content Content
}

func NewPromptMessage(role Role, content Content) PromptMessage {
	return PromptMessage{Role: role, Content: content}
}

type GetPromptParams struct {
	Name      string
	Arguments map[string]string
}

type GetPromptRequest struct{ Params GetPromptParams }

type GetPromptResult struct {
	Description string
	Messages    []PromptMessage
}

func NewGetPromptResult(description string, messages []PromptMessage) *GetPromptResult {
	return &GetPromptResult{Description: description, Messages: messages}
}

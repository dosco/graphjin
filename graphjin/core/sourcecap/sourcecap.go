package sourcecap

import (
	"fmt"
	"sort"
	"strings"
)

const (
	ModeDev     = "dev"
	ModeProd    = "prod"
	ModeAgentic = "agentic"
)

const (
	KindDatabase = "database"
	KindCode     = "code"
	KindFile     = "file"
	KindAPI      = "api"
)

const (
	ActionRead    = "read"
	ActionWrite   = "write"
	ActionDelete  = "delete"
	ActionWatch   = "watch"
	ActionExecute = "execute"
	ActionReload  = "reload"
	ActionQuery   = "query"
	ActionMutate  = "mutate"
	ActionUse     = "use"
)

const (
	EnforcementRuntime                   = "runtime"
	EnforcementRuntimeCoarseReadOnly     = "runtime_coarse_read_only"
	EnforcementExistingPolicy            = "existing_policy"
	EnforcementExistingReadOnlyAndPolicy = "existing_read_only_and_policy"
	EnforcementConfigAudit               = "config_audit"
)

const (
	KeyDataRead        = "data.read"
	KeyDataWrite       = "data.write"
	KeySchemaRead      = "schema.read"
	KeySchemaWrite     = "schema.write"
	KeyCodeSearch      = "code.search"
	KeyCodeRead        = "code.read"
	KeyCodeWrite       = "code.write"
	KeyCodeWatch       = "code.watch"
	KeyCodeInferDBRefs = "code.infer_db_refs"
	KeyFilesList       = "files.list"
	KeyFilesRead       = "files.read"
	KeyFilesWrite      = "files.write"
	KeyFilesDelete     = "files.delete"
	KeyFilesWatch      = "files.watch"
	KeyAPIRead         = "api.read"
	KeyAPIWrite        = "api.write"
)

// Definition is the source of truth for a public sources[].capabilities key.
type Definition struct {
	Kind           string
	Key            string
	Action         string
	Summary        string
	Reason         string
	Recommendation string
	Severity       string
	Enforcement    string
	ReadOnlyBlocks bool
	DefaultDev     bool
	DefaultProd    bool
	DefaultAgentic bool
	MCPFlag        string
	ExampleValue   string
}

// Default returns the secure default for a deployment mode.
func (d Definition) Default(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ModeDev:
		return d.DefaultDev
	case ModeAgentic:
		return d.DefaultAgentic
	default:
		return d.DefaultProd
	}
}

var kindOrder = []string{KindDatabase, KindCode, KindFile, KindAPI}

var definitions = []Definition{
	def(KindDatabase, KeyDataRead, ActionRead, true, true, true, "medium", EnforcementExistingPolicy, false, "Read application database data.", kindReason(KindDatabase), readRecommendation),
	def(KindDatabase, KeyDataWrite, ActionWrite, true, true, true, "high", EnforcementExistingReadOnlyAndPolicy, true, "Write application database data.", kindReason(KindDatabase), mutateRecommendation),
	def(KindDatabase, KeySchemaRead, ActionRead, true, true, true, "medium", EnforcementExistingPolicy, false, "Read application database schema metadata.", kindReason(KindDatabase), readRecommendation),
	def(KindDatabase, KeySchemaWrite, ActionWrite, true, false, false, "critical", EnforcementExistingReadOnlyAndPolicy, true, "Write application database schema.", kindReason(KindDatabase), mutateRecommendation),

	def(KindCode, KeyCodeSearch, ActionRead, true, true, true, "medium", EnforcementConfigAudit, false, "Search indexed code source metadata.", kindReason(KindCode), readRecommendation),
	def(KindCode, KeyCodeRead, ActionRead, true, true, true, "medium", EnforcementConfigAudit, false, "Read indexed code source contents.", kindReason(KindCode), readRecommendation),
	def(KindCode, KeyCodeWrite, ActionWrite, true, false, false, "high", EnforcementRuntime, true, "Write code source contents.", kindReason(KindCode), mutateRecommendation),
	def(KindCode, KeyCodeWatch, ActionWatch, true, false, false, "medium", EnforcementRuntime, true, "Watch code source changes.", kindReason(KindCode), mutateRecommendation),
	def(KindCode, KeyCodeInferDBRefs, ActionUse, true, true, true, "medium", EnforcementRuntime, false, "Infer database references from code sources.", kindReason(KindCode), readRecommendation),

	def(KindFile, KeyFilesList, ActionRead, true, true, true, "medium", EnforcementConfigAudit, false, "List file source objects.", kindReason(KindFile), readRecommendation),
	def(KindFile, KeyFilesRead, ActionRead, true, true, true, "medium", EnforcementConfigAudit, false, "Read file source objects.", kindReason(KindFile), readRecommendation),
	def(KindFile, KeyFilesWrite, ActionWrite, true, false, false, "high", EnforcementRuntimeCoarseReadOnly, true, "Write file source objects.", kindReason(KindFile), mutateRecommendation),
	def(KindFile, KeyFilesDelete, ActionDelete, true, false, false, "critical", EnforcementRuntimeCoarseReadOnly, true, "Delete file source objects.", kindReason(KindFile), mutateRecommendation),
	def(KindFile, KeyFilesWatch, ActionWatch, true, false, false, "medium", EnforcementRuntimeCoarseReadOnly, true, "Watch file source changes.", kindReason(KindFile), mutateRecommendation),

	def(KindAPI, KeyAPIRead, ActionRead, true, true, true, "medium", EnforcementConfigAudit, false, "Call read operations on remote API sources.", kindReason(KindAPI), readRecommendation),
	def(KindAPI, KeyAPIWrite, ActionWrite, true, false, false, "high", EnforcementConfigAudit, true, "Call mutating operations on remote API sources.", kindReason(KindAPI), mutateRecommendation),
}

var byKind map[string][]Definition
var byKindKey map[string]Definition

func init() {
	byKind = make(map[string][]Definition)
	byKindKey = make(map[string]Definition)
	for _, definition := range definitions {
		byKind[definition.Kind] = append(byKind[definition.Kind], definition)
		byKindKey[definition.Kind+"\x00"+definition.Key] = definition
	}
}

func def(kind, key, action string, dev, prod, agentic bool, severity, enforcement string, readOnlyBlocks bool, summary, reason, recommendation string, opts ...func(*Definition)) Definition {
	d := Definition{
		Kind:           kind,
		Key:            key,
		Action:         action,
		DefaultDev:     dev,
		DefaultProd:    prod,
		DefaultAgentic: agentic,
		Severity:       severity,
		Enforcement:    enforcement,
		ReadOnlyBlocks: readOnlyBlocks,
		Summary:        summary,
		Reason:         reason,
		Recommendation: recommendation,
	}
	for _, opt := range opts {
		opt(&d)
	}
	switch key {
	case KeyCodeRead, KeyFilesRead, KeyAPIRead, KeyDataRead, KeyDataWrite:
		d.ExampleValue = "true"
	case KeyCodeWrite, KeyFilesWrite, KeyAPIWrite:
		d.ExampleValue = "false"
	}
	return d
}

const readRecommendation = "Set this capability to false unless authenticated users need this read surface."
const mutateRecommendation = "Set this capability to false or mark the source read_only unless authenticated users need this mutating surface."

func kindReason(kind string) string {
	switch kind {
	case KindCode:
		return "Code sources may expose repository contents or permit source mutation."
	case KindFile:
		return "File sources may expose or mutate filesystem/object storage contents."
	case KindAPI:
		return "API sources may call remote API operations with GraphJin-held credentials."
	default:
		return "Source capabilities control authenticated user access to this source surface."
	}
}

// Kinds returns the canonical public source kinds in stable order.
func Kinds() []string {
	return append([]string(nil), kindOrder...)
}

// CanonicalKind normalizes a public sources[].kind value.
func CanonicalKind(kind string) (string, error) {
	k := strings.ToLower(strings.TrimSpace(kind))
	switch k {
	case KindDatabase, KindCode, KindFile, KindAPI:
		return k, nil
	case "sql":
		return "", fmt.Errorf("unsupported kind %q; use kind: database", kind)
	case "codesql":
		return "", fmt.Errorf("unsupported kind %q; use kind: code", kind)
	case "filesystem", "files":
		return "", fmt.Errorf("unsupported kind %q; use kind: file", kind)
	case "openapi":
		return "", fmt.Errorf("unsupported kind %q; use kind: api", kind)
	case "graphjin":
		return "", fmt.Errorf("unsupported kind %q; GraphJin system features moved to top-level system configuration", kind)
	case "workflow", "workflows":
		return "", fmt.Errorf("unsupported kind %q; workflows moved to top-level workflows configuration", kind)
	case "":
		return "", fmt.Errorf("kind is required (supported: %s)", strings.Join(kindOrder, ", "))
	default:
		return "", fmt.Errorf("unsupported kind %q (supported: %s)", kind, strings.Join(kindOrder, ", "))
	}
}

// Definitions returns the capability definitions for a source kind.
func Definitions(kind string) []Definition {
	k, err := CanonicalKind(kind)
	if err != nil {
		return nil
	}
	return append([]Definition(nil), byKind[k]...)
}

// Lookup returns the definition for a source kind and capability key.
func Lookup(kind, key string) (Definition, bool) {
	k, err := CanonicalKind(kind)
	if err != nil {
		return Definition{}, false
	}
	d, ok := byKindKey[k+"\x00"+strings.TrimSpace(key)]
	return d, ok
}

// ValidKeys returns valid capability keys for a source kind in stable order.
func ValidKeys(kind string) []string {
	defs := Definitions(kind)
	keys := make([]string, 0, len(defs))
	for _, d := range defs {
		keys = append(keys, d.Key)
	}
	return keys
}

// CapabilityMap returns a stable map from source kind to capability keys.
func CapabilityMap() map[string][]string {
	out := make(map[string][]string, len(kindOrder))
	for _, kind := range kindOrder {
		out[kind] = ValidKeys(kind)
	}
	return out
}

// Examples returns source capability examples for a configured source name.
func Examples(kind, sourceName string) []string {
	if strings.TrimSpace(sourceName) == "" {
		sourceName = "<source>"
	}
	defs := Definitions(kind)
	examples := make([]string, 0, len(defs))
	for _, d := range defs {
		if d.ExampleValue == "" {
			continue
		}
		examples = append(examples, fmt.Sprintf("sources[%s].capabilities.%s: %s", sourceName, d.Key, d.ExampleValue))
	}
	sort.Strings(examples)
	return examples
}

// ValidKeyList returns a human-readable valid key list for errors.
func ValidKeyList(kind string) string {
	keys := ValidKeys(kind)
	if len(keys) == 0 {
		return ""
	}
	return strings.Join(keys, ", ")
}

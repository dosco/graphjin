package serv

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// devConfigTemplate / prodConfigTemplate / agenticConfigTemplate are the
// heavily-commented example configs that `graphjin serve new` writes for new apps. They double as the
// canonical reference for what a GraphJin config.yml looks like: every option
// is documented inline with a comment explaining what it does and what the
// reasonable defaults are. We expose them via the `get_config_docs` MCP tool
// so an LLM (or a human) can read them without having to find the template
// files inside the repo.
//
//go:embed config_docs/dev.yml
var devConfigTemplate string

//go:embed config_docs/prod.yml
var prodConfigTemplate string

//go:embed config_docs/agentic.yml
var agenticConfigTemplate string

// registerConfigDocsTool registers the `get_config_docs` MCP tool. Unlike
// `get_current_config`, this is *static documentation* — it's safe to expose
// in production because it never reveals anything about the running server's
// configuration (no secrets, no DB names, no schemas — just example YAML
// with explanatory comments).
// registerConfigDocsTool is retained as an inert compatibility hook.
func (ms *mcpServer) registerConfigDocsTool() {
	return

	ms.srv.AddTool(mcp.NewTool(
		"get_config_docs",
		mcp.WithDescription(
			"Return annotated example GraphJin configuration files (dev.yml / prod.yml / agentic.yml) "+
				"with inline comments explaining every option. Use this as a reference when "+
				"authoring or modifying a GraphJin config — pair with `get_current_config` "+
				"(dev mode) to see the actual running configuration."),
		mcp.WithString("variant",
			mcp.Description("Which template to return: 'dev' (annotated, full), 'prod' (production), 'agentic' (agent deployment), or 'both'/'all' (default)."),
		),
	), ms.handleGetConfigDocs)
}

func (ms *mcpServer) handleGetConfigDocs(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	variant, _ := args["variant"].(string)
	if variant == "" {
		variant = "both"
	}
	variant = strings.ToLower(variant)

	type docs struct {
		Variant     string `json:"variant"`
		Notes       string `json:"notes"`
		DevYAML     string `json:"dev_yml,omitempty"`
		ProdYAML    string `json:"prod_yml,omitempty"`
		AgenticYAML string `json:"agentic_yml,omitempty"`
	}
	out := docs{
		Variant: variant,
		Notes: "These are the same templates that `graphjin serve new` writes when scaffolding a new app. " +
			"Strings like `{{ .AppName }}` are Go template placeholders — substitute your real values when authoring a config. " +
			"`dev.yml` is the verbose annotated form; `prod.yml` is the production counterpart; `agentic.yml` is production-oriented with agentic source capability policy. " +
			"GraphJin reads YAML keys via mapstructure, so any documented field maps 1:1 to the underlying Config struct.",
	}

	switch variant {
	case "dev":
		out.DevYAML = devConfigTemplate
	case "prod":
		out.ProdYAML = prodConfigTemplate
	case "agent", "agentic":
		out.AgenticYAML = agenticConfigTemplate
	case "both", "all":
		out.DevYAML = devConfigTemplate
		out.ProdYAML = prodConfigTemplate
		out.AgenticYAML = agenticConfigTemplate
	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown variant: %s. Valid: dev, prod, agentic, both", variant)), nil
	}

	return ms.toolResultJSON("get_config_docs", args, out)
}

# GraphJin MCP Claude Plugin

This plugin registers a GraphJin MCP server in Claude Code:

- command: `graphjin`
- args: `mcp --path ${GRAPHJIN_CONFIG_PATH:-./config}`

## Notes

- `GRAPHJIN_CONFIG_PATH` is optional. Default is `./config`.
- `graphjin` must be available in your `PATH`.

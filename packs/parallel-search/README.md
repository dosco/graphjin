# Parallel web search pack

This pack searches the open web through Parallel's Search REST API, as a
GraphJin API source pinned to `api.parallel.ai`. It replaces the
`@open-neko/plugin-parallel-search` plugin, which called Parallel's Search MCP
server from plugin code. No plugin code runs inside OpenNeko.

## Configure

1. Create an API key at Parallel.
2. Open Admin, Settings, Packs and install Parallel web search. Paste the key.
   GraphJin seals it in its keystore and sends it as `x-api-key`.
3. Review and apply the configuration.
4. Enable **Parallel web search**. It installs disabled. Enabled, it runs
   searches without approval: they read the web and change nothing.

## Coverage

| Plugin action | Pack equivalent |
|---|---|
| `web_search` | `parallel_search.web_search`, operation `search` |
| `web_fetch` | Not available. See below. |

Parallel's Search API takes POST, so search is a reviewed action rather than a
GraphJin read, and the pack has no saved query.

The plugin's `web_fetch` called Parallel's Extract API. That API needs a
`parallel-beta` request header with a fixed value, and a pack cannot set a
fixed header: GraphJin source specs have no static headers, and a pack action
sends only path, query, body, and its idempotency header. The skill reads a
known page with a domain-restricted search instead.

The plugin's anonymous free tier came from the MCP server. The REST API needs
an API key.

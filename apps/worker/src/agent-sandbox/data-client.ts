import { chmod, mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";

// Stdlib-only, so it runs under any python3 in the sandbox.
export const NEKO_DATA_PY = String.raw`"""Query OpenNeko data from a skill script.

The run serves the agent's neko_graphjin MCP tools on a local Unix socket.
Calls run under the same identity, policy and audit as the agent's own calls.

    from neko_data import query
    rows = query("query { orders(limit: 10) { id } }")["orders"]
"""
import http.client
import itertools
import json
import os
import socket

__all__ = ["NekoDataError", "call_tool", "query"]

_ids = itertools.count(1)

# Written by the run at install time. Tools that scrub the environment, such
# as sandboxed code runners, still reach the run's socket through this default.
_DEFAULT_SOCKET = __NEKO_DATA_SOCKET__


class NekoDataError(RuntimeError):
    pass


class _UnixConnection(http.client.HTTPConnection):
    def __init__(self, path, timeout):
        super().__init__("localhost", timeout=timeout)
        self._path = path

    def connect(self):
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.settimeout(self.timeout)
        sock.connect(self._path)
        self.sock = sock


def _socket_path():
    path = os.environ.get("OPENNEKO_DATA_SOCKET") or _DEFAULT_SOCKET
    if not path:
        raise NekoDataError("this run has no data socket, so it has no data access")
    return path


def _text(result):
    for item in result.get("content") or []:
        if item.get("type") == "text":
            return item.get("text") or ""
    return ""


def call_tool(name, arguments=None, timeout=300):
    """Call one neko_graphjin MCP tool and return the raw MCP result."""
    body = json.dumps({
        "jsonrpc": "2.0",
        "id": next(_ids),
        "method": "tools/call",
        "params": {"name": name, "arguments": arguments or {}},
    })
    conn = _UnixConnection(_socket_path(), timeout)
    try:
        conn.request("POST", "/mcp", body=body, headers={
            "content-type": "application/json",
            "accept": "application/json, text/event-stream",
        })
        resp = conn.getresponse()
        raw = resp.read()
    finally:
        conn.close()
    try:
        message = json.loads(raw)
    except ValueError:
        raise NekoDataError("data socket returned HTTP %s: %r" % (resp.status, raw[:300]))
    if message.get("error"):
        error = message["error"]
        raise NekoDataError(error.get("message") or json.dumps(error))
    result = message.get("result") or {}
    if result.get("isError"):
        raise NekoDataError(_text(result) or "%s failed" % name)
    return result


def _payload(value):
    if isinstance(value, str):
        try:
            value = json.loads(value)
        except ValueError:
            raise NekoDataError("execute_graphql returned text that is not JSON")
    if isinstance(value, dict) and "data" not in value and "errors" not in value:
        if "structuredContent" in value or "content" in value:
            structured = value.get("structuredContent")
            return _payload(structured if isinstance(structured, dict) else _text(value))
        if "result" in value:
            return _payload(value["result"])
    if not isinstance(value, dict):
        raise NekoDataError("execute_graphql returned no JSON object")
    return value


def query(graphql, variables=None, namespace=None):
    """Run a read query and return its data object. Raises on any GraphQL error."""
    arguments = {"query": graphql}
    if variables:
        arguments["variables"] = variables
    if namespace:
        arguments["namespace"] = namespace
    payload = _payload(call_tool("execute_graphql", arguments))
    if payload.get("errors"):
        raise NekoDataError(json.dumps(payload["errors"])[:4000])
    data = payload.get("data")
    if not isinstance(data, dict):
        raise NekoDataError("execute_graphql returned no data object")
    return data
`;

export const NEKO_QUERY_CLI = String.raw`#!/usr/bin/env python3
"""neko-query: run one GraphQL read query and print its data object as JSON.

    neko-query 'query { orders(limit: 5) { id } }'
    neko-query --variables '{"id": 7}' < query.graphql > rows.json
"""
import argparse
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.realpath(__file__)))
from neko_data import NekoDataError, query  # noqa: E402


def main():
    parser = argparse.ArgumentParser(prog="neko-query")
    parser.add_argument("graphql", nargs="?", help="query text; read from stdin when omitted")
    parser.add_argument("--variables", help="JSON object of query variables")
    args = parser.parse_args()
    graphql = args.graphql if args.graphql is not None else sys.stdin.read()
    try:
        variables = json.loads(args.variables) if args.variables else None
        json.dump(query(graphql, variables), sys.stdout)
    except (NekoDataError, ValueError) as err:
        print("neko-query: %s" % err, file=sys.stderr)
        return 1
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
`;

/** Writes neko_data.py, bound to the run's socket, and neko-query into bin. */
export async function installDataClient(
  binRoot: string,
  socketPath: string,
): Promise<void> {
  await mkdir(binRoot, { recursive: true });
  const client = NEKO_DATA_PY.replace(
    "__NEKO_DATA_SOCKET__",
    JSON.stringify(socketPath),
  );
  await writeFile(join(binRoot, "neko_data.py"), client, "utf8");
  const cli = join(binRoot, "neko-query");
  await writeFile(cli, NEKO_QUERY_CLI, "utf8");
  await chmod(cli, 0o755);
}

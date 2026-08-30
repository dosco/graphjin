"""A stand-in for the inference server a trainer would point GraphJin at.

GraphJin's agent talks to whatever OpenAI-compatible endpoint its configuration
names, so plugging a policy in is base_url and a model name rather than
anything training-specific. This serves a fixed program so the loop can be
exercised end to end without a GPU or a provider account; replace it with a
real inference server and nothing else changes.

The agent's action space is a JavaScript program rather than a tool call, so a
completion is a JSON object carrying `javascriptCode`.
"""

from __future__ import annotations

import argparse
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# A compliant program: discover a specific catalog card, then run a query, then
# answer from what came back. The protocol refuses raw GraphQL that is not
# preceded by real discovery, which is the environment behaving as it will for
# any policy.
DEFAULT_PROGRAM = """
const detail = await query_catalog({id: "table:app:main.accounts"});
const result = await execute_graphql({query: "query { accounts { count_id } }"});
await final({
  status: "answered",
  answer: "There are " + result.data.accounts[0].count_id + " accounts.",
  data: result.data,
  evidence: [detail]
});
""".strip()


class PolicyHandler(BaseHTTPRequestHandler):
    program = DEFAULT_PROGRAM
    calls = 0

    def do_POST(self) -> None:  # noqa: N802 - http.server API
        length = int(self.headers.get("Content-Length", "0"))
        _ = self.rfile.read(length)
        type(self).calls += 1
        completion = json.dumps({"javascriptCode": type(self).program})
        body = json.dumps(
            {
                "id": "policy-1",
                "object": "chat.completion",
                "model": "graphjin-policy-stub",
                "choices": [
                    {
                        "index": 0,
                        "message": {"role": "assistant", "content": completion},
                        "finish_reason": "stop",
                    }
                ],
                "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
            }
        ).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_: object) -> None:
        return


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--listen", default="127.0.0.1:8099")
    parser.add_argument("--program", default=None, help="path to a JavaScript program to serve")
    args = parser.parse_args()
    if args.program:
        PolicyHandler.program = open(args.program).read()
    host, _, port = args.listen.partition(":")
    server = ThreadingHTTPServer((host, int(port)), PolicyHandler)
    print(f"policy server on {args.listen}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()

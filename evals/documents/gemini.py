"""Minimal Gemini client: one generateContent call, with tools when given."""

from __future__ import annotations

import json
import os
import time
import urllib.error
import urllib.request

BASE = "https://generativelanguage.googleapis.com/v1beta/models"


class Gemini:
    def __init__(self, model: str, api_key: str | None = None):
        self.model = model
        self.api_key = api_key or os.environ["GEMINI_API_KEY"]

    def generate(self, contents: list[dict], system: str | None = None, tools: list[dict] | None = None,
                 timeout: float = 180) -> dict:
        body: dict = {"contents": contents}
        if system:
            body["systemInstruction"] = {"parts": [{"text": system}]}
        if tools:
            body["tools"] = [{"functionDeclarations": tools}]
        data = json.dumps(body).encode()
        for attempt in range(4):
            req = urllib.request.Request(f"{BASE}/{self.model}:generateContent", data=data,
                                         headers={"content-type": "application/json", "x-goog-api-key": self.api_key})
            try:
                with urllib.request.urlopen(req, timeout=timeout) as resp:
                    return json.load(resp)
            except urllib.error.HTTPError as e:
                if e.code in (429, 500, 502, 503, 504) and attempt < 3:
                    time.sleep(5 * (attempt + 1))
                    continue
                raise RuntimeError(f"Gemini {e.code}: {e.read()[:400]!r}") from e
            except (TimeoutError, urllib.error.URLError):
                if attempt < 3:
                    time.sleep(5 * (attempt + 1))
                    continue
                raise
        raise RuntimeError("unreachable")

    def text(self, prompt: str, system: str | None = None) -> tuple[str, dict]:
        out = self.generate([{"role": "user", "parts": [{"text": prompt}]}], system=system)
        parts = out["candidates"][0]["content"].get("parts", [])
        return "".join(p.get("text", "") for p in parts if not p.get("thought")), out.get("usageMetadata", {})

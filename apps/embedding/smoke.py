"""Runs inside the built image with --network none. Exercises real model
inference through the lazy listener, both APIs, and process eviction/restart,
not just the lightweight listener's health."""

import json
import math
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor

BASE = "http://127.0.0.1:5003"
MODEL = "google/embeddinggemma-2"

child = subprocess.Popen(
    ["lazy-service", "--listen", ":5003", "--upstream", "http://127.0.0.1:5004", "--",
     sys.executable, os.path.join(os.path.dirname(os.path.abspath(__file__)), "embedding_service.py")],
    env={**os.environ, "OPENNEKO_SERVICE_IDLE_TIMEOUT": "1s"},
)


def request(method, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method,
                                 headers={"content-type": "application/json", "authorization": "Bearer local"})
    try:
        with urllib.request.urlopen(req, timeout=150) as response:
            return response.status, json.loads(response.read())
    except urllib.error.HTTPError as error:
        return error.code, json.loads(error.read() or b"null")


def until(check):
    for _ in range(300):
        try:
            if check():
                return
        except (OSError, ValueError):
            pass
        if child.poll() is not None:
            raise SystemExit("listener exited")
        time.sleep(0.1)
    raise SystemExit("service state timed out")


def health():
    return request("GET", "/health/ready")[1]


def unit(vector, size):
    assert len(vector) == size, len(vector)
    assert all(math.isfinite(v) for v in vector)
    assert abs(math.sqrt(sum(v * v for v in vector)) - 1) < 1e-3


def embed(text):
    status, body = request("POST", "/v1/embed", {"role": "query", "task": "search", "inputs": [{"text": text}]})
    assert status == 200, (status, body)
    assert body["model"] == MODEL and body["dimensions"] == 768, body.keys()
    unit(body["vectors"][0], 768)
    return body["vectors"][0]


def dot(a, b):
    return sum(x * y for x, y in zip(a, b))


try:
    until(lambda: health()["state"] == "sleeping")
    assert health()["starts"] == 0
    with ThreadPoolExecutor(2) as pool:
        a, b = pool.map(embed, ["OpenNeko document search"] * 2)
    assert max(abs(x - y) for x, y in zip(a, b)) < 1e-5
    assert health()["starts"] == 1

    status, docs = request("POST", "/v1/embed", {"role": "document", "task": "search", "inputs": [
        {"text": "Quarterly revenue per region lives in the sales.orders table.", "title": "Revenue"},
        {"text": "Boil pasta for nine minutes in salted water.", "title": None},
    ]})
    assert status == 200, status
    query = embed("which table has sales by region?")
    assert dot(query, docs["vectors"][0]) > dot(query, docs["vectors"][1])

    status, body = request("POST", "/v1/embeddings", {"input": ["hello", "world"], "model": "x", "dimensions": 256})
    assert status == 200, (status, body)
    assert body["object"] == "list" and body["model"] == MODEL and [d["index"] for d in body["data"]] == [0, 1]
    for item in body["data"]:
        unit(item["embedding"], 256)
    assert request("POST", "/v1/embeddings", {"input": "x", "dimensions": 300})[0] == 400
    assert request("POST", "/v1/embed", {"role": "query", "task": "search", "inputs": []})[0] == 400

    until(lambda: health()["state"] == "sleeping")
    again = embed("OpenNeko document search")
    assert max(abs(x - y) for x, y in zip(a, again)) < 1e-5
    assert health()["starts"] == 2
    print("embedding_offline_lazy_restart=ok")
finally:
    child.terminate()
    child.wait(30)

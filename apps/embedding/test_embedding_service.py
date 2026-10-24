"""Tests for embedding_service. Run: python -m unittest -v test_embedding_service

The real-model test runs when OPENNEKO_EMBEDDING_TEST_MODEL=1 (it loads the
model from OPENNEKO_EMBEDDING_MODEL_PATH or the Hugging Face cache).
"""

import json
import math
import os
import threading
import unittest
import urllib.error
import urllib.request

import embedding_service as svc


def fake_encode(prompts):
    """Deterministic normalized 768-dim vectors that depend on the prompt."""
    out = []
    for prompt in prompts:
        seed = sum(prompt.encode("utf-8")) or 1
        vector = [math.sin(seed * (i + 1)) + 0.01 for i in range(svc.DIMENSIONS)]
        norm = math.sqrt(sum(v * v for v in vector))
        out.append([v / norm for v in vector])
    return out


class PrefixTests(unittest.TestCase):
    def test_search_query(self):
        self.assertEqual(svc.format_text("query", "search", "find revenue"), "task: search result | query: find revenue")

    def test_search_query_ignores_title(self):
        self.assertEqual(svc.format_text("query", "search", "q", "T"), "task: search result | query: q")

    def test_search_document_with_and_without_title(self):
        self.assertEqual(svc.format_text("document", "search", "body", "Sales"), "title: Sales | text: body")
        self.assertEqual(svc.format_text("document", "search", "body", None), "title: none | text: body")
        self.assertEqual(svc.format_text("document", "search", "body", "  "), "title: none | text: body")

    def test_none_task(self):
        self.assertEqual(svc.format_text("query", "none", "plain"), "plain")
        self.assertEqual(svc.format_text("query", "none", "plain", "T"), "plain")
        self.assertEqual(svc.format_text("document", "none", "plain"), "plain")
        self.assertEqual(svc.format_text("document", "none", "plain", "Title"), "Title\nplain")

    def test_parse_embed_keeps_order_and_trims(self):
        prompts = svc.parse_embed_request({
            "role": "document", "task": "search",
            "inputs": [{"text": "  a  ", "title": "A"}, {"text": "b"}, {"text": "c", "title": None}],
        })
        self.assertEqual(prompts, ["title: A | text: a", "title: none | text: b", "title: none | text: c"])


class ValidationTests(unittest.TestCase):
    def ok(self, **overrides):
        body = {"role": "query", "task": "search", "inputs": [{"text": "x"}]}
        body.update(overrides)
        return body

    def bad_embed(self, body):
        with self.assertRaises(svc.InputError):
            svc.parse_embed_request(body)

    def test_embed_rejections(self):
        self.bad_embed([])
        self.bad_embed(self.ok(role="answer"))
        self.bad_embed(self.ok(role=None))
        self.bad_embed(self.ok(task="classify"))
        self.bad_embed({"role": "query", "inputs": [{"text": "x"}]})
        self.bad_embed(self.ok(inputs=[]))
        self.bad_embed(self.ok(inputs="x"))
        self.bad_embed(self.ok(inputs=[{"text": "x"}] * 65))
        self.bad_embed(self.ok(inputs=[{"text": ""}]))
        self.bad_embed(self.ok(inputs=[{"text": " \n\t "}]))
        self.bad_embed(self.ok(inputs=[{"text": 5}]))
        self.bad_embed(self.ok(inputs=[{}]))
        self.bad_embed(self.ok(inputs=["x"]))
        self.bad_embed(self.ok(inputs=[{"text": "x" * 32001}]))
        self.bad_embed(self.ok(inputs=[{"text": "x", "title": 3}]))

    def test_embed_limits_accepted(self):
        self.assertEqual(len(svc.parse_embed_request(self.ok(inputs=[{"text": "x"}] * 64))), 64)
        svc.parse_embed_request(self.ok(inputs=[{"text": "x" * 32000}]))

    def bad_openai(self, body):
        with self.assertRaises(svc.InputError):
            svc.parse_openai_request(body)

    def test_openai_rejections(self):
        self.bad_openai({})
        self.bad_openai({"input": ""})
        self.bad_openai({"input": []})
        self.bad_openai({"input": ["ok", ""]})
        self.bad_openai({"input": [1, 2, 3]})
        self.bad_openai({"input": ["x"] * 65})
        self.bad_openai({"input": "x" * 32001})
        for dims in (0, 100, 384, 1024, "256", 256.0, True, -1):
            self.bad_openai({"input": "x", "dimensions": dims})

    def test_openai_accepts(self):
        self.assertEqual(svc.parse_openai_request({"input": "hi", "model": "anything"}), (["hi"], 768))
        self.assertEqual(svc.parse_openai_request({"input": ["a", " b "], "dimensions": 256}), (["a", " b "], 256))
        self.assertEqual(svc.parse_openai_request({"input": "a", "dimensions": None})[1], 768)
        for dims in (128, 256, 512, 768):
            self.assertEqual(svc.parse_openai_request({"input": "a", "dimensions": dims})[1], dims)


class ResponseTests(unittest.TestCase):
    def test_truncate_renormalizes(self):
        vectors = fake_encode(["one", "two"])
        for dims in (128, 256, 512):
            out = svc.truncate(vectors, dims)
            for full, short in zip(vectors, out):
                self.assertEqual(len(short), dims)
                self.assertAlmostEqual(math.sqrt(sum(v * v for v in short)), 1.0, places=6)
                scale = short[0] / full[0]
                for a, b in zip(full[:dims], short):
                    self.assertAlmostEqual(a * scale, b, places=9)
        self.assertIs(svc.truncate(vectors, 768), vectors)

    def test_openai_shape(self):
        body = svc.openai_response([[0.1], [0.2]])
        self.assertEqual(body, {
            "object": "list",
            "data": [{"object": "embedding", "index": 0, "embedding": [0.1]},
                     {"object": "embedding", "index": 1, "embedding": [0.2]}],
            "model": "google/embeddinggemma-2",
            "usage": {"prompt_tokens": 0, "total_tokens": 0},
        })

    def test_embedder_rejects_invalid_output(self):
        with self.assertRaises(ValueError):
            svc.Embedder(lambda p: [[0.0] * 10 for _ in p]).embed(["x"])
        with self.assertRaises(ValueError):
            svc.Embedder(lambda p: [[float("nan")] * 768 for _ in p]).embed(["x"])
        with self.assertRaises(ValueError):
            svc.Embedder(lambda p: []).embed(["x"])

    def test_token_batches(self):
        lengths = [10, 2000, 5, 2048, 700, 2048]
        batches = svc.token_batches(lengths, 4096)
        self.assertEqual(sorted(i for b in batches for i in b), list(range(len(lengths))))
        for batch in batches:
            self.assertLessEqual(len(batch) * max(lengths[i] for i in batch), 4096)
        self.assertEqual(svc.token_batches([20] * 64, 4096), [list(range(64))])
        self.assertEqual(svc.token_batches([5000], 4096), [[0]])

    def test_thread_count_env(self):
        old = os.environ.get("OPENNEKO_EMBEDDING_THREADS")
        try:
            os.environ["OPENNEKO_EMBEDDING_THREADS"] = "3"
            self.assertEqual(svc.thread_count(), 3)
            os.environ["OPENNEKO_EMBEDDING_THREADS"] = "0"
            self.assertEqual(svc.thread_count(), 1)
            del os.environ["OPENNEKO_EMBEDDING_THREADS"]
            self.assertGreaterEqual(svc.thread_count(), 1)
        finally:
            if old is None:
                os.environ.pop("OPENNEKO_EMBEDDING_THREADS", None)
            else:
                os.environ["OPENNEKO_EMBEDDING_THREADS"] = old


class HTTPTests(unittest.TestCase):
    """The HTTP contract against a fake model, including idle and 429."""

    def setUp(self):
        self.gate = threading.Event()
        self.started = threading.Event()
        self.block = False
        self.calls = []

        def encode(prompts):
            self.calls.append(list(prompts))
            if self.block:
                self.started.set()
                self.gate.wait(10)
            return fake_encode(prompts)

        self.server = svc.serve(encode, port=0)
        self.base = f"http://127.0.0.1:{self.server.server_address[1]}"
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def tearDown(self):
        self.gate.set()
        self.server.shutdown()
        self.server.server_close()

    def request(self, method, path, body=None, raw=None, headers=None):
        data = raw if raw is not None else (json.dumps(body).encode() if body is not None else None)
        req = urllib.request.Request(self.base + path, data=data, method=method, headers=headers or {})
        if data is not None:
            req.add_header("Content-Type", "application/json")
        try:
            with urllib.request.urlopen(req, timeout=20) as response:
                return response.status, json.loads(response.read())
        except urllib.error.HTTPError as error:
            return error.code, json.loads(error.read())

    def test_health(self):
        self.assertEqual(self.request("GET", "/health/ready"), (200, {"ok": True, "model": "google/embeddinggemma-2", "dimensions": 768}))
        self.assertEqual(self.request("GET", "/health/idle"), (200, {"idle": True}))

    def test_embed_route(self):
        status, body = self.request("POST", "/v1/embed", {
            "role": "query", "task": "search", "inputs": [{"text": "a"}, {"text": "b"}],
        })
        self.assertEqual(status, 200)
        self.assertEqual(set(body), {"model", "dimensions", "vectors"})
        self.assertEqual((body["model"], body["dimensions"], len(body["vectors"])), ("google/embeddinggemma-2", 768, 2))
        self.assertEqual(body["vectors"], fake_encode(["task: search result | query: a", "task: search result | query: b"]))
        self.assertEqual(self.calls, [["task: search result | query: a", "task: search result | query: b"]])

    def test_openai_route_ignores_authorization(self):
        status, body = self.request("POST", "/v1/embeddings", {"input": ["x", "y"], "model": "m", "dimensions": 256},
                                    headers={"Authorization": "Bearer local"})
        self.assertEqual(status, 200)
        self.assertEqual([d["index"] for d in body["data"]], [0, 1])
        self.assertEqual(len(body["data"][0]["embedding"]), 256)
        self.assertEqual(self.calls, [["x", "y"]])  # no prefix
        status, body = self.request("POST", "/v1/embeddings", {"input": "x"})
        self.assertEqual((status, len(body["data"][0]["embedding"])), (200, 768))

    def test_errors(self):
        self.assertEqual(self.request("GET", "/nope")[0], 404)
        self.assertEqual(self.request("POST", "/v1/other", {})[0], 404)
        self.assertEqual(self.request("POST", "/v1/embed", raw=b"{not json")[0], 400)
        self.assertEqual(self.request("POST", "/v1/embed", {"role": "query", "task": "search", "inputs": []}), (400, {"error": "inputs must be a list of 1-64 items"}))
        self.assertEqual(self.request("POST", "/v1/embeddings", {"input": "x", "dimensions": 300})[0], 400)
        status, body = self.request("POST", "/v1/embeddings", raw=b" " * (4 * 1024 * 1024 + 1))
        self.assertEqual((status, body), (413, {"error": "request body is too large"}))
        self.assertEqual(self.calls, [])

    def test_inference_failure_is_503(self):
        def broken(prompts):
            raise RuntimeError("secret text must not leak")
        self.server.RequestHandlerClass = svc.make_handler(svc.Embedder(broken))
        self.assertEqual(self.request("POST", "/v1/embeddings", {"input": "x"}), (503, {"error": "embedding unavailable"}))

    def test_queue_limit_and_idle(self):
        self.block = True
        results = []

        def call():
            results.append(self.request("POST", "/v1/embeddings", {"input": "x"})[0])

        first = threading.Thread(target=call)
        first.start()
        self.assertTrue(self.started.wait(5))
        self.assertEqual(self.request("GET", "/health/idle"), (200, {"idle": False}))
        waiters = [threading.Thread(target=call) for _ in range(svc.MAX_QUEUED)]
        for t in waiters:
            t.start()
        embedder_idle = lambda: self.request("GET", "/health/idle")[1]["idle"]
        for _ in range(200):  # wait until all 16 are queued behind the lock
            if self.server_waiting() == svc.MAX_QUEUED:
                break
            threading.Event().wait(0.02)
        self.assertEqual(self.server_waiting(), svc.MAX_QUEUED)
        self.assertEqual(self.request("POST", "/v1/embeddings", {"input": "x"}), (429, {"error": "embedding queue is full"}))
        self.gate.set()
        first.join(10)
        for t in waiters:
            t.join(10)
        self.assertEqual(sorted(results), [200] * (svc.MAX_QUEUED + 1))
        self.assertTrue(embedder_idle())

    def server_waiting(self):
        return self.server.RequestHandlerClass.embedder._waiting


@unittest.skipUnless(os.environ.get("OPENNEKO_EMBEDDING_TEST_MODEL") == "1", "set OPENNEKO_EMBEDDING_TEST_MODEL=1")
class RealModelTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.encode = staticmethod(svc.load_model())

    def embed(self, body):
        return svc.Embedder(self.encode).embed(svc.parse_embed_request(body))

    def test_query_ranks_matching_document_first(self):
        docs = [
            {"title": "Kubernetes", "text": "Restart a crashed pod by deleting it so the deployment schedules a new one."},
            {"title": "Quarterly revenue", "text": "Revenue by region for Q3 is in the sales.orders table, grouped by territory."},
            {"title": "Pasta", "text": "Boil salted water, cook spaghetti for nine minutes, then toss with garlic and oil."},
            {"title": "Vacation policy", "text": "Employees accrue 1.5 days of paid leave per month, up to 30 days."},
        ]
        doc_vectors = self.embed({"role": "document", "task": "search", "inputs": docs})
        for query, expected in (("which table has sales numbers per region?", 1),
                                ("how many days off do I get", 3),
                                ("my container keeps crashing", 0)):
            q = self.embed({"role": "query", "task": "search", "inputs": [{"text": query}]})[0]
            self.assertAlmostEqual(math.sqrt(sum(v * v for v in q)), 1.0, places=4)
            scores = [sum(a * b for a, b in zip(q, d)) for d in doc_vectors]
            self.assertEqual(max(range(len(scores)), key=scores.__getitem__), expected, (query, scores))
            short = svc.truncate([q], 256)[0]
            short_docs = svc.truncate(doc_vectors, 256)
            short_scores = [sum(a * b for a, b in zip(short, d)) for d in short_docs]
            self.assertEqual(max(range(len(short_scores)), key=short_scores.__getitem__), expected)


if __name__ == "__main__":
    unittest.main()

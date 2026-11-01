"""OpenNeko embedding service: google/embeddinggemma-2 on CPU.

The lazy-service role of the openneko binary starts this process on the first
request, proxies to it on 127.0.0.1:5004, and stops it after an idle period.

Routes:
  GET  /health/ready   model loaded (this process only listens once loaded)
  GET  /health/idle    no request in flight or queued
  POST /v1/embed       OpenNeko API: role/task-aware prefixes, input order kept
  POST /v1/embeddings  OpenAI-compatible, no prefixes, optional Matryoshka size

One inference runs at a time. Submitted text is never logged or stored.
"""

import json
import math
import os
import sys
import threading
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MODEL_NAME = "google/embeddinggemma-2"
DIMENSIONS = 768
TRUNCATION_DIMENSIONS = (128, 256, 512, 768)
MAX_INPUTS = 64
MAX_CHARS = 32000
MAX_BODY_BYTES = 4 * 1024 * 1024
MAX_QUEUED = 16
MAX_DRAIN_BYTES = 128 * 1024 * 1024
HOST = "127.0.0.1"
PORT = 5004


class InputError(ValueError):
    """A request the caller must fix (HTTP 400)."""


class QueueFull(RuntimeError):
    """MAX_QUEUED requests already wait for the model (HTTP 429)."""


def _is_int(value):
    return isinstance(value, int) and not isinstance(value, bool)


def _check_text(value, label):
    if not isinstance(value, str) or not value.strip():
        raise InputError(f"{label} must be a non-empty string")
    if len(value) > MAX_CHARS:
        raise InputError(f"{label} must contain at most {MAX_CHARS} characters")
    return value


def _check_count(items, label):
    if not isinstance(items, list) or not 1 <= len(items) <= MAX_INPUTS:
        raise InputError(f"{label} must be a list of 1-{MAX_INPUTS} items")


def format_text(role, task, text, title=None):
    """Apply EmbeddingGemma's model-card prompt format."""
    title = title.strip() if isinstance(title, str) and title.strip() else None
    if task == "search":
        if role == "query":
            return f"task: search result | query: {text}"
        return f"title: {title or 'none'} | text: {text}"
    if role == "document" and title:
        return f"{title}\n{text}"
    return text


def parse_embed_request(body):
    """Validate a /v1/embed body and return the prompts to embed, in order."""
    if not isinstance(body, dict):
        raise InputError("body must be a JSON object")
    role, task = body.get("role"), body.get("task")
    if role not in ("query", "document"):
        raise InputError('role must be "query" or "document"')
    if task not in ("search", "none"):
        raise InputError('task must be "search" or "none"')
    inputs = body.get("inputs")
    _check_count(inputs, "inputs")
    prompts = []
    for index, item in enumerate(inputs):
        if not isinstance(item, dict):
            raise InputError(f"inputs[{index}] must be an object")
        text = _check_text(item.get("text"), f"inputs[{index}].text").strip()
        title = item.get("title")
        if title is not None and not isinstance(title, str):
            raise InputError(f"inputs[{index}].title must be a string or null")
        prompts.append(format_text(role, task, text, title))
    return prompts


def parse_openai_request(body):
    """Validate a /v1/embeddings body; return (texts, dimensions). No prefixes."""
    if not isinstance(body, dict):
        raise InputError("body must be a JSON object")
    raw = body.get("input")
    texts = [raw] if isinstance(raw, str) else raw
    _check_count(texts, "input")
    for index, text in enumerate(texts):
        _check_text(text, f"input[{index}]")
    dimensions = body.get("dimensions")
    if dimensions is None:
        dimensions = DIMENSIONS
    if not _is_int(dimensions) or dimensions not in TRUNCATION_DIMENSIONS:
        raise InputError("dimensions must be one of 128, 256, 512, or 768")
    return list(texts), dimensions


def truncate(vectors, dimensions):
    """Matryoshka truncation: keep the leading dimensions and re-normalize."""
    if dimensions == DIMENSIONS:
        return vectors
    out = []
    for vector in vectors:
        head = vector[:dimensions]
        norm = math.sqrt(sum(v * v for v in head))
        if not norm or not math.isfinite(norm):
            raise ValueError("invalid truncated embedding")
        out.append([v / norm for v in head])
    return out


def openai_response(vectors):
    return {
        "object": "list",
        "data": [{"object": "embedding", "index": i, "embedding": v} for i, v in enumerate(vectors)],
        "model": MODEL_NAME,
        "usage": {"prompt_tokens": 0, "total_tokens": 0},
    }


def thread_count():
    """OPENNEKO_EMBEDDING_THREADS, else the container's CPU allowance (>= 1)."""
    raw = os.environ.get("OPENNEKO_EMBEDDING_THREADS", "").strip()
    if raw:
        try:
            return max(1, int(raw))
        except ValueError:
            pass
    try:
        cpus = len(os.sched_getaffinity(0))
    except (AttributeError, OSError):
        cpus = os.cpu_count() or 1
    try:  # cgroup v2 CPU quota, e.g. "200000 100000" for cpus: 2.0
        with open("/sys/fs/cgroup/cpu.max", encoding="ascii") as handle:
            quota, period = handle.read().split()[:2]
        if quota != "max":
            cpus = min(cpus, math.ceil(int(quota) / int(period)))
    except (OSError, ValueError):
        pass
    return max(1, cpus)


class Embedder:
    """Serializes inference behind one lock and bounds the waiting queue.

    `encode` takes a list of prompts and returns a list of vectors (or an
    array-like of shape [n, 768]).
    """

    def __init__(self, encode, dimensions=DIMENSIONS, max_queued=MAX_QUEUED):
        self._encode = encode
        self._dimensions = dimensions
        self._max_queued = max_queued
        self._lock = threading.Lock()
        self._state = threading.Lock()
        self._waiting = 0
        self._active = 0

    def idle(self):
        with self._state:
            return self._waiting == 0 and self._active == 0

    def embed(self, prompts):
        with self._state:
            if self._waiting >= self._max_queued:
                raise QueueFull()
            self._waiting += 1
        acquired = False
        try:
            self._lock.acquire()
            acquired = True
            with self._state:
                self._waiting -= 1
                self._active += 1
            result = self._encode(prompts)
        finally:
            if acquired:
                with self._state:
                    self._active -= 1
                self._lock.release()
            else:
                with self._state:
                    self._waiting -= 1
        vectors = [[float(v) for v in row] for row in result]
        if len(vectors) != len(prompts) or any(
            len(v) != self._dimensions or not all(math.isfinite(x) for x in v) for v in vectors
        ):
            raise ValueError("invalid embedding output")
        return vectors


def _env_int(name, default):
    try:
        return max(1, int(os.environ.get(name, "").strip() or default))
    except ValueError:
        return default


def token_batches(lengths, budget):
    """Group input indexes so each batch's padded size (count x longest input)
    stays within `budget` tokens. Activation memory grows with that product;
    an input longer than the budget runs alone."""
    order = sorted(range(len(lengths)), key=lambda i: -lengths[i])
    batches, current = [], []
    for index in order:
        longest = lengths[current[0]] if current else lengths[index]
        if current and (len(current) + 1) * longest > budget:
            batches.append(current)
            current = []
        current.append(index)
    if current:
        batches.append(current)
    return batches


def load_model():
    """Load the vendored text-only model. Returns an encode(prompts) function.

    Memory: about 1.8 GB resident once loaded, plus roughly 150 MB per 1000
    padded tokens in a batch. OPENNEKO_EMBEDDING_MAX_TOKENS (default 2048)
    truncates each input and OPENNEKO_EMBEDDING_BATCH_TOKENS (default 2048)
    bounds each batch so a request stays well inside the 2.5 GB limit.
    """
    threads = thread_count()
    os.environ.setdefault("OMP_NUM_THREADS", str(threads))
    os.environ.setdefault("TOKENIZERS_PARALLELISM", "false")
    import ctypes

    import torch
    from sentence_transformers import SentenceTransformer

    torch.set_num_threads(threads)
    try:
        torch.set_num_interop_threads(1)
    except RuntimeError:
        pass
    source = os.environ.get("OPENNEKO_EMBEDDING_MODEL_PATH") or MODEL_NAME
    model = SentenceTransformer(
        source,
        device="cpu",
        model_kwargs={"torch_dtype": torch.float32},
        config_kwargs={"vision_config": None, "audio_config": None},
    )
    model.eval()
    max_tokens = _env_int("OPENNEKO_EMBEDDING_MAX_TOKENS", 2048)
    budget = max(max_tokens, _env_int("OPENNEKO_EMBEDDING_BATCH_TOKENS", 2048))
    model.max_seq_length = max_tokens
    try:
        trim = ctypes.CDLL("libc.so.6").malloc_trim
    except (OSError, AttributeError):
        trim = None

    def release_memory():
        if trim is not None:
            trim(0)

    release_memory()

    def encode(prompts):
        lengths = [
            len(ids)
            for ids in model.tokenizer(prompts, truncation=True, max_length=max_tokens)["input_ids"]
        ]
        vectors = [None] * len(prompts)
        try:
            with torch.inference_mode():
                for batch in token_batches(lengths, budget):
                    rows = model.encode(
                        [prompts[i] for i in batch],
                        batch_size=len(batch),
                        normalize_embeddings=True,
                        convert_to_numpy=True,
                        show_progress_bar=False,
                    ).tolist()
                    for index, row in zip(batch, rows):
                        vectors[index] = row
        finally:
            release_memory()
        return vectors

    return encode


def make_handler(embedder):
    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"
        server_version = "openneko-embedding"
        sys_version = ""

        def log_message(self, format, *args):  # noqa: A002 - stdlib signature
            pass  # request lines only; still keep logs quiet and text-free

        def _reply(self, status, body, close=False):
            data = json.dumps(body, separators=(",", ":")).encode("utf-8")
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            if close:
                self.send_header("Connection", "close")
                self.close_connection = True
            self.end_headers()
            self.wfile.write(data)

        def _read_body(self):
            """Return the body bytes, or None after replying 413."""
            if "chunked" in self.headers.get("Transfer-Encoding", "").lower():
                chunks, total = [], 0
                while True:
                    line = self.rfile.readline(1024)
                    size = int(line.split(b";")[0].strip() or b"0", 16)
                    if size == 0:
                        while self.rfile.readline(1024) not in (b"\r\n", b"\n", b""):
                            pass
                        return b"".join(chunks)
                    total += size
                    if total > MAX_BODY_BYTES:
                        self._reply(413, {"error": "request body is too large"}, close=True)
                        return None
                    chunks.append(self.rfile.read(size))
                    self.rfile.readline(1024)
            length = int(self.headers.get("Content-Length") or 0)
            if length > MAX_BODY_BYTES:
                # Discard (bounded) what the client is still sending so it can
                # read the 413 instead of failing with a broken pipe.
                remaining = min(length, MAX_DRAIN_BYTES)
                while remaining > 0:
                    chunk = self.rfile.read(min(remaining, 65536))
                    if not chunk:
                        break
                    remaining -= len(chunk)
                self._reply(413, {"error": "request body is too large"}, close=True)
                return None
            return self.rfile.read(length) if length > 0 else b""

        def do_GET(self):
            path = self.path.split("?", 1)[0]
            if path == "/health/ready":
                self._reply(200, {"ok": True, "model": MODEL_NAME, "dimensions": DIMENSIONS})
            elif path == "/health/idle":
                self._reply(200, {"idle": self.embedder.idle()})
            else:
                self._reply(404, {"error": "not found"})

        def do_POST(self):
            path = self.path.split("?", 1)[0]
            if path not in ("/v1/embed", "/v1/embeddings"):
                self._reply(404, {"error": "not found"}, close=True)
                return
            try:
                raw = self._read_body()
            except (ValueError, OSError):
                self._reply(400, {"error": "invalid request body"}, close=True)
                return
            if raw is None:
                return
            try:
                body = json.loads(raw.decode("utf-8"))
            except (UnicodeDecodeError, ValueError):
                self._reply(400, {"error": "invalid JSON"})
                return
            try:
                if path == "/v1/embed":
                    prompts, dimensions = parse_embed_request(body), DIMENSIONS
                else:
                    prompts, dimensions = parse_openai_request(body)
            except InputError as error:
                self._reply(400, {"error": str(error)})
                return
            try:
                vectors = truncate(self.embedder.embed(prompts), dimensions)
            except QueueFull:
                self._reply(429, {"error": "embedding queue is full"})
                return
            except Exception as error:  # never include submitted text
                print(f"[embedding] inference failed: {type(error).__name__}", file=sys.stderr, flush=True)
                self._reply(503, {"error": "embedding unavailable"})
                return
            if path == "/v1/embed":
                self._reply(200, {"model": MODEL_NAME, "dimensions": DIMENSIONS, "vectors": vectors})
            else:
                self._reply(200, openai_response(vectors))

        def _not_found(self):
            self._reply(404, {"error": "not found"}, close=True)

        do_PUT = do_DELETE = do_PATCH = do_HEAD = do_OPTIONS = _not_found

    Handler.embedder = embedder
    return Handler


class Server(ThreadingHTTPServer):
    daemon_threads = True
    allow_reuse_address = True
    request_queue_size = 64

    def handle_error(self, request, client_address):
        # Peers (the lazy listener's probes) close keep-alive connections;
        # that is normal. Never print request data.
        error = sys.exc_info()[1]
        if not isinstance(error, (ConnectionError, TimeoutError)):
            print(f"[embedding] request failed: {type(error).__name__}", file=sys.stderr, flush=True)


def serve(encode, host=HOST, port=PORT):
    return Server((host, port), make_handler(Embedder(encode)))


def check(encode):
    """Build-time check: one embedding with 768 finite, normalized dimensions."""
    vector = Embedder(encode).embed([format_text("query", "search", "ready")])[0]
    norm = math.sqrt(sum(v * v for v in vector))
    if len(vector) != DIMENSIONS or abs(norm - 1) > 1e-3:
        raise SystemExit("embedding model failed its check")
    print(f"{MODEL_NAME}: {len(vector)} finite dimensions")


def main(argv):
    encode = load_model()
    if "--check" in argv:
        check(encode)
        return
    server = serve(encode)  # bind only after the model is loaded
    print(f"[embedding] {MODEL_NAME} ready on {HOST}:{PORT}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    import signal

    signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
    main(sys.argv[1:])

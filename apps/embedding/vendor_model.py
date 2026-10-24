"""Build-time only: vendor a text-only copy of google/embeddinggemma-2.

Downloads one pinned revision, keeps the language-model weights (dropping the
vision and audio towers, about 1 GB of bf16 weights the text service never
loads), and copies the configs, tokenizer, and model card. The running service
loads this directory with HF_HUB_OFFLINE=1 and never downloads.

Usage: python vendor_model.py OUTPUT_DIR [REVISION]
"""

import os
import shutil
import sys
import tempfile

from huggingface_hub import snapshot_download
from safetensors import safe_open
from safetensors.torch import save_file

REPO = "google/embeddinggemma-2"
TEXT_PREFIX = "language_model."


def vendor(output, revision):
    with tempfile.TemporaryDirectory() as cache:
        source = snapshot_download(REPO, revision=revision, cache_dir=cache)
        os.makedirs(output, exist_ok=True)
        for root, _dirs, files in os.walk(source):
            for name in files:
                path = os.path.join(root, name)
                relative = os.path.relpath(path, source)
                if relative == "model.safetensors" or relative.startswith("."):
                    continue
                target = os.path.join(output, relative)
                os.makedirs(os.path.dirname(target), exist_ok=True)
                shutil.copyfile(path, target)
        with safe_open(os.path.join(source, "model.safetensors"), "pt") as weights:
            text = {key: weights.get_tensor(key) for key in weights.keys() if key.startswith(TEXT_PREFIX)}
        if not text:
            raise SystemExit("no language-model weights found")
        save_file(text, os.path.join(output, "model.safetensors"), metadata={"format": "pt"})
    # Readable by the unprivileged runtime user (safetensors writes 0600).
    for root, dirs, files in os.walk(output):
        for name in dirs:
            os.chmod(os.path.join(root, name), 0o755)
        for name in files:
            os.chmod(os.path.join(root, name), 0o644)
    print(f"Vendored text-only {REPO}@{revision}: {len(text)} tensors -> {output}")


if __name__ == "__main__":
    if len(sys.argv) not in (2, 3):
        raise SystemExit(__doc__)
    vendor(sys.argv[1], sys.argv[2] if len(sys.argv) == 3 else "main")

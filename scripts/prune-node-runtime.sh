#!/bin/sh
# Remove build/debug data and native artifacts for platforms that cannot run in
# the target image. pnpm keeps every optional ONNX/Sharp platform package in a
# deploy, even though a Linux image can use exactly one of them.
set -eu

runtime_root="${1:?usage: prune-node-runtime.sh ROOT TARGETARCH}"
target_arch="${2:?usage: prune-node-runtime.sh ROOT TARGETARCH}"

case "$target_arch" in
  amd64) node_arch=x64 ;;
  arm64) node_arch=arm64 ;;
  *)
    echo "unsupported Node runtime architecture: $target_arch" >&2
    exit 1
    ;;
esac

# Source maps are development diagnostics. Keeping them in production closures
# adds tens of megabytes without changing runtime behavior.
find "$runtime_root/node_modules" -type f -name '*.map' -delete 2>/dev/null || true

# onnxruntime-node publishes macOS, Windows, Linux x64, and Linux arm64 native
# libraries in one package. Retain only the Linux directory for this image.
find "$runtime_root/node_modules" -type d \
  -path '*/node_modules/onnxruntime-node/bin/napi-v6' -print 2>/dev/null |
while IFS= read -r napi_root; do
  find "$napi_root" -mindepth 1 -maxdepth 1 -type d ! -name linux \
    -exec rm -rf '{}' +
  find "$napi_root/linux" -mindepth 1 -maxdepth 1 -type d \
    ! -name "$node_arch" -exec rm -rf '{}' +
done

# The Linux x64 onnxruntime-node postinstall also downloads CUDA and TensorRT
# providers. These control-plane images do not ship the NVIDIA runtime and run
# embeddings on CPU, so the providers are unusable; the CUDA library alone is
# over 300 MiB uncompressed. Keep the CPU runtime and shared provider shim.
find "$runtime_root/node_modules" -type f \
  \( -name 'libonnxruntime_providers_cuda.so' \
     -o -name 'libonnxruntime_providers_tensorrt.so' \) \
  -delete 2>/dev/null || true

# The package's script directory is only for install/build/prepack. Production
# loads dist/index.js and the retained native CPU runtime; keeping Microsoft's
# download metadata and postinstall implementation serves no runtime purpose.
find "$runtime_root/node_modules" -type d \
  -path '*/node_modules/onnxruntime-node/script' -print 2>/dev/null |
while IFS= read -r install_scripts; do
  rm -rf "$install_scripts"
done

# Transformers.js imports the WebGPU entry module even on Node, but selects
# onnxruntime-node for execution. Preserve that entry module and remove its
# browser-only WASM engines and duplicate browser bundles from server images.
find "$runtime_root/node_modules" -type d \
  -path '*/node_modules/onnxruntime-web/dist' -print 2>/dev/null |
while IFS= read -r web_dist; do
  find "$web_dist" -type f ! -name 'ort.webgpu.bundle.min.mjs' -delete
done

# Sharp also installs glibc/musl and foreign-platform optional payloads. The
# Node images use glibc, so only their matching Linux package can be loaded.
pnpm_root="$runtime_root/node_modules/.pnpm"
if [ -d "$pnpm_root" ]; then
  find "$pnpm_root" -mindepth 1 -maxdepth 1 -type d \
    \( -name '@img+sharp-*' -o -name '@img+sharp-libvips-*' \) \
    ! -name "@img+sharp-linux-${node_arch}@*" \
    ! -name "@img+sharp-libvips-linux-${node_arch}@*" \
    -exec rm -rf '{}' +
fi

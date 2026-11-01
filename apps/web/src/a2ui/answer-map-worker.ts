const WORKER = new URL("../../node_modules/maplibre-gl/dist/maplibre-gl-worker.mjs", import.meta.url);
const SHARED = new URL("../../node_modules/maplibre-gl/dist/maplibre-gl-shared.mjs", import.meta.url);

let workerUrl: Promise<string> | null = null;

/**
 * MapLibre finds its worker beside its own module, which the bundler does not
 * emit. Serve both files as static assets and point the worker at the shared
 * chunk's real URL.
 */
export function mapWorkerUrl(): Promise<string> {
  workerUrl ??= fetch(String(WORKER))
    .then((response) => {
      if (!response.ok) throw new Error(`map worker ${response.status}`);
      return response.text();
    })
    .then((source) => {
      const shared = new URL(String(SHARED), window.location.href).href;
      const linked = source.replaceAll("./maplibre-gl-shared.mjs", shared);
      return URL.createObjectURL(new Blob([linked], { type: "text/javascript" }));
    });
  return workerUrl;
}

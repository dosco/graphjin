import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker.js?worker&inline";
import JsonWorker from "monaco-editor/esm/vs/language/json/json.worker.js?worker&inline";
import GraphQLWorker from "monaco-graphql/esm/graphql.worker.js?worker&inline";

let lastMonacoWorkerFallbackAt = 0;

const suppressMonacoWorkerFallbackNoise = () => {
  const originalWarn = console.warn.bind(console);

  console.warn = (...args) => {
    const text = args.map((arg) => String(arg)).join(" ");

    if (text.includes("Could not create web worker(s). Falling back to loading web worker code in main thread")) {
      lastMonacoWorkerFallbackAt = Date.now();
      return;
    }

    if (Date.now() - lastMonacoWorkerFallbackAt < 1500 && args.length === 1 && (args[0] == null || args[0] instanceof Event)) {
      return;
    }

    originalWarn(...args);
  };

  window.addEventListener("error", (event) => {
    if (Date.now() - lastMonacoWorkerFallbackAt < 1500 && (event.message === "Unknown Error" || event.error instanceof Event)) {
      event.preventDefault();
    }
  }, true);

  window.addEventListener("unhandledrejection", (event) => {
    const reason = event.reason;

    if (Date.now() - lastMonacoWorkerFallbackAt < 1500 && (reason instanceof Event || String(reason) === "[object Event]")) {
      event.preventDefault();
    }
  }, true);
};

suppressMonacoWorkerFallbackNoise();

const withHandledWorkerErrors = (worker) => {
  worker.addEventListener("error", (event) => {
    lastMonacoWorkerFallbackAt = Date.now();
    event.preventDefault();
    event.stopImmediatePropagation();
  });

  return worker;
};

globalThis.MonacoEnvironment = {
  getWorker(_workerId, label) {
    if (label === "json") {
      return withHandledWorkerErrors(new JsonWorker());
    }

    if (label === "graphql") {
      return withHandledWorkerErrors(new GraphQLWorker());
    }

    return withHandledWorkerErrors(new EditorWorker());
  },
};

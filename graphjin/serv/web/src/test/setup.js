import "@testing-library/jest-dom/vitest";

const memoryStorage = new Map();
const localStorageMock = {
  getItem: (key) => memoryStorage.get(String(key)) ?? null,
  setItem: (key, value) => memoryStorage.set(String(key), String(value)),
  removeItem: (key) => memoryStorage.delete(String(key)),
  clear: () => memoryStorage.clear(),
  key: (index) => [...memoryStorage.keys()][index] ?? null,
  get length() { return memoryStorage.size; },
};

Object.defineProperty(globalThis, "localStorage", { configurable: true, value: localStorageMock });
Object.defineProperty(window, "localStorage", { configurable: true, value: localStorageMock });

Object.defineProperty(window, "matchMedia", {
  writable: true,
  value: (query) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  }),
});

Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
  configurable: true,
  value: () => null,
});

import { afterEach, describe, expect, it, vi } from "vitest";
import { PRINT_PREPARE_EVENT, printThread, type PrintPrepareDetail } from "@/lib/print-thread";

function stubBrowser() {
  const target = new EventTarget();
  const dataset: Record<string, string> = {};
  const steps: string[] = [];
  const window = Object.assign(target, {
    print: vi.fn(() => {
      steps.push(`print printing=${"printing" in dataset}`);
    }),
  });
  vi.stubGlobal("window", window);
  vi.stubGlobal("document", { documentElement: { dataset } });
  vi.stubGlobal("requestAnimationFrame", (callback: () => void) => setTimeout(callback, 0));
  return { window, dataset, steps };
}

describe("printThread", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("lays the page out for A4, waits for charts and maps, prints, then restores", async () => {
    const { window, dataset, steps } = stubBrowser();
    window.addEventListener(PRINT_PREPARE_EVENT, (event) => {
      steps.push(`prepare printing=${"printing" in dataset}`);
      (event as CustomEvent<PrintPrepareDetail>).detail.wait(new Promise((resolve) => setTimeout(() => {
        steps.push("map captured");
        resolve(undefined);
      }, 20)));
    });
    await printThread();
    expect(steps).toEqual(["prepare printing=true", "map captured", "print printing=true"]);
    window.dispatchEvent(new Event("afterprint"));
    expect("printing" in dataset).toBe(false);
  });
});

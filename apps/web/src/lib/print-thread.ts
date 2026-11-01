/** Fired after the page takes its A4 layout; listeners may hold printing until a promise settles. */
export const PRINT_PREPARE_EVENT = "openneko:print-prepare";

export type PrintPrepareDetail = { wait: (pending: Promise<unknown>) => void };

const SETTLE_MS = 350;
const MAX_WAIT_MS = 4_000;

const frame = () => new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));

/**
 * Lay the thread out at A4 width, let charts and maps redraw at that size,
 * then open the print dialog. The page returns to its screen layout after.
 */
export async function printThread(): Promise<void> {
  const root = document.documentElement;
  root.dataset.printing = "";
  await frame();
  await frame();
  const pending: Promise<unknown>[] = [];
  window.dispatchEvent(new CustomEvent<PrintPrepareDetail>(PRINT_PREPARE_EVENT, {
    detail: { wait: (promise) => pending.push(promise) },
  }));
  await Promise.race([
    Promise.allSettled(pending),
    new Promise((resolve) => setTimeout(resolve, MAX_WAIT_MS)),
  ]);
  await new Promise((resolve) => setTimeout(resolve, SETTLE_MS));
  const restore = () => {
    delete root.dataset.printing;
    window.removeEventListener("afterprint", restore);
  };
  window.addEventListener("afterprint", restore);
  window.print();
}

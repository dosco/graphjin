"use client";

import { useCallback, useEffect, useRef, useState } from "react";

type Opts<T> = {
  delayMs: number;
  save: (value: T) => Promise<void>;
};

type Api<T> = {
  schedule: (value: T) => void;
  flush: () => Promise<void>;
  isSaving: boolean;
  lastError: Error | null;
};

export function useDebouncedSave<T>({ delayMs, save }: Opts<T>): Api<T> {
  const pendingRef = useRef<{ has: boolean; value: T | undefined }>({
    has: false,
    value: undefined,
  });
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const tailRef = useRef<Promise<void>>(Promise.resolve());
  const saveRef = useRef(save);

  const [isSaving, setIsSaving] = useState(false);
  const [lastError, setLastError] = useState<Error | null>(null);

  useEffect(() => {
    saveRef.current = save;
  }, [save]);

  const runPending = useCallback(() => {
    if (!pendingRef.current.has) return;
    const value = pendingRef.current.value as T;
    pendingRef.current = { has: false, value: undefined };
    setIsSaving(true);
    tailRef.current = tailRef.current.then(async () => {
      try {
        await saveRef.current(value);
        setLastError(null);
      } catch (err) {
        setLastError(err instanceof Error ? err : new Error(String(err)));
      } finally {
        if (!pendingRef.current.has) setIsSaving(false);
      }
    });
  }, []);

  const schedule = useCallback(
    (value: T) => {
      pendingRef.current = { has: true, value };
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => {
        timerRef.current = null;
        runPending();
      }, delayMs);
    },
    [delayMs, runPending],
  );

  const flush = useCallback(async () => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    runPending();
    await tailRef.current;
  }, [runPending]);

  useEffect(() => {
    const onBeforeUnload = () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      runPending();
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => {
      window.removeEventListener("beforeunload", onBeforeUnload);
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      runPending();
    };
  }, [runPending]);

  return { schedule, flush, isSaving, lastError };
}

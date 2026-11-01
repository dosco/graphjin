"use client";

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type MutableRefObject,
} from "react";

type WorkShellContextValue = {
  activeRunId: string | null;
  setActiveRunId: (runId: string | null) => void;
  // Imperative handle WorkScreen registers so the rail can drop an "Ask next"
  // question into the CURRENT thread's composer (for the operator to edit),
  // without the rail reaching into WorkScreen's draft state.
  insertComposerRef: MutableRefObject<((q: string) => void) | null>;
  // Imperative handle WorkScreen registers so an interactive A2UI component
  // (e.g. a Choice button inside an answer) can submit a follow-up turn
  // directly — the action loop, distinct from the editable "Ask next" drop.
  submitFollowUpRef: MutableRefObject<((prompt: string) => void) | null>;
  // Stable wrapper consumers call; reads the ref in a callback (not render).
  submitFollowUp: (prompt: string) => void;
};

const WorkShellContext = createContext<WorkShellContextValue>({
  activeRunId: null,
  setActiveRunId: () => {},
  insertComposerRef: { current: null },
  submitFollowUpRef: { current: null },
  submitFollowUp: () => {},
});

export function WorkShellProvider({ children }: { children: React.ReactNode }) {
  const [activeRunId, setActiveRunId] = useState<string | null>(null);
  const insertComposerRef = useRef<((q: string) => void) | null>(null);
  const submitFollowUpRef = useRef<((prompt: string) => void) | null>(null);
  const submitFollowUp = useCallback((prompt: string) => {
    submitFollowUpRef.current?.(prompt);
  }, []);
  const value = useMemo(
    () => ({
      activeRunId,
      setActiveRunId,
      insertComposerRef,
      submitFollowUpRef,
      submitFollowUp,
    }),
    [activeRunId, submitFollowUp],
  );
  return (
    <WorkShellContext.Provider value={value}>
      {children}
    </WorkShellContext.Provider>
  );
}

export function useWorkShell() {
  return useContext(WorkShellContext);
}

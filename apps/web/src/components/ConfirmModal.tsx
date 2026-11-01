"use client";

import { createRoot, type Root } from "react-dom/client";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";

export type ConfirmDialogOptions = {
  title: string;
  description?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  destructive?: boolean;
};

let activeRoot: Root | null = null;
let activeContainer: HTMLDivElement | null = null;

function cleanup() {
  const root = activeRoot;
  const container = activeContainer;
  activeRoot = null;
  activeContainer = null;
  queueMicrotask(() => {
    root?.unmount();
    container?.remove();
  });
}

export function confirmDialog(options: ConfirmDialogOptions): Promise<boolean> {
  if (typeof document === "undefined") return Promise.resolve(false);
  cleanup();
  return new Promise<boolean>((resolve) => {
    const container = document.createElement("div");
    document.body.appendChild(container);
    activeContainer = container;
    activeRoot = createRoot(container);
    const choose = (choice: boolean) => {
      cleanup();
      resolve(choice);
    };
    activeRoot.render(
      <AlertDialog open onOpenChange={(open) => !open && choose(false)}>
        <AlertDialogContent className="max-w-[420px] rounded-card border border-border bg-card px-[22px] pb-[18px] pt-[22px] shadow-lift">
          <AlertDialogHeader>
            <AlertDialogTitle className="font-display text-base font-bold leading-tight text-text">
              {options.title}
            </AlertDialogTitle>
            {options.description ? (
              <AlertDialogDescription className="mt-2 whitespace-pre-line text-left text-ui-body leading-[1.55] text-text2">
                {options.description}
              </AlertDialogDescription>
            ) : null}
          </AlertDialogHeader>
          <AlertDialogFooter className="mx-0 mb-0 mt-[18px] border-0 bg-transparent p-0">
            <AlertDialogCancel size="sm" onClick={() => choose(false)}>
              {options.cancelLabel ?? "Cancel"}
            </AlertDialogCancel>
            <AlertDialogAction
              variant={options.destructive ? "danger" : "primary"}
              size="sm"
              onClick={() => choose(true)}
            >
              {options.confirmLabel ?? "Confirm"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>,
    );
  });
}

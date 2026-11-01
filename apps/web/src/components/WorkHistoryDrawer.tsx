"use client";

import { X } from "lucide-react";
import AskHistoryPanel from "@/components/AskHistoryPanel";
import { IconButton } from "@/components/ui/button";
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";

export default function WorkHistoryDrawer({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  return (
    <Sheet open={open} onOpenChange={(next) => !next && onClose()}>
      <SheetContent
        side="right"
        showCloseButton={false}
        overlayClassName="work-history-scrim"
        className="work-history-drawer gap-0 border-border bg-card p-0"
      >
        <SheetHeader className="work-history-drawer-head">
          <div>
            <span>OpenNeko / Work</span>
            <SheetTitle>Past work</SheetTitle>
            <SheetDescription>Resume one of your threads.</SheetDescription>
          </div>
          <SheetClose asChild>
            <IconButton
              label="Close thread history"
              size="icon-sm"
              variant="ghost"
              className="work-history-close"
            >
              <X aria-hidden="true" strokeWidth={2} />
            </IconButton>
          </SheetClose>
        </SheetHeader>
        <AskHistoryPanel className="work-history-panel" onNavigate={onClose} />
      </SheetContent>
    </Sheet>
  );
}

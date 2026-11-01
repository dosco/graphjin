"use client";

import { useEffect, useState } from "react";
import { ArrowDown, ArrowUp } from "lucide-react";
import { IconButton } from "@/components/ui/button";

export function ThreadJump({ onTop }: { onTop: () => Promise<void> }) {
  const [awayFromTop, setAwayFromTop] = useState(false);
  const [awayFromEnd, setAwayFromEnd] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const update = () => {
      const screen = window.innerHeight;
      setAwayFromTop(window.scrollY > screen);
      setAwayFromEnd(document.documentElement.scrollHeight - window.scrollY - screen > screen);
    };
    update();
    window.addEventListener("scroll", update, { passive: true });
    window.addEventListener("resize", update);
    return () => {
      window.removeEventListener("scroll", update);
      window.removeEventListener("resize", update);
    };
  }, []);

  if (!awayFromTop && !awayFromEnd) return null;

  async function jumpTop() {
    setBusy(true);
    try {
      await onTop();
      window.scrollTo({ top: 0, behavior: "smooth" });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="thread-jump">
      {awayFromTop && !awayFromEnd ? (
        <IconButton label="Jump to the first message" variant="secondary" onClick={() => void jumpTop()} disabled={busy}>
          <ArrowUp aria-hidden="true" strokeWidth={2} />
        </IconButton>
      ) : null}
      {awayFromEnd ? (
        <IconButton
          label="Jump to the latest message"
          variant="secondary"
          onClick={() =>
            window.scrollTo({ top: document.documentElement.scrollHeight, behavior: "smooth" })
          }
        >
          <ArrowDown aria-hidden="true" strokeWidth={2} />
        </IconButton>
      ) : null}
    </div>
  );
}

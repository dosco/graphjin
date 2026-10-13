"use client";

import { useEffect, useMemo, useState } from "react";
import { renderComponent, renderChildren } from "@/a2ui/renderer";
import { applyMessage, getRootComponent, setDataModelValue } from "@/a2ui/surface";
import { buildActionFollowUp } from "@/a2ui/action";
import type { SurfaceState, A2UIMessage } from "@/a2ui/types";
import { useWorkShell } from "../work-shell-context";

// Back-compat: confirmation cards (workflow/trigger/rule saves) used to be
// emitted as the dashboard `Briefing` root, which renders the 52px display
// greeting — jarring inside the chat timeline. They now emit a `Confirmation`
// component. Run events persisted before that change are frozen as `Briefing`,
// so normalize them here at render time. A real briefing (BriefingCard
// children) is left untouched.
function remapLegacyConfirmations(messages: A2UIMessage[]): A2UIMessage[] {
  return messages.map((message) => {
    if (!("updateComponents" in message)) return message;
    const comps = message.updateComponents.components;
    const isConfirmation =
      comps.some((c) => c.component === "Briefing") &&
      !comps.some((c) => c.component === "BriefingCard");
    if (!isConfirmation) return message;
    return {
      ...message,
      updateComponents: {
        ...message.updateComponents,
        components: comps.map((c) => {
          if (c.component !== "Briefing") return c;
          const { greeting, subtitle, role, isExample, ...rest } = c;
          void role;
          void isExample;
          return {
            ...rest,
            component: "Confirmation",
            label: typeof greeting === "string" ? greeting : "",
            title: typeof subtitle === "string" ? subtitle : "",
          };
        }),
      },
    };
  });
}

export function SurfaceBlock({ messages }: { messages: A2UIMessage[] }) {
  const surfaces = useMemo(() => {
    let next = new Map<string, SurfaceState>();
    for (const message of remapLegacyConfirmations(messages)) {
      next = applyMessage(next, message);
    }
    return next;
  }, [messages]);

  const { submitFollowUp } = useWorkShell();
  const nodes: React.ReactNode[] = [];
  for (const [, surface] of surfaces) {
    if (surface.components.size === 0) continue;
    nodes.push(<InteractiveSurface key={surface.surfaceId} surface={surface} submitFollowUp={submitFollowUp} />);
  }

  if (nodes.length === 0) return null;
  return <div className="flex flex-col gap-2.5 mt-1">{nodes}</div>;
}

function InteractiveSurface({
  surface,
  submitFollowUp,
}: {
  surface: SurfaceState;
  submitFollowUp: (prompt: string) => void;
}) {
  const [dataModel, setDataModel] = useState(surface.dataModel);
  useEffect(() => {
    const timer = window.setTimeout(
      () => setDataModel(surface.dataModel),
      0,
    );
    return () => window.clearTimeout(timer);
  }, [surface.dataModel]);

  const liveSurface = { ...surface, dataModel };
  const ctx = {
    surface: liveSurface,
    onDataChange: (path: string, value: unknown) => {
      setDataModel((current) => setDataModelValue(current, path, value));
    },
    onAction: (
      _componentId: string,
      _eventName: string,
      context?: Record<string, unknown>,
    ) => {
      const followUp = buildActionFollowUp(context);
      if (followUp) submitFollowUp(followUp);
    },
  };
  const root = getRootComponent(liveSurface);
  const ids = Array.from(liveSurface.components.keys());
  return (
    <div className="work-surface-frame">
      {root ? renderComponent(root, ctx) : renderChildren(ids, ctx)}
    </div>
  );
}

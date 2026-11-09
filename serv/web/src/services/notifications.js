import React from "react";
import { useQueryClient } from "@tanstack/react-query";

import { parseSSEFrame } from "./agent";
import { fetchJSON } from "./graphql";
import { operatorIdentityHeaders, operatorIdentityKey, useOperatorIdentity } from "./identity";

// Notifications: what happened while you were away (a change waits for your
// approval, a routine answered, a run stopped). The server streams new ones;
// the list and the unseen count live in react-query so the nav and the inbox
// read the same data.

export const notificationsKey = ["notifications"];
export const notificationCountKey = ["notifications-count"];

export function listNotifications({ unseen = false, limit = 100 } = {}) {
  const params = new URLSearchParams({ limit: String(limit) });
  if (unseen) {
    params.set("unseen", "1");
  }
  return fetchJSON(`/api/v1/notifications?${params}`);
}

export function markNotificationSeen(id, seen = true) {
  return fetchJSON(`/api/v1/notifications/${encodeURIComponent(id)}/seen`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ seen }),
  });
}

export function markAllNotificationsSeen() {
  return fetchJSON("/api/v1/notifications/seen", { method: "POST" });
}

// followNotifications reads the server's notification stream until signal
// aborts. It returns the response status, so a caller can stop retrying a
// server that has notifications off (404) or a signed-out viewer (401).
export async function followNotifications({ signal, onNotification, onCount }) {
  const response = await fetch("/api/v1/notifications/stream", {
    credentials: "same-origin",
    signal,
    headers: { Accept: "text/event-stream", ...operatorIdentityHeaders() },
  });
  if (!response.ok || !response.body) {
    return response.status;
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  for (;;) {
    const { done, value } = await reader.read();
    if (done) {
      return response.status;
    }
    buffer += decoder.decode(value, { stream: true });
    let boundary;
    while ((boundary = buffer.indexOf("\n\n")) >= 0) {
      const frame = parseSSEFrame(buffer.slice(0, boundary));
      buffer = buffer.slice(boundary + 2);
      if (frame?.event === "notification") {
        onNotification?.(frame.data);
      } else if (frame?.event === "count") {
        onCount?.(frame.data?.unseen_count ?? 0);
      }
    }
  }
}

// useNotificationStream keeps the unseen count and the inbox list current for
// the signed-in viewer. Mount it once, high in the tree.
export function useNotificationStream(enabled = true) {
  const queryClient = useQueryClient();
  const identityKey = operatorIdentityKey(useOperatorIdentity());
  React.useEffect(() => {
    if (!enabled) {
      return undefined;
    }
    const controller = new AbortController();
    let stopped = false;
    async function run() {
      let delay = 2000;
      while (!stopped) {
        let status = 0;
        try {
          status = await followNotifications({
            signal: controller.signal,
            onCount: (count) => queryClient.setQueryData(notificationCountKey, count),
            onNotification: () => queryClient.invalidateQueries({ queryKey: notificationsKey }),
          });
        } catch {
          status = 0;
        }
        if (stopped || status === 401 || status === 403 || status === 404) {
          return;
        }
        await new Promise((resolve) => setTimeout(resolve, delay));
        delay = Math.min(delay * 2, 30000);
      }
    }
    run();
    return () => {
      stopped = true;
      controller.abort();
    };
  }, [queryClient, identityKey, enabled]);
}

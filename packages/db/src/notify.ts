import pg from "pg";
import { buildPoolConfig } from "./connection";

export type NotifyClient = {
  on: (handler: (channel: string, payload: string) => void) => void;
  close: () => Promise<void>;
};

// Dedicated LISTEN connection. Pool clients can't be used for LISTEN because
// returning them to the pool drops the subscription.
export async function createNotifyClient(
  channel: string,
): Promise<NotifyClient> {
  const client = new pg.Client(buildPoolConfig());
  await client.connect();
  await client.query(`LISTEN ${client.escapeIdentifier(channel)}`);
  const handlers = new Set<(channel: string, payload: string) => void>();
  client.on("notification", (msg: pg.Notification) => {
    for (const h of handlers) h(msg.channel, msg.payload ?? "");
  });
  client.on("error", (err) => {
    console.warn(`[neko/db notify] LISTEN client error on ${channel}:`, err);
  });
  return {
    on(handler) {
      handlers.add(handler);
    },
    async close() {
      handlers.clear();
      try {
        await client.end();
      } catch {
        // ignore — already closed or errored
      }
    },
  };
}

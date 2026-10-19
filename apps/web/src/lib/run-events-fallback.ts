// Streaming backends can deliver SSE message deltas in an order
// that doesn't match the agent's emission sequence, which scrambles the
// concatenated text on the client. The persisted work_run_event log IS
// stored in seq order, though — so after a run completes, re-fetching from
// the DB gives us a clean, correctly-ordered transcript that fence parsers
// can read reliably.
//
// Use this from authoring chats' settle() handler to recover the live
// card payload even when the live stream was garbled.
export async function fetchAssistantTextFromRun(
  runId: string,
): Promise<string> {
  try {
    const res = await fetch(`/api/work/runs/${runId}/events`, {
      cache: "no-store",
    });
    if (!res.ok) return "";
    const data = (await res.json()) as {
      events?: Array<{ type?: string; role?: string; content?: string }>;
    };
    if (!Array.isArray(data.events)) return "";
    return data.events
      .filter((e) => e.type === "message" && e.role === "assistant")
      .map((e) => e.content ?? "")
      .join("");
  } catch {
    return "";
  }
}

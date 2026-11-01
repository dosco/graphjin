/** Turn an A2UI action context into the visible follow-up sent to the agent. */
export function buildActionFollowUp(context?: Record<string, unknown>): string | null {
  const prompt = typeof context?.prompt === "string" ? context.prompt.trim() : "";
  if (!prompt) return null;
  const submitted = Object.fromEntries(
    Object.entries(context ?? {}).filter(([key, value]) =>
      key !== "prompt" && !(typeof value === "string" && prompt.includes(value.trim()))),
  );
  if (Object.keys(submitted).length === 0) return prompt;
  return [
    prompt,
    "",
    "Submitted values:",
    "```json",
    JSON.stringify(submitted, null, 2),
    "```",
  ].join("\n");
}

/** Present submitted clarification answers without changing the agent's message. */
export function parseClarificationReply(message: string): Array<{ question: string; answer: string }> | null {
  if (!message.startsWith("Continue the previous request using these operator-supplied answers.")) return null;
  const marker = "\n\nSubmitted values:\n```json\n";
  const start = message.indexOf(marker);
  if (start < 0 || !message.endsWith("\n```")) return null;
  try {
    const submitted = JSON.parse(message.slice(start + marker.length, -4));
    if (!Array.isArray(submitted.questions) || !submitted.answers || typeof submitted.answers !== "object") return null;
    const replies: Array<{ question: string; answer: string } | null> = submitted.questions.map((item: unknown) => {
      if (!item || typeof item !== "object") return null;
      const { id, header, question } = item as Record<string, unknown>;
      if (typeof id !== "string" || typeof question !== "string") return null;
      const value = submitted.answers[id];
      const answer = Array.isArray(value) ? value.join(", ") : value;
      if (typeof answer !== "string") return null;
      return { question: typeof header === "string" ? `${header}: ${question}` : question, answer };
    });
    const valid = replies.filter((item): item is { question: string; answer: string } => item !== null);
    return valid.length > 0 && valid.length === replies.length ? valid : null;
  } catch {
    return null;
  }
}

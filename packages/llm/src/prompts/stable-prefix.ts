// Sections that change per user, run or message. The prompt builders put them
// after every org-wide section, so the text before the first one is the same
// for every turn of the org, and providers can cache it.
const TURN_SECTION_OPENERS = [
  "<operator-profile>",
  "<bot>",
  "<workspace>",
  "<long_term_memory>",
  "<conversation_so_far>",
  "<files>",
];

export function splitStablePrompt(prompt: string): { stable: string; turn: string } {
  let cut = -1;
  for (const opener of TURN_SECTION_OPENERS) {
    const at = prompt.indexOf(`\n\n${opener}\n`);
    if (at > 0 && (cut < 0 || at < cut)) cut = at;
  }
  if (cut < 0) return { stable: "", turn: prompt };
  return { stable: prompt.slice(0, cut), turn: prompt.slice(cut + 2) };
}
